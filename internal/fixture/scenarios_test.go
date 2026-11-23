// Every scenario under eval/fixtures/scenarios generates, so a patch broken by a change in the
// demo-app fails go test (and the local gate), not only the recording in CI.
package fixture_test

import (
	"os"
	"os/exec"
	"path/filepath"
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
