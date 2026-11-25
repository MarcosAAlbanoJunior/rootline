package analysis_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// check returns the Verificação id of the Result, failing the test when it is absent.
func check(t *testing.T, m map[string]any, id string) map[string]any {
	t.Helper()
	for _, c := range list(m, "checks") {
		if cm := obj(t, c); cm["id"] == id {
			return cm
		}
	}
	t.Fatalf("no check %q in %v", id, m["checks"])
	return nil
}

func wantStatus(t *testing.T, c map[string]any, status string) {
	t.Helper()
	if c["status"] != status {
		t.Errorf("check %v: status %v, want %s (reason %q)", c["id"], c["status"], status, c["reason"])
	}
}

func contains(t *testing.T, c map[string]any, field, sub string) {
	t.Helper()
	s, _ := c[field].(string)
	if !strings.Contains(s, sub) {
		t.Errorf("check %v: %s = %q, want it to contain %q", c["id"], field, s, sub)
	}
}

// serverOnlyTrace has no database span at all.
func serverOnlyTrace(id string) []trace.Span { return []trace.Span{server(id, bad)} }

func TestResult_SchemaVersionIsOneAndEveryCheckHasTheFourFields(t *testing.T) {
	m := run(t, allSpans(4, 4, badTrace), fakeGit{}, symptom)
	if m["schema_version"] != "1" {
		t.Errorf("schema_version = %v, want 1", m["schema_version"])
	}
	for _, c := range list(m, "checks") {
		cm := obj(t, c)
		for _, k := range []string{"id", "status", "reason", "hint"} {
			if _, ok := cm[k]; !ok {
				t.Errorf("check %v has no %q", cm, k)
			}
		}
	}
}

func TestNPlusOneCheck_RanAndSaysWhatItFound(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, badTrace), fakeGit{}, symptom), "n_plus_one")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "1 candidate")
}

func TestNPlusOneCheck_SkippedWithoutDatabaseQuerySpans(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, serverOnlyTrace), fakeGit{}, symptom), "n_plus_one")
	wantStatus(t, c, analysis.CheckSkipped)
	contains(t, c, "reason", "no database query spans")
}

func TestNPlusOneCheck_NotApplicableWithoutTracesOfTheOperation(t *testing.T) {
	s := symptom
	s.BadVersion = "fffffff"
	c := check(t, run(t, allSpans(4, 4, badTrace), fakeGit{}, s), "n_plus_one")
	wantStatus(t, c, analysis.CheckNotApplicable)
}

func TestMissingIndexCheck_SkippedWithoutPlansSaysWherePlansComeFrom(t *testing.T) {
	m := analyzeJSON(t, allSpans(4, 4, singleQueryTrace), nil)
	c := check(t, m, "missing_index")
	wantStatus(t, c, analysis.CheckSkipped)
	contains(t, c, "reason", "no query plans")
	contains(t, c, "hint", "evaluation")
	contains(t, c, "hint", "roadmap")
	for _, w := range list(m, "warnings") {
		if strings.HasPrefix(w.(string), "missing_index_skipped") {
			t.Errorf("warnings = %v, want the missing-index notice only in checks", m["warnings"])
		}
	}
}

func TestMissingIndexCheck_RanKeepsThePerQueryReasons(t *testing.T) {
	plans := fakePlans{items: {Unexplainable: "not a single SELECT"}}
	c := check(t, analyzeJSON(t, allSpans(4, 4, badTrace), plans), "missing_index")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", orders+": no recorded plan")
	contains(t, c, "reason", items+": not a single SELECT")
}

func TestMissingIndexCheck_SkippedWithoutDatabaseQuerySpans(t *testing.T) {
	c := check(t, analyzeJSON(t, allSpans(4, 4, serverOnlyTrace), fakePlans{}), "missing_index")
	wantStatus(t, c, analysis.CheckSkipped)
}

func TestCommitCorrelationCheck_NotApplicableWithoutBaseline(t *testing.T) {
	s := symptom
	s.BaselineVersion = ""
	c := check(t, run(t, allSpans(4, 4, badTrace), fakeGit{}, s), "commit_correlation")
	wantStatus(t, c, analysis.CheckNotApplicable)
	contains(t, c, "reason", "no baseline")
}

func TestCommitCorrelationCheck_SkippedWhenAVersionIsNotInTheRepository(t *testing.T) {
	m := run(t, allSpans(4, 4, badTrace), fakeGit{missing: map[string]bool{baseline: true}}, symptom)
	c := check(t, m, "commit_correlation")
	wantStatus(t, c, analysis.CheckSkipped)
	contains(t, c, "reason", baseline)
	if obj(t, m["cause"])["commit"] != nil {
		t.Errorf("cause = %v, want no commit when the correlation was skipped", m["cause"])
	}
}

