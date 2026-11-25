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
