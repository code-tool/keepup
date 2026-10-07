package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func mockQueryFunc(string) (string, string, error) {
	return "1.0", "", nil
}

func TestUUIDFromClusterName_Deterministic(t *testing.T) {
	a := UUIDFromClusterName("minikube")
	b := UUIDFromClusterName("minikube")
	if a != b {
		t.Fatalf("expected same input to produce the same UUID, got %s and %s", a, b)
	}
}

func TestUUIDFromClusterName_DifferentInputsDiffer(t *testing.T) {
	a := UUIDFromClusterName("cluster-a")
	b := UUIDFromClusterName("cluster-b")
	if a == b {
		t.Fatalf("expected different cluster names to produce different UUIDs, both were %s", a)
	}
}

func TestClusterInsertAndRetrieve(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	id, err := c.InsertClusterData(KubernetesCluster{ClusterName: "minikube", KubeVersion: "1.30"}, ctx, con, mockQueryFunc, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := c.RetrieveCluster(id, ctx, con)
	if err != nil {
		t.Fatalf("unexpected error retrieving inserted cluster: %v", err)
	}
	if stored.ClusterName != "minikube" {
		t.Errorf("expected cluster_name %q, got %q", "minikube", stored.ClusterName)
	}
}

func TestClusterInsertAndRetrieve_ChartVersionEoF(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	queryFunc := func(chartName string) (string, string, error) {
		return "7.2.0", "2026-01-01", nil
	}

	cluster := KubernetesCluster{
		ClusterName: "minikube",
		HelmCharts: []HelmChartData{
			{
				ChartName:    "redis",
				ChartVersion: "7.1.0",
			},
		},
	}

	id, err := c.InsertClusterData(cluster, ctx, con, queryFunc, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := c.RetrieveCluster(id, ctx, con)
	if err != nil {
		t.Fatalf("unexpected error retrieving inserted cluster: %v", err)
	}

	if stored.HelmCharts[0].ChartVersionEoF != "2026-01-01" {
		t.Errorf("expected ChartVersionEoF %q, got %q", "2026-01-01", stored.HelmCharts[0].ChartVersionEoF)
	}

	if !stored.HelmCharts[0].ChartVersionExpired {
		t.Errorf("expected ChartVersionExpired to be true")
	}

	if stored.HelmCharts[0].ChartVersion != "7.1.0" {
		t.Errorf("expected full ChartVersion %q, got %q", "7.1.0", stored.HelmCharts[0].ChartVersion)
	}
}

func TestClusterInsertAndRetrieve_KeepsChartsWithoutVersion(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	queried := 0
	queryFunc := func(chartName string) (string, string, error) {
		queried++
		return "7.2.0", "2026-01-01", nil
	}

	cluster := KubernetesCluster{
		ClusterName: "minikube",
		HelmCharts: []HelmChartData{
			{ChartName: "redis", ChartVersion: "", ChartNamespace: "database"},
			{ChartName: "keepup", ChartVersion: "unknown", ChartNamespace: "monitoring"},
		},
	}

	id, err := c.InsertClusterData(cluster, ctx, con, queryFunc, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := c.RetrieveCluster(id, ctx, con)
	if err != nil {
		t.Fatalf("unexpected error retrieving inserted cluster: %v", err)
	}

	if len(stored.HelmCharts) != 2 {
		t.Fatalf("expected both charts to be stored, got %d", len(stored.HelmCharts))
	}
	if queried != 0 {
		t.Errorf("expected no EOL lookups for charts without a version, got %d", queried)
	}

	for i, want := range cluster.HelmCharts {
		got := stored.HelmCharts[i]
		if got.ChartName != want.ChartName || got.ChartVersion != want.ChartVersion || got.ChartNamespace != want.ChartNamespace {
			t.Errorf("chart %d: expected %s/%q/%s, got %s/%q/%s", i,
				want.ChartName, want.ChartVersion, want.ChartNamespace,
				got.ChartName, got.ChartVersion, got.ChartNamespace)
		}
		if got.ChartNewestVersion != "unknown" || got.ChartVersionEoF != "false" || got.ChartVersionExpired {
			t.Errorf("chart %d: expected unknown/false/false EOL fields, got %q/%q/%t", i,
				got.ChartNewestVersion, got.ChartVersionEoF, got.ChartVersionExpired)
		}
	}
}

func TestClusterInsertAndRetrieve_ChartVersionEoF_QueryError(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	queryFunc := func(chartName string) (string, string, error) {
		return "", "", errors.New("EOL API error")
	}

	cluster := KubernetesCluster{
		ClusterName: "minikube",
		HelmCharts: []HelmChartData{
			{
				ChartName:    "redis",
				ChartVersion: "7.1.0",
			},
		},
	}

	id, err := c.InsertClusterData(cluster, ctx, con, queryFunc, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := c.RetrieveCluster(id, ctx, con)
	if err != nil {
		t.Fatalf("unexpected error retrieving inserted cluster: %v", err)
	}

	if stored.HelmCharts[0].ChartVersionEoF != "false" {
		t.Errorf("expected ChartVersionEoF %q, got %q", "false", stored.HelmCharts[0].ChartVersionEoF)
	}

	if stored.HelmCharts[0].ChartNewestVersion != "unknown" {
		t.Errorf("expected ChartNewestVersion %q, got %q", "unknown", stored.HelmCharts[0].ChartNewestVersion)
	}

	if stored.HelmCharts[0].ChartVersionExpired {
		t.Errorf("expected ChartVersionExpired to be false")
	}
}

func TestClusterRetrieve_UnmarshalFailureOnCorruptData(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	id := uuid.New()
	if err := con.Set(ctx, id.String(), "not-json", 0).Err(); err != nil {
		t.Fatalf("failed to seed corrupt value: %v", err)
	}

	if _, err := c.RetrieveCluster(id, ctx, con); err != ErrClusterMarshalFailed {
		t.Fatalf("expected ErrClusterMarshalFailed for corrupt data, got %v", err)
	}
}

func TestClusterScan_ReturnsAllInsertedClusters(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	first, err := c.InsertClusterData(KubernetesCluster{ClusterName: "cluster-a"}, ctx, con, mockQueryFunc, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := c.InsertClusterData(KubernetesCluster{ClusterName: "cluster-b"}, ctx, con, mockQueryFunc, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := c.ScanClusters(ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	if _, ok := result.Items[first]; !ok {
		t.Errorf("expected scan to contain first inserted cluster %s", first)
	}
	if _, ok := result.Items[second]; !ok {
		t.Errorf("expected scan to contain second inserted cluster %s", second)
	}
}

func TestClusterScan_SkipsCorruptAndNonUUIDEntries(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	good, err := c.InsertClusterData(KubernetesCluster{ClusterName: "cluster-a"}, ctx, con, mockQueryFunc, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	corrupt := uuid.New()
	if err := con.Set(ctx, corrupt.String(), "not-json", 0).Err(); err != nil {
		t.Fatalf("failed to seed corrupt value: %v", err)
	}
	if err := con.Set(ctx, "eol_cache:all_packages", "{}", 0).Err(); err != nil {
		t.Fatalf("failed to seed eol cache key: %v", err)
	}

	result, err := c.ScanClusters(ctx, con)
	if err != nil {
		t.Fatalf("unexpected error from scan: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected scan to skip corrupt/non-uuid entries and return 1 item, got %d", len(result.Items))
	}
	if _, ok := result.Items[good]; !ok {
		t.Errorf("expected scan to still contain the valid cluster %s", good)
	}
}

func TestClusterScan_EmptyDatabase(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	result, err := c.ScanClusters(ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected no items in an empty database, got %d", len(result.Items))
	}
}

func TestIsVersionExpired(t *testing.T) {
	currentVersions := []string{"7.1", "7.2", "7.3", "invalid"}
	newestVersions := []string{"7.2", "unknown", "invalid"}

	for _, current := range currentVersions {
		isVersionExpired(current, "7.2")
	}

	for _, newest := range newestVersions {
		isVersionExpired("7.2", newest)
	}
}
