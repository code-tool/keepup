package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeEnvironment(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", EnvironmentProd, false},
		{"prod", EnvironmentProd, false},
		{" PROD ", EnvironmentProd, false},
		{"non-prod", EnvironmentNonProd, false},
		{"Non-Prod", EnvironmentNonProd, false},
		{"dev", "", true},
		{"staging", "", true},
	}
	for _, tc := range cases {
		got, err := normalizeEnvironment(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("normalizeEnvironment(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeEnvironment(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPackageVersionsInsert_EnvironmentDefaultsToProd(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &PackageVersionss{Items: make(map[uuid.UUID]PackageVersions)}

	id, err := c.Insert(PackageVersions{DataCenterPkg: "dc1", HostIPPkg: "10.0.0.1"}, ctx, con, noopQuery, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stored, err := c.Retrieve(id, ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.Environment != EnvironmentProd {
		t.Errorf("expected environment %q, got %q", EnvironmentProd, stored.Environment)
	}
}

func TestPackageVersionsInsert_RejectsUnknownEnvironment(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &PackageVersionss{Items: make(map[uuid.UUID]PackageVersions)}

	_, err := c.Insert(PackageVersions{DataCenterPkg: "dc1", HostIPPkg: "10.0.0.1", Environment: "dev"}, ctx, con, noopQuery, 60)
	if err != ErrInvalidEnvironment {
		t.Fatalf("expected ErrInvalidEnvironment, got %v", err)
	}
}

func TestClusterInsert_StoresNonProdEnvironment(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	id, err := c.InsertClusterData(KubernetesCluster{ClusterName: "dev-1", Environment: "non-prod"}, ctx, con, noopQuery, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stored, err := c.RetrieveCluster(id, ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.Environment != EnvironmentNonProd {
		t.Errorf("expected environment %q, got %q", EnvironmentNonProd, stored.Environment)
	}
}

func TestClusterInsert_RejectsUnknownEnvironment(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	c := &KubernetesClusters{Items: make(map[uuid.UUID]KubernetesCluster)}

	_, err := c.InsertClusterData(KubernetesCluster{ClusterName: "dev-1", Environment: "dev"}, ctx, con, noopQuery, 60)
	if err != ErrInvalidEnvironment {
		t.Fatalf("expected ErrInvalidEnvironment, got %v", err)
	}
}

func TestHandleInsertPackages_EnvironmentIsMetadataNotPackage(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	h := &PackageVersionsHandler{
		PackageVersions: &PackageVersionss{Items: make(map[uuid.UUID]PackageVersions)},
		Client:          con,
		Context:         ctx,
		ApiToken:        "secret",
		TTL:             60,
	}

	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/package-version", strings.NewReader(body))
		req.Header.Set("x-api-token", "secret")
		rec := httptest.NewRecorder()
		h.Handler()(rec, req)
		return rec
	}

	rec := put(`{"packages": {"host_ip": "10.0.0.1", "data_center": "dc1", "environment": "dev"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown environment, got %d", rec.Code)
	}

	rec = put(`{"packages": {"host_ip": "10.0.0.1", "data_center": "dc1", "environment": "non-prod"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	stored, err := h.PackageVersions.Retrieve(UUIDFromDcAndIPPackage("dc1", "10.0.0.1"), ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.Environment != EnvironmentNonProd {
		t.Errorf("expected environment %q, got %q", EnvironmentNonProd, stored.Environment)
	}
	if _, ok := stored.Packages["environment"]; ok {
		t.Errorf("environment must not be stored as a package")
	}
}
