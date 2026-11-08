package portfolio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	SourcePackage = "package"
	SourceHelm    = "helm"
	SourceStatic  = "static"

	EnvironmentProd    = "prod"
	EnvironmentNonProd = "non-prod"
)

type Object struct {
	Name                string   `yaml:"name"`
	Environment         string   `yaml:"environment"`
	Source              string   `yaml:"source"`
	Key                 string   `yaml:"key"`
	Count               *float64 `yaml:"count"`
	MaintenancesPerYear float64  `yaml:"maintenances_per_year"`
	HoursPerObject      float64  `yaml:"hours_per_object"`
	AutomationFactor    float64  `yaml:"automation_factor"`
}

func (o Object) AnnualHoursPerObject() float64 {
	return o.MaintenancesPerYear * o.HoursPerObject * o.AutomationFactor
}

type Team struct {
	Team         string   `yaml:"team"`
	CapacityDays float64  `yaml:"capacity_days"`
	HoursPerDay  float64  `yaml:"hours_per_day"`
	Objects      []Object `yaml:"objects"`
}

func (t Team) CapacityHours() float64 {
	return t.CapacityDays * t.HoursPerDay
}

type Portfolio struct {
	Teams []Team
}

func Parse(data []byte) (Team, error) {
	var t Team
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		if errors.Is(err, io.EOF) {
			return t, errors.New("empty catalog")
		}
		return t, err
	}
	if t.HoursPerDay == 0 {
		t.HoursPerDay = 8
	}
	return t, t.validate()
}

func (t Team) validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if strings.TrimSpace(t.Team) == "" {
		fail("team is required")
	}
	if t.CapacityDays < 0 {
		fail("capacity_days must be >= 0")
	}
	if t.HoursPerDay < 0 {
		fail("hours_per_day must be > 0")
	}
	if len(t.Objects) == 0 {
		fail("objects: at least one object is required")
	}

	seen := make(map[string]int)
	for i, o := range t.Objects {
		where := fmt.Sprintf("objects[%d] (%s)", i, o.Name)
		if strings.TrimSpace(o.Name) == "" {
			fail("%s: name is required", where)
		}
		if o.Environment != EnvironmentProd && o.Environment != EnvironmentNonProd {
			fail("%s: environment %q must be %q or %q", where, o.Environment, EnvironmentProd, EnvironmentNonProd)
		}

		id := o.Source + "/" + o.Key + "/" + o.Environment
		switch o.Source {
		case SourcePackage, SourceHelm:
			if strings.TrimSpace(o.Key) == "" {
				fail("%s: key is required for source %q", where, o.Source)
			}
			if o.Count != nil {
				fail("%s: count is only allowed for source %q", where, SourceStatic)
			}
		case SourceStatic:
			if o.Count == nil {
				fail("%s: count is required for source %q", where, SourceStatic)
			} else if *o.Count < 0 {
				fail("%s: count must be >= 0", where)
			}
			id = o.Source + "/" + o.Name + "/" + o.Environment
		default:
			fail("%s: source %q must be %q, %q or %q", where, o.Source, SourcePackage, SourceHelm, SourceStatic)
		}

		if o.MaintenancesPerYear <= 0 {
			fail("%s: maintenances_per_year must be > 0", where)
		}
		if o.HoursPerObject <= 0 {
			fail("%s: hours_per_object must be > 0", where)
		}
		if o.AutomationFactor <= 0 {
			fail("%s: automation_factor must be > 0", where)
		}

		if prev, ok := seen[id]; ok {
			fail("%s: duplicates objects[%d]", where, prev)
		} else {
			seen[id] = i
		}
	}
	return errors.Join(errs...)
}

func LoadDir(dir string) (*Portfolio, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	p := &Portfolio{}
	files := make(map[string]string)
	for _, e := range entries {
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		// skip ConfigMap internals: ..data, ..<timestamp>
		if strings.HasPrefix(name, ".") || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		t, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if prev, ok := files[t.Team]; ok {
			return nil, fmt.Errorf("%s: team %q is already defined in %s", path, t.Team, prev)
		}
		files[t.Team] = path
		p.Teams = append(p.Teams, t)
	}

	if len(p.Teams) == 0 {
		return nil, fmt.Errorf("no team catalogs (*.yaml) found in %s", dir)
	}
	sort.Slice(p.Teams, func(i, j int) bool { return p.Teams[i].Team < p.Teams[j].Team })
	return p, nil
}
