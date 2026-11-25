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

// opTraces gives n server spans of service/version for "METHOD /route", each ms long, starting at
// startS seconds and one second apart, the first failed of them in error.
func opTraces(service, version, op string, n int, ms float64, startS, failed int) []trace.Span {
	var out []trace.Span
	for i := 0; i < n; i++ {
		start := t0.Add(time.Duration(startS+i) * time.Second)
		sp := trace.Span{
			TraceID: fmt.Sprintf("%s-%s-%s-%d", service, version, op, i), SpanID: fmt.Sprintf("s%d", i),
			Name: op, Kind: trace.KindServer, Service: service, Version: version,
			Start: start, End: start.Add(time.Duration(ms * float64(time.Millisecond))),
		}
		if i < failed {
			sp.StatusCode = trace.StatusError
		}
		out = append(out, sp)
	}
	return out
}

func spansOf(groups ...[]trace.Span) []trace.Span {
	var out []trace.Span
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// history says which version is an ancestor of which.
type history map[string]string // descendant -> ancestor

func (h history) IsAncestor(a, d string) (bool, error) { return h[d] == a, nil }

const (
	vOld = "aaaaaaa"
	vNew = "bbbbbbb"
)

// twoVersions: GET /orders regressed 10x, GET /health is unchanged, both with enough traces.
func twoVersions(oldStart, newStart int) []trace.Span {
	return spansOf(
		opTraces("api", vOld, "GET /orders", 40, 10, oldStart, 0),
		opTraces("api", vOld, "GET /health", 40, 1, oldStart, 0),
		opTraces("api", vNew, "GET /orders", 40, 100, newStart, 0),
		opTraces("api", vNew, "GET /health", 40, 1, newStart, 0),
	)
}

func TestSymptoms_InfersTheOnlyServiceAndOrdersVersionsByGit(t *testing.T) {
	rep, err := analysis.FindSymptoms(twoVersions(0, 100), analysis.SymptomsQuery{}, history{vNew: vOld})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Service != "api" || rep.BaselineVersion != vOld || rep.BadVersion != vNew || rep.VersionOrder != analysis.OrderGit {
		t.Errorf("report = %s %s→%s by %s", rep.Service, rep.BaselineVersion, rep.BadVersion, rep.VersionOrder)
	}
	if len(rep.Notices) != 0 {
		t.Errorf("notices = %v, want none", rep.Notices)
	}
}

func TestSymptoms_RollbackTheGitOrderWinsOverTheTimeOrder(t *testing.T) {
	// The bad version ran first; the baseline came back later in time, but it is the ancestor.
	rep, err := analysis.FindSymptoms(twoVersions(100, 0), analysis.SymptomsQuery{}, history{vNew: vOld})
	if err != nil {
		t.Fatal(err)
	}
	if rep.BaselineVersion != vOld || rep.BadVersion != vNew {
		t.Errorf("baseline %s bad %s, want the git ancestor %s as baseline", rep.BaselineVersion, rep.BadVersion, vOld)
	}
}

func TestSymptoms_WithoutRepositoryTheFirstVersionInTimeIsTheBaselineWithANotice(t *testing.T) {
	rep, err := analysis.FindSymptoms(twoVersions(100, 0), analysis.SymptomsQuery{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.BaselineVersion != vNew || rep.BadVersion != vOld || rep.VersionOrder != analysis.OrderTime {
		t.Errorf("baseline %s bad %s by %s, want the first in time as baseline", rep.BaselineVersion, rep.BadVersion, rep.VersionOrder)
	}
	if len(rep.Notices) == 0 {
		t.Error("want a notice that the order came from time")
	}
}

func TestSymptoms_FlagsChooseTheVersions(t *testing.T) {
	spans := spansOf(twoVersions(0, 100), opTraces("api", "ccccccc", "GET /orders", 40, 10, 200, 0))
	rep, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{BaselineVersion: vNew, BadVersion: "ccccccc"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.BaselineVersion != vNew || rep.BadVersion != "ccccccc" || rep.VersionOrder != analysis.OrderFlags {
		t.Errorf("report = %s→%s by %s", rep.BaselineVersion, rep.BadVersion, rep.VersionOrder)
	}
}

func TestSymptoms_FlagsMustNameTwoDifferentVersionsOfTheTraces(t *testing.T) {
	for _, q := range []analysis.SymptomsQuery{
		{BaselineVersion: vOld, BadVersion: "deff00d"},
		{BaselineVersion: vOld, BadVersion: vOld},
	} {
		_, err := analysis.FindSymptoms(twoVersions(0, 100), q, nil)
		var amb *analysis.VersionAmbiguityError
		if !errors.As(err, &amb) || len(amb.Versions) != 2 {
			t.Errorf("%+v: err = %v, want the versions listed", q, err)
		}
	}
}

func TestSymptoms_UnrelatedVersionsInTheRepositoryFallBackToTimeWithoutAskingForRepo(t *testing.T) {
	rep, err := analysis.FindSymptoms(twoVersions(0, 100), analysis.SymptomsQuery{}, history{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.VersionOrder != analysis.OrderTime || len(rep.Notices) != 1 || strings.Contains(rep.Notices[0], "--repo") {
		t.Errorf("order %s, notices %q", rep.VersionOrder, rep.Notices)
	}
}

func TestSymptoms_AZeroBaselineP50RanksAsTheLargestRatio(t *testing.T) {
	spans := spansOf(
		opTraces("api", vOld, "GET /a", 40, 0, 0, 0), opTraces("api", vNew, "GET /a", 40, 300, 100, 0),
		opTraces("api", vOld, "GET /b", 40, 10, 0, 0), opTraces("api", vNew, "GET /b", 40, 16, 100, 0),
	)
	rep, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{}, history{vNew: vOld})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Operations[0].Route != "/a" || rep.Operations[0].P50Ratio != nil {
		t.Errorf("first = %+v, want GET /a with a nil ratio", rep.Operations[0])
	}
}

func TestSymptoms_MoreThanOneServiceFailsListingThem(t *testing.T) {
	spans := spansOf(twoVersions(0, 100), opTraces("worker", vOld, "POST /jobs", 3, 10, 0, 0))
	_, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{}, nil)
	var amb *analysis.ServiceAmbiguityError
	if !errors.As(err, &amb) || fmt.Sprint(amb.Services) != "[api worker]" {
		t.Fatalf("err = %v, want the services listed", err)
	}
	if _, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{Service: "api"}, nil); err != nil {
		t.Errorf("with --service: %v", err)
	}
}

func TestSymptoms_MoreThanTwoVersionsFailsListingThemWithCountsAndTimes(t *testing.T) {
	spans := spansOf(twoVersions(0, 100), opTraces("api", "ccccccc", "GET /orders", 5, 10, 200, 0))
	_, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{}, nil)
	var amb *analysis.VersionAmbiguityError
	if !errors.As(err, &amb) || len(amb.Versions) != 3 {
		t.Fatalf("err = %v, want the three versions listed", err)
	}
	v := amb.Versions[2]
	if v.Version != "ccccccc" || v.Traces != 5 || !v.First.Equal(t0.Add(200*time.Second)) || !v.Last.Equal(t0.Add(204*time.Second).Add(10*time.Millisecond)) {
		t.Errorf("version = %+v", v)
	}
}

func TestSymptoms_WithoutServiceVersionFails(t *testing.T) {
	spans := spansOf(opTraces("api", "", "GET /orders", 40, 10, 0, 0), opTraces("api", vNew, "GET /orders", 40, 10, 100, 0))
	_, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{}, nil)
	var amb *analysis.VersionAmbiguityError
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, want a version ambiguity", err)
	}
}

