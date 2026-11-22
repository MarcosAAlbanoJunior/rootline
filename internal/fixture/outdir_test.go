package fixture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

// AC16: a non-empty output directory fails with a clear message; nothing is reused or touched.
func TestGenerateRefusesANonEmptyOutDir(t *testing.T) {
	source, scenario := setup(t)
	out := t.TempDir()
	leftover := filepath.Join(out, "old-recording.txt")
	if err := os.WriteFile(leftover, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := fixture.Generate(fixture.Options{ScenarioDir: scenario, SourceDir: source, OutDir: out})
	if err == nil || !strings.Contains(err.Error(), "not empty") || !strings.Contains(err.Error(), out) {
		t.Fatalf("want a clear 'not empty' error naming the directory, got %v", err)
	}
	if data, rerr := os.ReadFile(leftover); rerr != nil || string(data) != "keep me" {
		t.Fatalf("existing content must be left untouched: %v %q", rerr, data)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 {
		t.Fatalf("nothing may be written into the directory, found %d entries", len(entries))
	}
}

func TestGenerateAcceptsAnExistingEmptyOutDir(t *testing.T) {
	source, scenario := setup(t)
	out := t.TempDir()
	if _, err := fixture.Generate(fixture.Options{ScenarioDir: scenario, SourceDir: source, OutDir: out}); err != nil {
		t.Fatal(err)
	}
}
