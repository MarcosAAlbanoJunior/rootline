package recording

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

// Record copies the scenario's symptom block into the manifest.
func TestBuildManifest_CopiesTheScenarioSymptomIntoTheManifest(t *testing.T) {
	sym := fixture.Symptom{Service: "svc-x", Method: "POST", RouteSource: "http_route", Bad: "ruim", Baseline: "boa"}
	sc := fixture.Scenario{Name: "x", Symptom: sym}
	gen := fixture.Generated{Commits: []fixture.Commit{{Label: "boa", SHA: "sha-boa"}, {Label: "ruim", SHA: "sha-ruim"}}}

	m := buildManifest(sc, gen, LoadConfig{Route: "/things/{id}"})

	if m.Symptom != sym {
		t.Fatalf("Symptom = %+v, want %+v", m.Symptom, sym)
	}
	if len(m.Commits) != 2 || m.Commits[1] != (CommitRef{Label: "ruim", SHA: "sha-ruim"}) {
		t.Fatalf("Commits = %+v", m.Commits)
	}
}
