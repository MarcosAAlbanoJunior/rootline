package recording_test

import (
	"encoding/json"
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
		Load: recording.LoadConfig{Requests: 200, Rate: 50, Routes: []recording.LoadRoute{{Route: "/customers/{id}/orders", Weight: 1}}},
	}
}

func TestManifest_RoundTrip(t *testing.T) {
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

func TestManifest_Contents(t *testing.T) {
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

func TestManifest_LoadIsWrittenWithItsRoutesAndWeights(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	m := sampleManifest()
	m.Load = recording.LoadConfig{Requests: 8, Rate: 4, Routes: []recording.LoadRoute{{Route: "/a/{id}", Weight: 3}, {Route: "/b", Weight: 1}}}
	if err := recording.WriteManifest(path, m); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Load map[string]any `json:"load"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"requests": 8.0, "rate_per_second": 4.0,
		"routes": []any{
			map[string]any{"route": "/a/{id}", "weight": 3.0},
			map[string]any{"route": "/b", "weight": 1.0},
		},
	}
	if !reflect.DeepEqual(doc.Load, want) {
		t.Fatalf("load = %v, want %v", doc.Load, want)
	}
}
