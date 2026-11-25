package textreport_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/textreport"
	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name string, r analysis.Result) {
	t.Helper()
	var got bytes.Buffer
	if err := textreport.Render(&got, r); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("text differs from %s (rerun with -update to accept):\n%s", path, got.String())
	}
}

const (
	badSHA  = "1111111111111111111111111111111111111111"
	baseSHA = "2222222222222222222222222222222222222222"
	fixSHA  = "3333333333333333333333333333333333333333"
)

func symptom() analysis.SymptomInfo {
	base := baseSHA
	return analysis.SymptomInfo{
		Service:         "demo-app",
		Operation:       analysis.OperationInfo{Method: "GET", Route: "/customers/{id}/orders", RouteSource: "attribute"},
		BadVersion:      badSHA,
		BaselineVersion: &base,
	}
}

func TestRender_ConclusiveResult(t *testing.T) {
	inLoop := true
	loc := &analysis.Location{Level: 1, Function: "example.com/demo-app/internal/orders.(*Service).List",
		File: "demo-app/internal/orders/service.go", Line: 42, InLoop: &inLoop}
	golden(t, "conclusive.md", analysis.Result{
		SchemaVersion: analysis.SchemaVersion, Mode: analysis.ModeDetectors, Symptom: symptom(),
		Status: analysis.StatusCauseFound,
		Cause: &analysis.Cause{CandidateID: "C1", Type: analysis.TypeNPlusOne, Confidence: "high", Location: loc,
			Commit: &analysis.CommitRef{SHA: fixSHA, Touch: "location"}},
		Candidates: []analysis.Candidate{{ID: "C1", Type: analysis.TypeNPlusOne, Score: 1.75, Confidence: "high",
			Location: loc, Evidence: []string{"E1", "E2"}}},
		Evidence: []any{
			analysis.RepeatedQueryEvidence{ID: "E1", Kind: "repeated_query", Query: "SELECT * FROM items WHERE order_id = $1",
				GroupSizeMedian: 10, RowsMedian: 3, PlusOneMatchedRatio: 1, TracesAffected: 40, TracesTotal: 40},
			analysis.StackTouchEvidence{ID: "E2", Kind: "stack_touch", Commit: fixSHA,
				Function: "example.com/demo-app/internal/orders.(*Service).List", Strength: "location"},
			analysis.SymptomStatsEvidence{ID: "E3", Kind: "symptom_stats", Version: badSHA, Traces: 40, DurationMsMedian: 12.5, DBQuerySpansMedian: 11},
		},
		Observations: []string{},
		Checks: []analysis.Check{
			{ID: "n_plus_one", Status: analysis.CheckRan, Reason: "1 candidate(s) in 40 traces"},
			{ID: "missing_index", Status: analysis.CheckSkipped, Reason: "no query plans", Hint: "Plans come from the evaluation today."},
			{ID: "commit_correlation", Status: analysis.CheckRan, Reason: "touched: C1 by commit 3333333 (location)"},
		},
		Warnings: []string{},
	})
}

func TestRender_InconclusiveResultListsSkippedChecksFirst(t *testing.T) {
	golden(t, "inconclusive.md", analysis.Result{
		SchemaVersion: analysis.SchemaVersion, Mode: analysis.ModeDetectors, Symptom: symptom(),
		Status: analysis.StatusInconclusive, Candidates: []analysis.Candidate{},
		Evidence: []any{
			analysis.SymptomStatsEvidence{ID: "E1", Kind: "symptom_stats", Version: badSHA, Traces: 40, DurationMsMedian: 950, DBQuerySpansMedian: 0},
		},
		Observations: []string{},
		Checks: []analysis.Check{
			{ID: "n_plus_one", Status: analysis.CheckSkipped, Reason: "no database query spans in the 40 analyzed traces", Hint: "See the db_spans check."},
			{ID: "missing_index", Status: analysis.CheckSkipped, Reason: "no query plans", Hint: "Plans come from the evaluation today."},
			{ID: "commit_correlation", Status: analysis.CheckNotApplicable, Reason: "no candidate to correlate"},
		},
		Warnings: []string{"no traces of the operation in baseline version 2222222"},
	})
}
