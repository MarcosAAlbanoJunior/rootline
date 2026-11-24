package main

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
	"github.com/MarcosAAlbanoJunior/rootline/internal/recording"
)

func TestScoreManifest_TheTableOfTheCauseIsScored(t *testing.T) {
	m := recording.Manifest{
		Scenario: "idx",
		Commits:  []recording.CommitRef{{Label: "referencia", SHA: "sha-ref"}, {Label: "regressao", SHA: "sha-reg"}},
		AnswerKey: fixture.AnswerKey{Cause: "missing_index", Function: "f", Target: "orders",
			File: "a.go", Line: 30, CommitLabel: "regressao"},
		Symptom: fixture.Symptom{Service: "demo-app", Method: "GET", Route: "/x", RouteSource: "span_name",
			Bad: "regressao", Baseline: "referencia"},
	}
	for _, tc := range []struct {
		table string
		want  bool
	}{{"orders", true}, {"order_items", false}} {
		analyze := func(string, analysis.Symptom) (analysis.Result, error) {
			loc := &analysis.Location{Level: 1, Function: "f", File: "a.go", Line: 30}
			return analysis.Result{
				Cause: &analysis.Cause{Type: "missing_index", Target: tc.table, Location: loc,
					Commit: &analysis.CommitRef{SHA: "sha-reg"}},
				Candidates: []analysis.Candidate{{Type: "missing_index", Target: tc.table, Location: loc}},
			}, nil
		}
		o, err := scoreManifest("rec", m, analyze)
		if err != nil {
			t.Fatal(err)
		}
		if o.Cause != tc.want {
			t.Errorf("table %s: Cause = %v, want %v", tc.table, o.Cause, tc.want)
		}
	}
}
