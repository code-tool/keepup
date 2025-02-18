package metrics

import (
	"fmt"
	"keepup/src/handler"
	"log"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	IDPkg             = "id"
	PackageName       = "package_name"
	CurrentVersion    = "current_version"
	CurrentVersionEoF = "current_version_eof"
	NewestVersion     = "newest_version"
	Expired           = "expired"
	DataCenterpkg     = "data_center"
	HostIPpkg         = "host_ip"

	packageMetricDesc = prometheus.NewDesc(
		"package_version_info",
		"Metrics for package versions",
		[]string{
			IDPkg,
			PackageName,
			CurrentVersion,
			CurrentVersionEoF,
			NewestVersion,
			Expired,
			DataCenterpkg,
			HostIPpkg,
		}, nil,
	)
)

type PackageVersionsCollector struct {
	PackageInfo *handler.PackageVersionsHandler
}

func (pc PackageVersionsCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(pc, ch)
}

func (pc PackageVersionsCollector) Collect(ch chan<- prometheus.Metric) {
	pkgss, err := pc.PackageInfo.PackageVersions.Scan(pc.PackageInfo.Context, pc.PackageInfo.Client)
	if err != nil {
		log.Printf("Failed to scan package versions: %v", err)
		return
	}

	for id, pkgs := range pkgss.Items {
		for packageName, details := range pkgs.Packages {
			ch <- prometheus.MustNewConstMetric(
				packageMetricDesc,
				prometheus.GaugeValue,
				1.0,
				fmt.Sprint(id),
				packageName,
				details.CurrentVersion,
				details.CurrentVersionEoF,
				details.NewestVersion,
				fmt.Sprintf("%t", details.Expired),
				pkgs.DataCenterPkg,
				pkgs.HostIPPkg,
			)
		}
	}
}
