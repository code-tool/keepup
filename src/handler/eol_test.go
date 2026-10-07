package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newFakeEOLAPI serves a single release cycle for every product except those in failing,
// and points eolAPIBaseURL at it for the duration of the test.
func newFakeEOLAPI(t *testing.T, failing ...string) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		product := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".json")
		for _, f := range failing {
			if product == f {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
		}
		w.Write([]byte(`[{"cycle":"7.4","eol":"2027-01-01","latest":"7.4.2"}]`))
	}))
	t.Cleanup(srv.Close)

	orig := eolAPIBaseURL
	eolAPIBaseURL = srv.URL
	t.Cleanup(func() { eolAPIBaseURL = orig })
	return &hits
}

func TestQueryEndOfLifeAPI_PackagesAndChartsShareCache(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	hits := newFakeEOLAPI(t)

	// Mix of a package-style lookup, a chart-style lookup and a product endoflife.date doesn't know.
	for _, name := range []string{"redis", "metallb", "keepup", "redis", "ingress-nginx"} {
		if _, _, err := queryEndOfLifeAPI(name, ctx, con); err != nil {
			t.Fatalf("query %s: unexpected error: %v", name, err)
		}
	}

	if got, want := int(hits.Load()), len(supportedEOLProducts); got != want {
		t.Fatalf("expected the cache to be built once (%d requests), got %d requests", want, got)
	}
}

func TestQueryEndOfLifeAPI_UnknownProductIsReportedUnknown(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	newFakeEOLAPI(t)

	latest, eol, err := queryEndOfLifeAPI("keepup", ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "unknown" || eol != "false" {
		t.Fatalf("expected (unknown, false), got (%s, %s)", latest, eol)
	}
}

func TestQueryEndOfLifeAPI_ReturnsLatestAndEOL(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	newFakeEOLAPI(t)

	latest, eol, err := queryEndOfLifeAPI("redis", ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "7.4" || eol != "2027-01-01" {
		t.Fatalf("expected (7.4, 2027-01-01), got (%s, %s)", latest, eol)
	}
}

func TestQueryEndOfLifeAPI_RebuildsLegacyHelmChartCache(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	hits := newFakeEOLAPI(t)

	// Cache document in the format previously written by the Helm chart lookup.
	if err := con.Set(ctx, eolCacheKey, `{"helm_chart":{"redis":[]}}`, time.Hour).Err(); err != nil {
		t.Fatalf("failed to seed cache: %v", err)
	}

	latest, _, err := queryEndOfLifeAPI("redis", ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "7.4" {
		t.Fatalf("expected latest 7.4 after rebuild, got %s", latest)
	}
	if got, want := int(hits.Load()), len(supportedEOLProducts); got != want {
		t.Fatalf("expected one rebuild (%d requests), got %d", want, got)
	}
}

func TestUpdateEOLCache_ShortTTLOnPartialFailure(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	newFakeEOLAPI(t, "metallb")

	if err := updateEOLCache(ctx, con); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ttl, err := con.TTL(ctx, eolCacheKey).Result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ttl > eolCachePartialTTL {
		t.Fatalf("expected TTL <= %s after a failed fetch, got %s", eolCachePartialTTL, ttl)
	}

	// The failed product is reported as unknown rather than triggering another rebuild.
	latest, _, err := queryEndOfLifeAPI("metallb", ctx, con)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "unknown" {
		t.Fatalf("expected unknown for a product that failed to fetch, got %s", latest)
	}
}

func TestUpdateEOLCache_FullTTLOnSuccess(t *testing.T) {
	ctx := context.Background()
	con := newTestClient(t)
	newFakeEOLAPI(t)

	if err := updateEOLCache(ctx, con); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ttl, err := con.TTL(ctx, eolCacheKey).Result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ttl != eolCacheTTL {
		t.Fatalf("expected TTL %s, got %s", eolCacheTTL, ttl)
	}
}