func TestSymptoms_OneLinePerOperationRegressedFirstAndASuggestion(t *testing.T) {
	spans := spansOf(twoVersions(0, 100),
		opTraces("api", vOld, "POST /pay", 40, 10, 0, 0), opTraces("api", vNew, "POST /pay", 40, 30, 100, 4))
	rep, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{}, history{vNew: vOld})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, op := range rep.Operations {
		order = append(order, op.Method+" "+op.Route+"="+op.Regression)
	}
	want := "[GET /orders=latency POST /pay=latency_and_error GET /health=]"
	if fmt.Sprint(order) != want {
		t.Errorf("operations = %v, want %s", order, want)
	}
	o := rep.Operations[0]
	if o.Baseline.N != 40 || o.Bad.N != 40 || o.Baseline.P50 != 10 || o.Bad.P95 != 100 || *o.P50Ratio != 10 || *o.NRatio != 1 {
		t.Errorf("GET /orders = %+v", o)
	}
	if p := rep.Operations[1]; p.Bad.ErrorRate != 0.1 || p.Baseline.ErrorRate != 0 {
		t.Errorf("POST /pay error rates = %v %v", p.Baseline.ErrorRate, p.Bad.ErrorRate)
	}
	if rep.Suggested == nil || rep.Suggested.Method != "GET" || rep.Suggested.Route != "/orders" {
		t.Errorf("suggested = %+v, want GET /orders", rep.Suggested)
	}
}

