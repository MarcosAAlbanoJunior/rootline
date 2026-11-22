// Package recording records an evaluation scenario: it generates the fixture repo, builds and
// runs each version of the demo-app under a fixed load and keeps the traces (ADR-0008).
package recording

import (
	"encoding/json"
	"os"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

// SeedID identifies the deterministic seed of the demo-app database.
const SeedID = "deterministic-sql-v1"

// CommitRef maps a scenario label to the SHA of the generated commit.
type CommitRef struct {
	Label string `json:"label"`
	SHA   string `json:"sha"`
}

// LoadConfig is the fixed load sent to each version.
type LoadConfig struct {
	Requests int    `json:"requests"`
	Rate     int    `json:"rate_per_second"`
	Route    string `json:"route"`
}

// Manifest describes a recording. The answer key lives here, never in the fixture repo.
type Manifest struct {
	Scenario  string            `json:"scenario"`
	Commits   []CommitRef       `json:"commits"`
	AnswerKey fixture.AnswerKey `json:"answer_key"`
	Seed      string            `json:"seed"`
	Load      LoadConfig        `json:"load"`
	Symptom   fixture.Symptom   `json:"symptom"`
}

// WriteManifest writes m as indented JSON.
func WriteManifest(path string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// ReadManifest reads a manifest written by WriteManifest.
func ReadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
