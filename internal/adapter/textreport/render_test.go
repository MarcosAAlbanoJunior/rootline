package textreport_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
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
			analysis.TimeBreakdownEvidence{ID: "E2", Kind: "time_breakdown", BaselineMeanMs: 4.4, BadMeanMs: 145.6,
				BaselineTraces: 40, BadTraces: 40, BaselineMeanTraces: 38, BadMeanTraces: 38,
				Dependencies: []analysis.DependencyTime{
					{Dependency: "http:moderation:8090", Kind: "http", Explained: 0.99,
						Baseline:   analysis.VersionTime{MeanMs: 3.5, Presence: 1, CallsPerTrace: 1, MsPerCall: 3.5},
						Bad:        analysis.VersionTime{MeanMs: 143.3, Presence: 1, CallsPerTrace: 1, MsPerCall: 143.3},
						Operations: []analysis.OperationPresence{{Operation: "POST", BaselinePresence: 1, BadPresence: 1}}},
					{Dependency: "db:postgresql", Kind: "db", Explained: 0.01,
						Baseline: analysis.VersionTime{MeanMs: 0.6, Presence: 1, CallsPerTrace: 2, MsPerCall: 0.3},
						Bad:      analysis.VersionTime{MeanMs: 0.7, Presence: 1, CallsPerTrace: 2, MsPerCall: 0.35},
						Operations: []analysis.OperationPresence{
							{Operation: "SELECT id FROM rooms WHERE id = $1", BaselinePresence: 1, BadPresence: 1},
							{Operation: "INSERT INTO messages (room_id, body) VALUES ($1, $2)", BaselinePresence: 1, BadPresence: 1},
							{Operation: "SELECT 1", BaselinePresence: 0.5, BadPresence: 0.5},
							{Operation: "SELECT 2", BaselinePresence: 0.1, BadPresence: 0.1},
						}},
					{Dependency: "own_time", Kind: "own", Explained: 0, Operations: []analysis.OperationPresence{},
						Baseline: analysis.VersionTime{MeanMs: 0.3, Presence: 1},
						Bad:      analysis.VersionTime{MeanMs: 0.3, Presence: 1}},
				}},
		},
		Observations: []string{},
		Checks: []analysis.Check{
			{ID: "n_plus_one", Status: analysis.CheckSkipped, Reason: "no database query spans in the 40 analyzed traces", Hint: "See the db_spans check."},
			{ID: "missing_index", Status: analysis.CheckSkipped, Reason: "no query plans", Hint: "Plans come from the evaluation today."},
			{ID: "trace_shape", Status: analysis.CheckRan, Reason: "mean per trace 4.4 → 145.6 ms (+141.2 ms, 38 and 38 traces); dominant: http:moderation:8090, 99% of the difference; nothing promoted"},
			{ID: "commit_correlation", Status: analysis.CheckNotApplicable, Reason: "no candidate to correlate"},
		},
		Warnings: []string{"no traces of the operation in baseline version 2222222"},
	})
}

func ptr(v float64) *float64 { return &v }

