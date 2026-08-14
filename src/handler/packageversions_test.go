package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func noopQuery(name string) (string, string, error) {
	return "unknown", "", nil
}

func TestUUIDFromDcAndIPPackage_Deterministic(t *testing.T) {
	a := UUIDFromDcAndIPPackage("dc1", "10.0.0.1")
	b := UUIDFromDcAndIPPackage("dc1", "10.0.0.1")
	if a != b {
		t.Fatalf("expected same input to produce the same UUID, got %s and %s", a, b)
	}
}

func TestPackageVersionsInsertAndRetrieve(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &PackageVersionss{Items: make(map[uuid.UUID]PackageVersions)}

	pkg := PackageVersions{
		DataCenterPkg: "dc1",
		HostIPPkg:     "10.0.0.1",
		Packages: map[string]PackageDetail{
			"redis": {CurrentVersion: "7.0.15"},
		},
	}

	id, err := c.Insert(pkg, ctx, con, noopQuery, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := c.Retrieve(id, ctx, con)
	if err != nil {
		t.Fatalf("unexpected error retrieving inserted package: %v", err)
	}
	if _, ok := stored.Packages["redis"]; !ok {
		t.Errorf("expected stored packages to contain %q", "redis")
	}
}

func TestPackageVersionsRetrieve_UnmarshalFailureOnCorruptData(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &PackageVersionss{Items: make(map[uuid.UUID]PackageVersions)}

	id := uuid.New()
	if err := con.Set(ctx, id.String(), "not-json", 0).Err(); err != nil {
		t.Fatalf("failed to seed corrupt value: %v", err)
	}

	if _, err := c.Retrieve(id, ctx, con); err != ErrMarshalFailedPackage {
		t.Fatalf("expected ErrMarshalFailedPackage for corrupt data, got %v", err)
	}
}

func TestPackageVersionsScan_ReturnsAllInsertedPackages(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &PackageVersionss{Items: make(map[uuid.UUID]PackageVersions)}

	first, err := c.Insert(PackageVersions{DataCenterPkg: "dc1", HostIPPkg: "10.0.0.1"}, ctx, con, noopQuery, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := c.Insert(PackageVersions{DataCenterPkg: "dc1", HostIPPkg: "10.0.0.2"}, ctx, con, noopQuery, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := c.Scan(ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	if _, ok := result.Items[first]; !ok {
		t.Errorf("expected scan to contain first inserted package %s", first)
	}
	if _, ok := result.Items[second]; !ok {
		t.Errorf("expected scan to contain second inserted package %s", second)
	}
}

func TestPackageVersionsScan_SkipsCorruptEntryWithoutFailingWholeScan(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &PackageVersionss{Items: make(map[uuid.UUID]PackageVersions)}

	good, err := c.Insert(PackageVersions{DataCenterPkg: "dc1", HostIPPkg: "10.0.0.1"}, ctx, con, noopQuery, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	corrupt := uuid.New()
	if err := con.Set(ctx, corrupt.String(), "not-json", 0).Err(); err != nil {
		t.Fatalf("failed to seed corrupt value: %v", err)
	}

	result, err := c.Scan(ctx, con)
	if err != nil {
		t.Fatalf("unexpected error from scan: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected scan to skip the corrupt entry and return 1 item, got %d", len(result.Items))
	}
	if _, ok := result.Items[good]; !ok {
		t.Errorf("expected scan to still contain the valid package %s", good)
	}
}

func TestPackageVersionsScan_EmptyDatabase(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &PackageVersionss{Items: make(map[uuid.UUID]PackageVersions)}

	result, err := c.Scan(ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected no items in an empty database, got %d", len(result.Items))
	}
}
