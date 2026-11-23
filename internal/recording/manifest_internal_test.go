package recording

import (
	"reflect"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

// Record copies the scenario's symptom block into the manifest.
func TestBuildManifest_CopiesTheScenarioSymptomIntoTheManifest(t *testing.T) {
	sym := fixture.Symptom{Service: "svc-x", Method: "POST", Route: "/things/{id}", RouteSource: "http_route", Bad: "ruim", Baseline: "boa"}
	sc := fixture.Scenario{Name: "x", Symptom: sym}
	gen := fixture.Generated{Commits: []fixture.Commit{{Label: "boa", SHA: "sha-boa"}, {Label: "ruim", SHA: "sha-ruim"}}}

	m := buildManifest(sc, gen)

	if m.Symptom != sym {
		t.Fatalf("Symptom = %+v, want %+v", m.Symptom, sym)
	}
	if len(m.Commits) != 2 || m.Commits[1] != (CommitRef{Label: "ruim", SHA: "sha-ruim"}) {
		t.Fatalf("Commits = %+v", m.Commits)
	}
}

// Record writes the scenario's load into the manifest, with its routes and weights.
func TestBuildManifest_WritesTheScenarioLoad(t *testing.T) {
	sc := fixture.Scenario{Name: "x", Load: fixture.Load{Requests: 8, RatePerSecond: 4,
		Routes: []fixture.LoadRoute{{Route: "/a/{id}", Weight: 3}, {Route: "/b", Weight: 1}}}}

	m := buildManifest(sc, fixture.Generated{})

	want := LoadConfig{Requests: 8, Rate: 4, Routes: []LoadRoute{{Route: "/a/{id}", Weight: 3}, {Route: "/b", Weight: 1}}}
	if !reflect.DeepEqual(m.Load, want) {
		t.Fatalf("Load = %+v, want %+v", m.Load, want)
	}
}
