package portfolio

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validCatalog = `
team: team-a
capacity_days: 59.4
objects:
  - name: mongo replicaset
    environment: prod
    source: package
    key: mongodb
    maintenances_per_year: 2
    hours_per_object: 0.5
    automation_factor: 1
  - name: ingress controller
    environment: non-prod
    source: helm
    key: ingress-nginx
    maintenances_per_year: 4
    hours_per_object: 0.25
    automation_factor: 1
  - name: sentry
    environment: prod
    source: static
    count: 1
    maintenances_per_year: 4
    hours_per_object: 12
    automation_factor: 1
`

const okObject = `  - name: redis sentinel
    environment: prod
    source: package
    key: redis
    maintenances_per_year: 2
    hours_per_object: 1
    automation_factor: 1
`

func catalogWith(objects string) string {
	return "team: team-a\nobjects:\n" + objects
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseValid(t *testing.T) {
	team, err := Parse([]byte(validCatalog))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if team.Team != "team-a" || team.HoursPerDay != 8 || len(team.Objects) != 3 {
		t.Fatalf("unexpected team: %+v", team)
	}
	if !almostEqual(team.CapacityHours(), 59.4*8) {
		t.Errorf("CapacityHours = %v, want %v", team.CapacityHours(), 59.4*8)
	}
	if got := team.Objects[0].AnnualHoursPerObject(); !almostEqual(got, 1) {
		t.Errorf("AnnualHoursPerObject = %v, want 1", got)
	}
	if c := team.Objects[2].Count; c == nil || *c != 1 {
		t.Errorf("static count = %v, want 1", c)
	}
}

func TestParseExplicitHoursPerDay(t *testing.T) {
	team, err := Parse([]byte("team: team-a\ncapacity_days: 10\nhours_per_day: 6\nobjects:\n" + okObject))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(team.CapacityHours(), 60) {
		t.Errorf("CapacityHours = %v, want 60", team.CapacityHours())
	}
}

func TestParseSameKeyDifferentEnvironments(t *testing.T) {
	nonProd := strings.Replace(okObject, "environment: prod", "environment: non-prod", 1)
	if _, err := Parse([]byte(catalogWith(okObject + nonProd))); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseInvalid(t *testing.T) {
	static := strings.Replace(okObject, "source: package", "source: static", 1)
	with := func(from, to string) string { return catalogWith(strings.Replace(okObject, from, to, 1)) }

	cases := []struct {
		name, catalog, wantErr string
	}{
		{"empty", "", "empty catalog"},
		{"missing team", "objects:\n" + okObject, "team is required"},
		{"no objects", "team: team-a\n", "at least one object"},
		{"negative hours_per_day", "team: team-a\nhours_per_day: -1\nobjects:\n" + okObject, "hours_per_day must be > 0"},
		{"negative capacity", "team: team-a\ncapacity_days: -1\nobjects:\n" + okObject, "capacity_days must be >= 0"},
		{"unknown field", with("automation_factor: 1", "automation: 1"), "field automation not found"},
		{"bad environment", with("environment: prod", "environment: dev"), `environment "dev"`},
		{"bad source", with("source: package", "source: rpm"), `source "rpm"`},
		{"missing key", with("    key: redis\n", ""), "key is required"},
		{"count on package", catalogWith(okObject + "    count: 3\n"), "count is only allowed"},
		{"static without count", catalogWith(static), "count is required"},
		{"static negative count", catalogWith(static + "    count: -1\n"), "count must be >= 0"},
		{"zero hours", with("hours_per_object: 1", "hours_per_object: 0"), "hours_per_object must be > 0"},
		{"missing automation_factor", with("    automation_factor: 1\n", ""), "automation_factor must be > 0"},
		{"missing maintenances_per_year", with("    maintenances_per_year: 2\n", ""), "maintenances_per_year must be > 0"},
		{"duplicate key", catalogWith(okObject + okObject), "duplicates objects[0]"},
		{"duplicate static", catalogWith(static + "    count: 1\n" + static + "    count: 2\n"), "duplicates objects[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.catalog))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "team-a.yaml"), validCatalog)
	writeFile(t, filepath.Join(dir, "bravo.yml"), strings.Replace(catalogWith(okObject), "team: team-a", "team: bravo", 1))
	writeFile(t, filepath.Join(dir, "README.txt"), "not a catalog")
	writeFile(t, filepath.Join(dir, ".hidden.yaml"), "garbage: [")

	p, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Teams) != 2 || p.Teams[0].Team != "bravo" || p.Teams[1].Team != "team-a" {
		t.Fatalf("teams = %+v, want [bravo team-a]", p.Teams)
	}
}

func TestLoadDirConfigMapLayout(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "..2026_11_08_12_00_00.123", "team-a.yaml"), validCatalog)
	if err := os.Symlink("..2026_11_08_12_00_00.123", filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "team-a.yaml"), filepath.Join(dir, "team-a.yaml")); err != nil {
		t.Fatal(err)
	}

	p, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Teams) != 1 || p.Teams[0].Team != "team-a" {
		t.Fatalf("teams = %+v, want [team-a]", p.Teams)
	}
}

func TestLoadDirErrors(t *testing.T) {
	invalid := catalogWith(strings.Replace(okObject, "environment: prod", "environment: dev", 1))
	cases := []struct {
		name    string
		files   map[string]string
		wantErr string
	}{
		{"duplicate team", map[string]string{"a.yaml": validCatalog, "b.yaml": validCatalog}, `team "team-a" is already defined`},
		{"invalid file", map[string]string{"team-a.yaml": invalid}, "team-a.yaml"},
		{"empty dir", nil, "no team catalogs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				writeFile(t, filepath.Join(dir, name), content)
			}
			_, err := LoadDir(dir)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadDirMissing(t *testing.T) {
	if _, err := LoadDir(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("expected error for missing directory")
	}
}