func TestCommitCorrelationCheck_SkippedWhenGitFailsAndTheAnalysisStillFinishes(t *testing.T) {
	m := run(t, allSpans(4, 4, badTrace), fakeGit{broken: errors.New("not a git repository")}, symptom)
	c := check(t, m, "commit_correlation")
	wantStatus(t, c, analysis.CheckSkipped)
	contains(t, c, "reason", "not a git repository")
	if m["status"] != analysis.StatusCauseFound {
		t.Errorf("status = %v, want the cause found without the commit", m["status"])
	}
}

func TestCommitCorrelationCheck_RanAndNamesTheTouchingCommit(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, badTrace), touchingService, symptom), "commit_correlation")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "c1")
}

// timedTraces gives n server-only traces of the symptom's operation at version, each ms long, the
// first failed of them in error.
func timedTraces(prefix, version string, n int, ms float64, failed int) source {
	var out source
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s%03d", prefix, i)
		srv := server(id, version)
		srv.End = srv.Start.Add(time.Duration(ms * float64(time.Millisecond)))
		if i < failed {
			srv.StatusCode = trace.StatusError
		}
		out = append(out, srv)
	}
	return out
}

func joined(sources ...source) source {
	var out source
	for _, s := range sources {
		out = append(out, s...)
	}
	return out
}

func TestSymptomRegressionCheck_ReportsTheStatsAndTheLatencyRegression(t *testing.T) {
	m := run(t, joined(timedTraces("g", baseline, 40, 10, 0), timedTraces("b", bad, 36, 30, 0)), fakeGit{}, symptom)
	c := check(t, m, "symptom_regression")
	wantStatus(t, c, analysis.CheckRan)
	for _, sub := range []string{"n=40", "n=36", "p50 10.0 ms", "p50 30.0 ms", "p95 30.0 ms", "n ratio 0.90", "latency regression"} {
		contains(t, c, "reason", sub)
	}
	if m["status"] != analysis.StatusInconclusive {
		t.Errorf("status = %v, want inconclusive: the regression check does not change the status", m["status"])
	}
}

func TestSymptomRegressionCheck_WithoutLatencyRegressionTheDetectorsStillRun(t *testing.T) {
	var src source
	for i := 0; i < 30; i++ {
		// Spread durations: 30 traces that all end together would be a ceiling.
		src = append(src, lasting(1+float64(i)/10, badTrace)(fmt.Sprintf("bad%02d", i))...)
		src = append(src, lasting(1+float64(i)/10, healthyTrace)(fmt.Sprintf("good%02d", i))...)
	}
	m := run(t, src, fakeGit{}, symptom)
	c := check(t, m, "symptom_regression")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "no latency regression")
	contains(t, c, "hint", "rootline symptoms")
	if m["status"] != analysis.StatusCauseFound {
		t.Errorf("status = %v, want cause_found as before", m["status"])
	}
}

func TestSymptomRegressionCheck_SkippedBelowTheMinimumSample(t *testing.T) {
	c := check(t, run(t, joined(timedTraces("g", baseline, 40, 10, 0), timedTraces("b", bad, 29, 30, 0)), fakeGit{}, symptom), "symptom_regression")
	wantStatus(t, c, analysis.CheckSkipped)
	contains(t, c, "reason", "n=29")
}

func TestSymptomRegressionAndErrorRateChecks_NotApplicableWithoutBaseline(t *testing.T) {
	s := symptom
	s.BaselineVersion = ""
	m := run(t, timedTraces("b", bad, 40, 30, 4), fakeGit{}, s)
	wantStatus(t, check(t, m, "symptom_regression"), analysis.CheckNotApplicable)
	c := check(t, m, "error_rate")
	wantStatus(t, c, analysis.CheckNotApplicable)
	contains(t, c, "reason", "10.0%")
}

func TestErrorRateCheck_ReportsBothRatesAndSaysV1DoesNotAnalyzeErrors(t *testing.T) {
	m := run(t, joined(timedTraces("g", baseline, 40, 10, 0), timedTraces("b", bad, 40, 10, 3)), fakeGit{}, symptom)
	c := check(t, m, "error_rate")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "0.0% (0 of 40)")
	contains(t, c, "reason", "7.5% (3 of 40)")
	contains(t, c, "reason", "error regression")
	contains(t, c, "hint", "latency only")
}

func TestErrorRateCheck_WithoutErrorRegressionHasNoHint(t *testing.T) {
	c := check(t, run(t, joined(timedTraces("g", baseline, 40, 10, 0), timedTraces("b", bad, 40, 10, 0)), fakeGit{}, symptom), "error_rate")
	wantStatus(t, c, analysis.CheckRan)
	if c["hint"] != "" {
		t.Errorf("hint = %q, want none", c["hint"])
	}
}

func TestChecks_ComeInAFixedOrder(t *testing.T) {
	m := run(t, allSpans(4, 4, badTrace), fakeGit{}, symptom)
	var ids []string
	for _, c := range list(m, "checks") {
		ids = append(ids, obj(t, c)["id"].(string))
	}
	want := "[symptom_regression error_rate truncated_traces caller_hook db_spans n_plus_one missing_index commit_correlation]"
	if fmt.Sprint(ids) != want {
		t.Errorf("checks = %v, want %s", ids, want)
	}
}