func TestRenderSymptoms_TableMarksSuggestionAndCommand(t *testing.T) {
	var got bytes.Buffer
	rep := analysis.SymptomsReport{
		Service: "api", BaselineVersion: baseSHA, BadVersion: badSHA, VersionOrder: analysis.OrderTime, Generalized: true,
		Notices: []string{"The baseline is 222222222222 because it appeared first in the traces."},
		Operations: []analysis.OperationSymptom{
			{OperationRef: analysis.OperationRef{Method: "GET", Route: "/orders"}, EnoughSample: true, Regression: analysis.RegressionLatency,
				Baseline: analysis.VersionStats{N: 40, P50: 10, P95: 12}, Bad: analysis.VersionStats{N: 38, P50: 100, P95: 130, ErrorRate: 0.05},
				NRatio: ptr(0.95), P50Ratio: ptr(10), P95Ratio: ptr(10.83), OnsetS: ptr(12.5), CeilingMs: ptr(10000)},
			{OperationRef: analysis.OperationRef{Method: "GET", Route: "/health"}, EnoughSample: false,
				Baseline: analysis.VersionStats{N: 40, P50: 1, P95: 1}, Bad: analysis.VersionStats{N: 1, P50: 2, P95: 2},
				NRatio: ptr(0.025), P50Ratio: ptr(2), P95Ratio: ptr(2), NDrop: true},
		},
		Suggested: &analysis.OperationRef{Method: "GET", Route: "/orders"},
	}
	if err := textreport.RenderSymptoms(&got, rep, "rootline analyze --operation 'GET /orders'"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "symptoms.md")
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

func TestRenderSymptoms_UnevenLoadSaysWhyThereIsNoSuggestion(t *testing.T) {
	var got bytes.Buffer
	rep := analysis.SymptomsReport{
		Service: "api", BaselineVersion: baseSHA, BadVersion: badSHA, VersionOrder: analysis.OrderGit,
		MedianNRatio: ptr(0.51), UnevenLoad: true,
		Operations: []analysis.OperationSymptom{
			{OperationRef: analysis.OperationRef{Method: "GET", Route: "/orders"}, EnoughSample: true, Regression: analysis.RegressionLatency,
				Baseline: analysis.VersionStats{N: 80, P50: 10, P95: 12}, Bad: analysis.VersionStats{N: 40, P50: 100, P95: 130},
				NRatio: ptr(0.5), P50Ratio: ptr(10), P95Ratio: ptr(10.83)},
		},
	}
	if err := textreport.RenderSymptoms(&got, rep, "rootline analyze --operation 'GET /orders'"); err != nil {
		t.Fatal(err)
	}
	text := got.String()
	for _, want := range []string{"No operation is suggested: the load of the two recordings was uneven", "median n ratio 0.51", "0.8 to 1.25", "same load"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "rootline analyze") || strings.Contains(text, "none regressed") {
		t.Errorf("uneven load: want no command and no other reason:\n%s", text)
	}
}

func TestRenderSymptoms_UnevenLoadWithoutRegressionSaysNoneRegressed(t *testing.T) {
	var got bytes.Buffer
	rep := analysis.SymptomsReport{
		Service: "api", BaselineVersion: baseSHA, BadVersion: badSHA, VersionOrder: analysis.OrderGit,
		MedianNRatio: ptr(0.5), UnevenLoad: true,
		Operations: []analysis.OperationSymptom{
			{OperationRef: analysis.OperationRef{Method: "GET", Route: "/orders"}, EnoughSample: true,
				Baseline: analysis.VersionStats{N: 80, P50: 10, P95: 12}, Bad: analysis.VersionStats{N: 40, P50: 10, P95: 12},
				NRatio: ptr(0.5), P50Ratio: ptr(1), P95Ratio: ptr(1)},
		},
	}
	if err := textreport.RenderSymptoms(&got, rep, ""); err != nil {
		t.Fatal(err)
	}
	text := got.String()
	for _, want := range []string{"none regressed", "median n ratio 0.50"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "do not tell which operation regressed") {
		t.Errorf("no operation regressed: the text must not imply one did:\n%s", text)
	}
}

func TestRender_ADependencyThatShrankALittleIsNotMinusZeroPercent(t *testing.T) {
	var got bytes.Buffer
	err := textreport.Render(&got, analysis.Result{Symptom: symptom(), Evidence: []any{
		analysis.TimeBreakdownEvidence{ID: "E1", Kind: "time_breakdown", Dependencies: []analysis.DependencyTime{
			{Dependency: "db:postgresql", Kind: "db", Explained: -0.001},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if s := got.String(); strings.Contains(s, "-0%") || !strings.Contains(s, "explains 0%") {
		t.Errorf("want explains 0%%:\n%s", s)
	}
}
