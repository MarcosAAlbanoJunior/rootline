// Package fixture generates the git repository of an evaluation scenario: real commits whose
// answer key is computed, never typed (ADR-0004).
package fixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// Scenario is the content of scenario.json.
type Scenario struct {
	Name    string           `json:"name"`
	Commits []ScenarioCommit `json:"commits"`
	Answer  ScenarioAnswer   `json:"answer"`
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
	return sc, nil
}
