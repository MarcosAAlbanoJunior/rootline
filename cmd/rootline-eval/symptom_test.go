// scoreManifest takes the analyzer as a parameter, so the symptom seam is tested without a git
// repository.
package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
	"github.com/MarcosAAlbanoJunior/rootline/internal/recording"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func manifestWith(sym fixture.Symptom) recording.Manifest {
	return recording.Manifest{
		Scenario: "x",
		Commits: []recording.CommitRef{
			{Label: "boa", SHA: "sha-boa"},
			{Label: "ruim", SHA: "sha-ruim"},
		},
		Load:    recording.LoadConfig{Requests: 1, Rate: 1, Routes: []recording.LoadRoute{{Route: "/load/{id}", Weight: 1}}},
		Symptom: sym,
	}
}

func TestSymptomFromManifest_SymptomComesFromTheManifest(t *testing.T) {
	m := manifestWith(fixture.Symptom{Service: "svc-x", Method: "POST", Route: "/things/{id}", RouteSource: "http_route", Bad: "ruim", Baseline: "boa"})
	got, err := symptomFromManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if got.Service != "svc-x" || got.Operation.Method != "POST" || got.Operation.RouteSource != "http_route" ||
		got.Operation.Route != "/things/{id}" || got.BadVersion != "sha-ruim" || got.BaselineVersion != "sha-boa" {
		t.Fatalf("Symptom = %+v", got)
	}
}

func TestSymptomFromManifest_EmptyBaselineGivesEmptyBaselineVersion(t *testing.T) {
	m := manifestWith(fixture.Symptom{Service: "svc-x", Method: "GET", Route: "/things/{id}", RouteSource: "span_name", Bad: "ruim"})
	got, err := symptomFromManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if got.BaselineVersion != "" || got.BadVersion != "sha-ruim" {
		t.Fatalf("Symptom = %+v", got)
	}
}

// The analyzed route comes from symptom.route, never from the load (which may send several routes).
func TestSymptomFromManifest_RouteComesFromTheSymptomNotFromTheLoad(t *testing.T) {
	m := manifestWith(fixture.Symptom{Service: "svc-x", Method: "GET", Route: "/things/{id}", RouteSource: "span_name", Bad: "ruim"})
	got, err := symptomFromManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if got.Operation.Route != "/things/{id}" {
		t.Fatalf("Route = %q, want the symptom route %q", got.Operation.Route, "/things/{id}")
	}
}

// A recording made before the symptom had a route has no default: the error says to record again.
func TestScoreOne_ManifestWithoutSymptomRouteIsAnErrorCitingTheFieldPathAndRecordAgain(t *testing.T) {
	rec := t.TempDir()
	m := manifestWith(fixture.Symptom{Service: "svc-x", Method: "GET", RouteSource: "span_name", Bad: "ruim"})
	if err := recording.WriteManifest(filepath.Join(rec, "manifest.json"), m); err != nil {
		t.Fatal(err)
	}
	_, err := scoreOne(rec)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"symptom.route", rec, "record again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to cite %q", err, want)
		}
	}
}

func TestScoreOne_ManifestWithoutSymptomIsAnErrorCitingSymptomAndPath(t *testing.T) {
	rec := t.TempDir()
	m := manifestWith(fixture.Symptom{})
	if err := recording.WriteManifest(filepath.Join(rec, "manifest.json"), m); err != nil {
		t.Fatal(err)
	}
	_, err := scoreOne(rec)
	if err == nil || !strings.Contains(err.Error(), "symptom") || !strings.Contains(err.Error(), rec) {
		t.Fatalf("err = %v, want an error citing %q and %q", err, "symptom", rec)
	}
}

// Scoring a recording uses only the manifest. With service, method, route source and labels
// unlike the n1-basic ones, the analysis receives those values and the Result is scored against
// the manifest's own labels.
func TestScoreManifest_UsesTheManifestSymptomAndLabels(t *testing.T) {
	m := manifestWith(fixture.Symptom{Service: "svc-x", Method: "POST", Route: "/things/{id}", RouteSource: "http_route", Bad: "ruim", Baseline: "boa"})
	m.AnswerKey = fixture.AnswerKey{Cause: "n_plus_one", Function: "f", File: "a.go", Line: 7, CommitLabel: "ruim"}

	var got analysis.Symptom
	analyze := func(rec string, s analysis.Symptom) (analysis.Result, error) {
		got = s
		// The fake blames the version it was told is bad, as the real analysis would.
		return analysis.Result{Cause: &analysis.Cause{
			Type:     "n_plus_one",
			Location: &analysis.Location{Level: 1, Function: "f", File: "a.go", Line: 7},
			Commit:   &analysis.CommitRef{SHA: s.BadVersion, Touch: "location"},
		}}, nil
	}

	o, err := scoreManifest("rec", m, analyze)
	if err != nil {
		t.Fatal(err)
	}
	want := analysis.Symptom{
		Service:         "svc-x",
		Operation:       trace.Operation{Method: "POST", Route: "/things/{id}", RouteSource: "http_route"},
		BadVersion:      "sha-ruim",
		BaselineVersion: "sha-boa",
	}
	if got != want {
		t.Fatalf("analysis got Symptom %+v, want %+v", got, want)
	}
	if !o.Cause || !o.Line || !o.Commit || !o.Strict {
		t.Fatalf("outcome = %+v, want all hits (commit scored as label %q)", o, "ruim")
	}
}

// The scoring error names the recording directory exactly once.
func TestScoreErrorNamesTheRecordingOnce(t *testing.T) {
	rec := t.TempDir()
	if err := recording.WriteManifest(filepath.Join(rec, "manifest.json"), manifestWith(fixture.Symptom{})); err != nil {
		t.Fatal(err)
	}
	_, err := scoreRecordings(rec)
	if err == nil {
		t.Fatal("want an error")
	}
	if n := strings.Count(err.Error(), rec); n != 1 {
		t.Fatalf("error names %q %d times: %v", rec, n, err)
	}
}

func TestFoundFromResult_CarriesTheLocationLevel(t *testing.T) {
	cases := []struct {
		name string
		res  analysis.Result
		want int
	}{
		{"level 1", resultWithLocation(&analysis.Location{Level: 1, Function: "f", File: "a.go", Line: 1}), 1},
		{"level 2", resultWithLocation(&analysis.Location{Level: 2, Function: "h", File: "h.go", Approximate: true}), 2},
		{"no location", resultWithLocation(nil), 0},
		{"no cause", analysis.Result{}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := foundFromResult(c.res).Level; got != c.want {
				t.Fatalf("Level = %d, want %d", got, c.want)
			}
		})
	}
}

func TestScore_OutputShowsTheLevelDistribution(t *testing.T) {
	sc := evaluation.Score{Cause: oneOfOne, Line: oneOfOne, Commit: oneOfOne, Strict: oneOfOne,
		LineLevels: evaluation.LineLevels{Level1: 3, Level2: 2, None: 1}}
	sb := evaluation.Scoreboard{"detectors_only": sc}
	placar := writeBoard(t, sb)

	code, stdout, stderr := runScore(t, fixedScorer(sb, nil), "--recordings", "r", "--placar", placar)
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if want := "  Níveis da Linha: 1=3 2=2 ausente=1"; !strings.Contains(stdout, want) {
		t.Fatalf("stdout must contain %q, got %q", want, stdout)
	}
}
