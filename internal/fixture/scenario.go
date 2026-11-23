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

// ScenarioCommit is one commit of a scenario. The first commit has no patch; the others apply
// Patch (a file in the scenario directory).
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

// Scenario is the content of scenario.json. Assumption is the detector or location assumption the
// scenario breaks; a scenario that breaks none only inflates the scoreboard, so it is required.
type Scenario struct {
	Name       string           `json:"name"`
	Assumption string           `json:"assumption"`
	Commits    []ScenarioCommit `json:"commits"`
	Answer     ScenarioAnswer   `json:"answer"`
	Symptom    Symptom          `json:"symptom"`
}

// LoadScenario reads <dir>/scenario.json.
func LoadScenario(dir string) (Scenario, error) {
	data, err := os.ReadFile(filepath.Join(dir, "scenario.json"))
	if err != nil {
		return Scenario{}, err
	}
	var sc Scenario
	if err := json.Unmarshal(data, &sc); err != nil {
		return Scenario{}, fmt.Errorf("fixture: scenario.json: %w", err)
	}
	if sc.Name == "" || len(sc.Commits) == 0 || sc.Answer.Marker == "" || sc.Answer.File == "" {
		return Scenario{}, fmt.Errorf("fixture: scenario.json in %s is incomplete", dir)
	}
	if strings.TrimSpace(sc.Assumption) == "" {
		return Scenario{}, fmt.Errorf("fixture: scenario.json in %s: assumption is empty (declare which assumption the scenario breaks)", dir)
	}
	if err := sc.validateSymptom(); err != nil {
		return Scenario{}, fmt.Errorf("fixture: scenario.json in %s: %w", dir, err)
	}
	return sc, nil
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
