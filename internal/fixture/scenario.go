// Package fixture generates the git repository of an evaluation scenario: real commits whose
// answer key is computed, never typed (ADR-0004).
package fixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScenarioCommit is one commit of a scenario. Any commit may apply Patch (a file in the scenario
// directory). On the first commit ("referencia") it is the preparation patch: it adds the
// pre-existing code the scenario needs, so the base demo-app stays minimal. The next commits apply
// their patches on top.
type ScenarioCommit struct {
	Label string `json:"label"`
	Patch string `json:"patch,omitempty"`
}

// ScenarioAnswer is the answer key as written in scenario.json: everything but the line, which the
// generator computes from the marker.
type ScenarioAnswer struct {
	Cause    string `json:"cause"`
	Function string `json:"function"`
	File     string `json:"file"`
	Marker   string `json:"marker"`
	Commit   string `json:"commit"`
}

// Symptom says how the evaluation frames the regression: which service and route to analyze and
// which commit labels are the bad version and the baseline (empty baseline = no reference). Route
// is the analyzed route pattern; it cannot come from the load, which may send several routes.
type Symptom struct {
	Service     string `json:"service"`
	Method      string `json:"method"`
	Route       string `json:"route"`
	RouteSource string `json:"route_source"`
	Bad         string `json:"bad"`
	Baseline    string `json:"baseline"`
}

// Load is the load the recording sends to each version: Requests GET requests at RatePerSecond,
// spread over Routes by weight.
type Load struct {
	Requests      int         `json:"requests"`
	RatePerSecond int         `json:"rate_per_second"`
	Routes        []LoadRoute `json:"routes"`
}

// LoadRoute is one route pattern of the load ("{id}" is replaced by a customer id) and its weight.
type LoadRoute struct {
	Route  string `json:"route"`
	Weight int    `json:"weight"`
}

// DefaultLoad is the load of a scenario without a load block: 200 requests at 50 per second to
// the orders route.
func DefaultLoad() Load {
	return Load{Requests: 200, RatePerSecond: 50, Routes: []LoadRoute{{Route: "/customers/{id}/orders", Weight: 1}}}
}

// Scenario is the content of scenario.json. Assumption is the detector or location assumption the
// scenario breaks; a scenario that breaks none only inflates the scoreboard, so it is required.
// Load is the declared load block, or DefaultLoad when the block is absent.
type Scenario struct {
	Name       string           `json:"name"`
	Assumption string           `json:"assumption"`
	Commits    []ScenarioCommit `json:"commits"`
	Answer     ScenarioAnswer   `json:"answer"`
	Symptom    Symptom          `json:"symptom"`
	Load       Load             `json:"load"`
}

// LoadScenario reads <dir>/scenario.json.
func LoadScenario(dir string) (Scenario, error) {
	data, err := os.ReadFile(filepath.Join(dir, "scenario.json"))
	if err != nil {
		return Scenario{}, err
	}
	var raw struct {
		Scenario
		Load *Load `json:"load"` // nil = no block, which is not the same as an empty block
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Scenario{}, fmt.Errorf("fixture: scenario.json: %w", err)
	}
	sc := raw.Scenario
	sc.Load = DefaultLoad()
	if raw.Load != nil {
		sc.Load = *raw.Load
	}
	if sc.Name == "" || len(sc.Commits) == 0 || sc.Answer.Marker == "" || sc.Answer.File == "" {
		return Scenario{}, fmt.Errorf("fixture: scenario.json in %s is incomplete", dir)
	}
	if strings.TrimSpace(sc.Assumption) == "" {
		return Scenario{}, fmt.Errorf("fixture: scenario.json in %s: assumption is empty (declare which assumption the scenario breaks)", dir)
	}
	if err := sc.Load.validate(); err != nil {
		return Scenario{}, fmt.Errorf("fixture: scenario.json in %s: %w", dir, err)
	}
	if err := sc.validateSymptom(); err != nil {
		return Scenario{}, fmt.Errorf("fixture: scenario.json in %s: %w", dir, err)
	}
	return sc, nil
}

func (l Load) validate() error {
	if l.Requests <= 0 {
		return fmt.Errorf("load.requests is %d (must be > 0)", l.Requests)
	}
	if l.RatePerSecond <= 0 {
		return fmt.Errorf("load.rate_per_second is %d (must be > 0)", l.RatePerSecond)
	}
	if len(l.Routes) == 0 {
		return fmt.Errorf("load.routes is empty")
	}
	seen := map[string]bool{}
	for i, r := range l.Routes {
		switch {
		case !strings.HasPrefix(r.Route, "/"):
			return fmt.Errorf("load.routes[%d].route %q does not start with /", i, r.Route)
		case seen[r.Route]:
			return fmt.Errorf("load.routes[%d].route %q is repeated", i, r.Route)
		case r.Weight <= 0:
			return fmt.Errorf("load.routes[%d].weight is %d (must be > 0)", i, r.Weight)
		}
		seen[r.Route] = true
	}
	return nil
}

func (l Load) hasRoute(route string) bool {
	for _, r := range l.Routes {
		if r.Route == route {
			return true
		}
	}
	return false
}

func (sc Scenario) validateSymptom() error {
	sy := sc.Symptom
	switch {
	case sy.Service == "":
		return fmt.Errorf("symptom.service is empty")
	case sy.Method == "":
		return fmt.Errorf("symptom.method is empty")
	case strings.TrimSpace(sy.Route) == "":
		return fmt.Errorf("symptom.route is empty")
	case sy.Bad == "":
		return fmt.Errorf("symptom.bad is empty")
	}
	if !sc.Load.hasRoute(sy.Route) {
		return fmt.Errorf("symptom.route %q is not a route of the load", sy.Route)
	}
	if !sc.hasLabel(sy.Bad) {
		return fmt.Errorf("symptom.bad %q is not a commit label", sy.Bad)
	}
	if sy.Baseline != "" && !sc.hasLabel(sy.Baseline) {
		return fmt.Errorf("symptom.baseline %q is not a commit label", sy.Baseline)
	}
	return nil
}

func (sc Scenario) hasLabel(label string) bool {
	for _, c := range sc.Commits {
		if c.Label == label {
			return true
		}
	}
	return false
}
