package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

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

	id, err := c.InsertClusterData(KubernetesCluster{ClusterName: "minikube", KubeVersion: "1.30"}, ctx, con, 60)
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

	first, err := c.InsertClusterData(KubernetesCluster{ClusterName: "cluster-a"}, ctx, con, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := c.InsertClusterData(KubernetesCluster{ClusterName: "cluster-b"}, ctx, con, 60)
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

	good, err := c.InsertClusterData(KubernetesCluster{ClusterName: "cluster-a"}, ctx, con, 60)
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
