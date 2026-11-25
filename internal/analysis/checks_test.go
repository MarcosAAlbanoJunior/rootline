package analysis_test

import (
	"errors"
	"strings"
	"testing"

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
