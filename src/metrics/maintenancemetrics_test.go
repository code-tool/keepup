package metrics

import (
	"context"
	"encoding/json"
	"keepup/src/handler"
	"keepup/src/portfolio"
	"math"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

type seedFunc func(ctx context.Context, c *redis.Client)

func store(t *testing.T, ctx context.Context, c *redis.Client, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, uuid.New().String(), data, 0).Err(); err != nil {
		t.Fatal(err)
	}
}

func host(team, env string, packages map[string]string) handler.PackageVersions {
	details := make(map[string]handler.PackageDetail, len(packages))
	for name, version := range packages {
		details[name] = handler.PackageDetail{CurrentVersion: version}
	}
	return handler.PackageVersions{Team: team, Environment: env, Packages: details}
}

func cluster(team, env string, charts ...[2]string) handler.KubernetesCluster {
	data := make([]handler.HelmChartData, len(charts))
	for i, c := range charts {
		data[i] = handler.HelmChartData{ChartName: c[0], ChartNamespace: c[1]}
	}
	return handler.KubernetesCluster{Team: team, Environment: env, HelmCharts: data}
}

func gauge(t *testing.T, g prometheus.Gatherer, name string, labels map[string]string) (float64, bool) {
	t.Helper()
	mfs, err := g.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
	next:
		for _, m := range mf.GetMetric() {
			if len(m.GetLabel()) != len(labels) {
				continue
			}
			for _, l := range m.GetLabel() {
				if labels[l.GetName()] != l.GetValue() {
					continue next
				}
			}
			return m.GetGauge().GetValue(), true
		}
	}
	return 0, false
}

func objLabels(env, object, source, key string) map[string]string {
	return map[string]string{"team": "team-a", "environment": env, "object": object, "source": source, "key": key}
}

func uncatLabels(team, env, source, key string) map[string]string {
	return map[string]string{"team": team, "environment": env, "source": source, "key": key}
}

func newRegistry(t *testing.T, seed seedFunc) *prometheus.Registry {
	t.Helper()
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	seed(ctx, client)

	one := 1.0
	catalog := &portfolio.Portfolio{Teams: []portfolio.Team{{
		Team:         "team-a",
		CapacityDays: 59.4,
		HoursPerDay:  8,
		Objects: []portfolio.Object{
			{Name: "mongo replicaset", Environment: "prod", Source: "package", Key: "mongodb", MaintenancesPerYear: 2, HoursPerObject: 0.5, AutomationFactor: 1},
			{Name: "redis sentinel", Environment: "prod", Source: "package", Key: "redis", MaintenancesPerYear: 2, HoursPerObject: 1, AutomationFactor: 1},
			{Name: "ingress controller", Environment: "non-prod", Source: "helm", Key: "ingress-nginx", MaintenancesPerYear: 4, HoursPerObject: 0.25, AutomationFactor: 1},
			{Name: "sentry", Environment: "prod", Source: "static", Count: &one, MaintenancesPerYear: 4, HoursPerObject: 12, AutomationFactor: 1},
		},
	}}}

	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(MaintenanceCollector{
		Portfolio: catalog,
		PackageInfo: &handler.PackageVersionsHandler{
			PackageVersions: &handler.PackageVersionss{},
			Client:          client,
			Context:         ctx,
		},
		ClusterInfo: &handler.KubernetesClusterMiddleware{
			Clusters: &handler.KubernetesClusters{},
			Client:   client,
			Context:  ctx,
		},
	})
	return reg
}

func TestMaintenanceCollector(t *testing.T) {
	reg := newRegistry(t, func(ctx context.Context, c *redis.Client) {
		store(t, ctx, c, host("team-a", "prod", map[string]string{"mongodb": "7.0", "redis": "unknown", "php": "8.2"}))
		store(t, ctx, c, host("team-a", "prod", map[string]string{"mongodb": "6.0", "redis": ""}))
		store(t, ctx, c, host("team-a", "", map[string]string{"mongodb": "7.0"}))
		store(t, ctx, c, host("team-a", "non-prod", map[string]string{"mongodb": "7.0"}))
		store(t, ctx, c, host("other", "prod", map[string]string{"mongodb": "7.0", "php": "8.1"}))

		store(t, ctx, c, cluster("team-a", "non-prod", [2]string{"ingress-nginx", "a"}, [2]string{"ingress-nginx", "a"}, [2]string{"ingress-nginx", "b"}))
		store(t, ctx, c, cluster("team-a", "non-prod", [2]string{"ingress-nginx", "a"}))
		store(t, ctx, c, cluster("team-a", "prod", [2]string{"ingress-nginx", "x"}))
		store(t, ctx, c, cluster("other", "non-prod", [2]string{"ingress-nginx", "a"}))
	})

	cases := []struct {
		metric string
		labels map[string]string
		want   float64
	}{
		{"maintenance_capacity_hours", map[string]string{"team": "team-a"}, 59.4 * 8},
		{"maintenance_objects", objLabels("prod", "mongo replicaset", "package", "mongodb"), 3},
		{"maintenance_hours_annual", objLabels("prod", "mongo replicaset", "package", "mongodb"), 3},
		{"maintenance_objects", objLabels("prod", "redis sentinel", "package", "redis"), 0},
		{"maintenance_hours_annual", objLabels("prod", "redis sentinel", "package", "redis"), 0},
		{"maintenance_objects", objLabels("non-prod", "ingress controller", "helm", "ingress-nginx"), 3},
		{"maintenance_hours_annual", objLabels("non-prod", "ingress controller", "helm", "ingress-nginx"), 3},
		{"maintenance_objects", objLabels("prod", "sentry", "static", ""), 1},
		{"maintenance_hours_annual", objLabels("prod", "sentry", "static", ""), 48},
		{"maintenance_uncatalogued_objects", uncatLabels("team-a", "prod", "package", "php"), 1},
		{"maintenance_uncatalogued_objects", uncatLabels("team-a", "non-prod", "package", "mongodb"), 1},
		{"maintenance_uncatalogued_objects", uncatLabels("team-a", "prod", "helm", "ingress-nginx"), 1},
	}
	for _, tc := range cases {
		got, ok := gauge(t, reg, tc.metric, tc.labels)
		if !ok || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s%v = %v (found %v), want %v", tc.metric, tc.labels, got, ok, tc.want)
		}
	}

	for _, labels := range []map[string]string{
		uncatLabels("other", "prod", "package", "php"),
		uncatLabels("other", "non-prod", "helm", "ingress-nginx"),
	} {
		if _, ok := gauge(t, reg, "maintenance_uncatalogued_objects", labels); ok {
			t.Errorf("unexpected series %v", labels)
		}
	}
	if _, ok := gauge(t, reg, "maintenance_capacity_hours", map[string]string{"team": "other"}); ok {
		t.Error("unexpected capacity series for team other")
	}
}

func TestMaintenanceCollectorEmptyInventory(t *testing.T) {
	reg := newRegistry(t, func(context.Context, *redis.Client) {})

	if got, ok := gauge(t, reg, "maintenance_objects", objLabels("prod", "mongo replicaset", "package", "mongodb")); !ok || got != 0 {
		t.Errorf("mongo objects = %v (found %v), want 0", got, ok)
	}
	if got, ok := gauge(t, reg, "maintenance_hours_annual", objLabels("prod", "sentry", "static", "")); !ok || got != 48 {
		t.Errorf("sentry hours = %v (found %v), want 48", got, ok)
	}
}
