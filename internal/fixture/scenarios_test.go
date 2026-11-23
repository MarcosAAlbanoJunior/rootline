// Every scenario under eval/fixtures/scenarios generates, so a patch broken by a change in the
// demo-app fails go test (and the local gate), not only the recording in CI.
package fixture_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

func TestGenerate_EveryScenarioInTheRepositoryGenerates(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := filepath.Join("..", "..")
	scenariosDir := filepath.Join(root, "eval", "fixtures", "scenarios")
	entries, err := os.ReadDir(scenariosDir)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		found++
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := fixture.Generate(fixture.Options{
				ScenarioDir: filepath.Join(scenariosDir, name),
				SourceDir:   root,
				OutDir:      filepath.Join(t.TempDir(), "out"),
			})
			if err != nil {
				t.Fatalf("scenario %s does not generate: %v", name, err)
			}
		})
	}
	if found == 0 {
		t.Fatalf("no scenario directories under %s", scenariosDir)
	}
}

const demoModule = "github.com/MarcosAAlbanoJunior/rootline/demo-app"

// What each scenario of the repository declares: the symptom route, the load and the answer key.
func TestLoadScenario_RepositoryScenariosDeclareTheirSymptomLoadAndAnswer(t *testing.T) {
	cases := []struct {
		dir, route, function, file string
	}{
		{"n1-helper", "/customers/{id}/receipts", demoModule + "/internal/receipts.(*Service).Receipts", "demo-app/internal/receipts/service.go"},
		// The declared function, without the -rangeN suffix of the range-over-func body (ADR-0016).
		{"n1-rangefunc", "/customers/{id}/history", demoModule + "/internal/history.(*Service).History", "demo-app/internal/history/service.go"},
	}
	for _, c := range cases {
		t.Run(c.dir, func(t *testing.T) {
			sc, err := fixture.LoadScenario(filepath.Join("..", "..", "eval", "fixtures", "scenarios", c.dir))
			if err != nil {
				t.Fatal(err)
			}
			if sc.Name != c.dir {
				t.Errorf("Name = %q, want %q", sc.Name, c.dir)
			}
			wantSymptom := fixture.Symptom{Service: "demo-app", Method: "GET", Route: c.route, RouteSource: "span_name", Bad: "regressao", Baseline: "referencia"}
			if sc.Symptom != wantSymptom {
				t.Errorf("Symptom = %+v, want %+v", sc.Symptom, wantSymptom)
			}
			if len(sc.Load.Routes) != 1 || sc.Load.Routes[0] != (fixture.LoadRoute{Route: c.route, Weight: 1}) {
				t.Errorf("Load.Routes = %+v, want only %s with weight 1", sc.Load.Routes, c.route)
			}
			wantAnswer := fixture.ScenarioAnswer{Cause: "n_plus_one", Function: c.function, File: c.file, Marker: " // rootline:answer", Commit: "regressao"}
			if sc.Answer != wantAnswer {
				t.Errorf("Answer = %+v, want %+v", sc.Answer, wantAnswer)
			}
			wantCommits := []fixture.ScenarioCommit{{Label: "referencia", Patch: "preparacao.patch"}, {Label: "regressao", Patch: "regressao.patch"}}
			if !slices.Equal(sc.Commits, wantCommits) {
				t.Errorf("Commits = %+v, want %+v", sc.Commits, wantCommits)
			}
		})
	}
}