func TestSymptoms_NoSuggestionWithoutRegression(t *testing.T) {
	spans := spansOf(opTraces("api", vOld, "GET /x", 40, 10, 0, 0), opTraces("api", vNew, "GET /x", 40, 11, 100, 0))
	rep, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{}, history{vNew: vOld})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Suggested != nil {
		t.Errorf("suggested = %+v, want none", rep.Suggested)
	}
}

// saturated: every operation regresses together, as when a pool saturates.
func saturated(ordersMs, othersMs float64) []trace.Span {
	return spansOf(
		opTraces("api", vOld, "GET /orders", 40, 10, 0, 0), opTraces("api", vNew, "GET /orders", 40, ordersMs, 100, 0),
		opTraces("api", vOld, "GET /users", 40, 10, 0, 0), opTraces("api", vNew, "GET /users", 40, othersMs, 100, 0),
		opTraces("api", vOld, "GET /items", 40, 10, 0, 0), opTraces("api", vNew, "GET /items", 40, othersMs*0.9, 100, 0),
		opTraces("api", vOld, "GET /health", 40, 1, 0, 0), opTraces("api", vNew, "GET /health", 40, 1, 100, 0),
	)
}

func findSymptoms(t *testing.T, spans []trace.Span) analysis.SymptomsReport {
	t.Helper()
	rep, err := analysis.FindSymptoms(spans, analysis.SymptomsQuery{}, history{vNew: vOld})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func operation(t *testing.T, rep analysis.SymptomsReport, method, route string) analysis.OperationSymptom {
	t.Helper()
	for _, o := range rep.Operations {
		if o.Method == method && o.Route == route {
			return o
		}
	}
	t.Fatalf("no %s %s in %+v", method, route, rep.Operations)
	return analysis.OperationSymptom{}
}

func TestSymptoms_GeneralizedRegressionWithoutAnOperationThatStandsOutSuggestsNothing(t *testing.T) {
	rep := findSymptoms(t, saturated(3000, 2000)) // 300x against 200x: no operation stands out
	if !rep.Generalized {
		t.Fatal("3 of 4 operations regressed: want a generalized regression")
	}
	if rep.Suggested != nil {
		t.Errorf("suggested = %+v, want none", rep.Suggested)
	}
}

func TestSymptoms_GeneralizedRegressionSuggestsTheOperationThatStandsOut(t *testing.T) {
	rep := findSymptoms(t, saturated(10000, 50)) // 1000x against 5x
	if !rep.Generalized || rep.Suggested == nil || rep.Suggested.Route != "/orders" {
		t.Errorf("generalized %v, suggested %+v, want GET /orders", rep.Generalized, rep.Suggested)
	}
}

func TestSymptoms_LessThanThreeQuartersRegressedIsNotGeneralized(t *testing.T) {
	if findSymptoms(t, twoVersions(0, 100)).Generalized {
		t.Error("1 of 2 operations regressed: not generalized")
	}
}

func TestSymptoms_AnOperationThatVanishedInTheBadVersionIsListedMarkedAndNotSuggested(t *testing.T) {
	spans := spansOf(twoVersions(0, 100),
		opTraces("api", vOld, "POST /claim", 50, 2, 0, 0), opTraces("api", vNew, "POST /claim", 1, 5000, 100, 0))
	rep := findSymptoms(t, spans)
	c := operation(t, rep, "POST", "/claim")
	if c.EnoughSample || c.Regression != "" || !c.NDrop || *c.NRatio != 0.02 {
		t.Errorf("POST /claim = %+v, want listed with the drop and no regression", c)
	}
	if rep.Suggested == nil || rep.Suggested.Route != "/orders" {
		t.Errorf("suggested = %+v, want GET /orders", rep.Suggested)
	}
	if operation(t, rep, "GET", "/orders").NDrop {
		t.Error("GET /orders kept its traces: no drop")
	}
}

func TestSymptoms_OnsetIsTheThirdSlowTraceSinceTheBadVersionStarted(t *testing.T) {
	// GET /orders is fast for its first 20 traces in the bad version, then slow; the bad version
	// starts at 100 s with GET /health.
	spans := spansOf(
		opTraces("api", vOld, "GET /orders", 40, 10, 0, 0), opTraces("api", vOld, "GET /health", 40, 1, 0, 0),
		opTraces("api", vNew, "GET /health", 40, 1, 100, 0),
		opTraces("api", vNew, "GET /orders", 20, 10, 105, 0), opTraces("api", vNew, "GET /orders", 20, 100, 125, 0),
	)
	// opTraces builds the same trace ids for both halves; rename the second half.
	for i := range spans[len(spans)-20:] {
		spans[len(spans)-20+i].TraceID += "-late"
	}
	o := operation(t, findSymptoms(t, spans), "GET", "/orders")
	if o.OnsetS == nil || *o.OnsetS != 27 {
		t.Errorf("onset = %v, want 27 s (third slow trace at 127 s, version started at 100 s)", o.OnsetS)
	}
	if h := operation(t, findSymptoms(t, spans), "GET", "/health"); h.OnsetS != nil {
		t.Errorf("GET /health onset = %v, want none", *h.OnsetS)
	}
}

func TestSymptoms_MarksTheOperationsWithACeiling(t *testing.T) {
	spans := spansOf(twoVersions(0, 100), opTraces("api", vOld, "GET /slow", 40, 10, 0, 0),
		opTraces("api", vNew, "GET /slow", 30, 50, 100, 0))
	cut := opTraces("api", vNew, "GET /slow", 10, 10000, 200, 0)
	for i := range cut {
		cut[i].TraceID += "-cut"
	}
	spread := opTraces("api", vNew, "GET /spread", 40, 10, 100, 0)
	for i := range spread {
		spread[i].End = spread[i].End.Add(time.Duration(i) * time.Millisecond) // 10 to 49 ms
	}
	rep := findSymptoms(t, spansOf(spans, cut, spread))
	if c := operation(t, rep, "GET", "/slow").CeilingMs; c == nil || *c != 10000 {
		t.Errorf("GET /slow ceiling = %v, want 10000 ms", c)
	}
	if c := operation(t, rep, "GET", "/spread").CeilingMs; c != nil {
		t.Errorf("GET /spread ceiling = %v, want none", *c)
	}
}

func TestSymptoms_GeneralizedRegressionAnInfiniteRatioStandsOut(t *testing.T) {
	spans := spansOf(
		opTraces("api", vOld, "GET /a", 40, 0, 0, 0), opTraces("api", vNew, "GET /a", 40, 300, 100, 0),
		opTraces("api", vOld, "GET /b", 40, 10, 0, 0), opTraces("api", vNew, "GET /b", 40, 30, 100, 0),
	)
	rep := findSymptoms(t, spans)
	if !rep.Generalized || rep.Suggested == nil || rep.Suggested.Route != "/a" {
		t.Errorf("generalized %v, suggested %+v, want GET /a", rep.Generalized, rep.Suggested)
	}
}
