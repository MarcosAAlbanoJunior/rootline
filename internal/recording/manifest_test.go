// Expected API (package recording):
//
//	const SeedID = "deterministic-sql-v1"
//	type CommitRef struct { Label, SHA string }
//	type LoadConfig struct { Requests, Rate int; Route string }
//	type Manifest struct {
//	    Scenario  string
//	    Commits   []CommitRef
//	    AnswerKey fixture.AnswerKey // with computed line
//	    Seed      string            // SeedID
//	    Load      LoadConfig
//	}
//	func WriteManifest(path string, m Manifest) error
//	func ReadManifest(path string) (Manifest, error)
package recording_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
	"github.com/MarcosAAlbanoJunior/rootline/internal/recording"
)

func sampleManifest() recording.Manifest {
	return recording.Manifest{
		Scenario: "n1-basic",
		Commits: []recording.CommitRef{
			{Label: "referencia", SHA: "1111111111111111111111111111111111111111"},
			{Label: "regressao", SHA: "2222222222222222222222222222222222222222"},
		},
		AnswerKey: fixture.AnswerKey{
			Cause: "n_plus_one", Function: "example.com/demo-app/internal/orders.(*Service).List",
			File: "demo-app/internal/orders/service.go", Marker: " // rootline:answer",
			CommitLabel: "regressao", Line: 42,
		},
		Seed: recording.SeedID,
		Load: recording.LoadConfig{Requests: 200, Rate: 50, Route: "/customers/{id}/orders"},
	}
}

func TestAC17_ManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	in := sampleManifest()
	if err := recording.WriteManifest(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := recording.ReadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip lost data:\n in: %+v\nout: %+v", in, out)
	}
}

func TestAC17_ManifestContents(t *testing.T) {
	if recording.SeedID != "deterministic-sql-v1" {
		t.Fatalf("SeedID = %q", recording.SeedID)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := recording.WriteManifest(path, sampleManifest()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"n1-basic", "referencia", "regressao",
		"1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222",
		"deterministic-sql-v1", "/customers/{id}/orders", "n_plus_one", "service.go",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("manifest.json lacks %q:\n%s", want, raw)
		}
	}
}
