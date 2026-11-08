package metrics

import (
	"keepup/src/handler"
	"keepup/src/portfolio"
	"log"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	maintenanceObjectLabels = []string{Team, Environment, "object", "source", "key"}

	maintenanceObjectsDesc = prometheus.NewDesc(
		"maintenance_objects",
		"Objects behind a maintenance catalog entry",
		maintenanceObjectLabels, nil,
	)
	maintenanceHoursAnnualDesc = prometheus.NewDesc(
		"maintenance_hours_annual",
		"Planned yearly maintenance hours of a catalog entry",
		maintenanceObjectLabels, nil,
	)
	maintenanceCapacityHoursDesc = prometheus.NewDesc(
		"maintenance_capacity_hours",
		"Planned yearly maintenance capacity of a team",
		[]string{Team}, nil,
	)
	maintenanceUncataloguedDesc = prometheus.NewDesc(
		"maintenance_uncatalogued_objects",
		"Inventory objects of a catalogued team that match no catalog entry",
		[]string{Team, Environment, "source", "key"}, nil,
	)
)

type MaintenanceCollector struct {
	Portfolio   *portfolio.Portfolio
	PackageInfo *handler.PackageVersionsHandler
	ClusterInfo *handler.KubernetesClusterMiddleware
}

type inventoryKey struct {
	team, environment, source, key string
}

func (mc MaintenanceCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- maintenanceObjectsDesc
	ch <- maintenanceHoursAnnualDesc
	ch <- maintenanceCapacityHoursDesc
	ch <- maintenanceUncataloguedDesc
}

func (mc MaintenanceCollector) Collect(ch chan<- prometheus.Metric) {
	counts, err := mc.countInventory()
	if err != nil {
		log.Printf("Failed to count inventory for maintenance metrics: %v", err)
		return
	}

	for _, team := range mc.Portfolio.Teams {
		ch <- prometheus.MustNewConstMetric(maintenanceCapacityHoursDesc, prometheus.GaugeValue, team.CapacityHours(), team.Team)

		catalogued := make(map[inventoryKey]bool)
		for _, obj := range team.Objects {
			var n float64
			if obj.Source == portfolio.SourceStatic {
				n = *obj.Count
			} else {
				k := inventoryKey{team.Team, obj.Environment, obj.Source, obj.Key}
				n = float64(counts[k])
				catalogued[k] = true
			}
			labels := []string{team.Team, obj.Environment, obj.Name, obj.Source, obj.Key}
			ch <- prometheus.MustNewConstMetric(maintenanceObjectsDesc, prometheus.GaugeValue, n, labels...)
			ch <- prometheus.MustNewConstMetric(maintenanceHoursAnnualDesc, prometheus.GaugeValue, n*obj.AnnualHoursPerObject(), labels...)
		}

		for k, n := range counts {
			if k.team == team.Team && !catalogued[k] {
				ch <- prometheus.MustNewConstMetric(maintenanceUncataloguedDesc, prometheus.GaugeValue, float64(n), k.team, k.environment, k.source, k.key)
			}
		}
	}
}

func (mc MaintenanceCollector) countInventory() (map[inventoryKey]int, error) {
	counts := make(map[inventoryKey]int)

	hosts, err := mc.PackageInfo.PackageVersions.Scan(mc.PackageInfo.Context, mc.PackageInfo.Client)
	if err != nil {
		return nil, err
	}
	for _, host := range hosts.Items {
		env := inventoryEnvironment(host.Environment)
		for name, details := range host.Packages {
			if details.CurrentVersion != "" && details.CurrentVersion != "unknown" {
				counts[inventoryKey{host.Team, env, portfolio.SourcePackage, name}]++
			}
		}
	}

	clusters, err := mc.ClusterInfo.Clusters.ScanClusters(mc.ClusterInfo.Context, mc.ClusterInfo.Client)
	if err != nil {
		return nil, err
	}
	for _, cluster := range clusters.Items {
		env := inventoryEnvironment(cluster.Environment)
		seen := make(map[[2]string]bool)
		for _, chart := range cluster.HelmCharts {
			nsChart := [2]string{chart.ChartName, chart.ChartNamespace}
			if !seen[nsChart] {
				seen[nsChart] = true
				counts[inventoryKey{cluster.Team, env, portfolio.SourceHelm, chart.ChartName}]++
			}
		}
	}

	return counts, nil
}

// records stored before the environment field existed
func inventoryEnvironment(env string) string {
	if env == "" {
		return handler.EnvironmentProd
	}
	return env
}
