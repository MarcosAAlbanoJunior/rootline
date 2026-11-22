// Expected API (package analysis; imports only stdlib and internal domain packages):
//
//	type TraceSource interface { Spans() ([]trace.Span, error) }  // all spans of the file (any service/version/trace)
//	type CodeIndex interface {
//	    InLoop(sha, file string, line int) (bool, error)
//	    FuncRange(sha, file, function string) (start, end int, found bool, err error)
//	}
//	type Git interface {
//	    ResolvePath(sha, modulePath, relFile string) (string, error)
//	    RevList(base, bad string) ([]string, error)
//	    ChangedLines(commit string) (map[string][][2]int, error)
//	}
//	type Symptom struct {
//	    Service string; Operation trace.Operation
//	    BadVersion, BaselineVersion string // BaselineVersion "" = no reference
//	}
//	// Analyze builds the Result. json.Marshal(Result) must match the "Formato minimo do Resultado"
//	// of the spec (keys in English, snake_case). Result is compared here only through its JSON form.
//	func Analyze(src TraceSource, idx CodeIndex, git Git, s Symptom) (Result, error)
//
// Result JSON (this test relies on): schema_version "0", mode "detectors_only", symptom{service,
// operation{method,route,route_source}, bad_version, baseline_version|null}, status, cause|null
// {candidate_id,type,confidence,location{level,function,file,line,in_loop},commit{sha,touch}|null},
// candidates[]{id,type,score,confidence,location,evidence[]}, evidence[] (repeated_query,
// stack_touch, symptom_stats), observations[], warnings[]. Candidate score = detector score
// (traces_affected/traces_total + 0.5 plus-one) + 0.5 commit touch on the location function or +0.25
// touch on another stack frame.
package analysis_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

const (
	bad      = "bad1234"
	baseline = "ba5e123"
	modPath  = "example.com/demo-app"
	svcFn    = modPath + "/internal/orders.(*Service).List"
	repoFn   = modPath + "/internal/orders.(*Repo).ItemsByOrder"
	svcFile  = "demo-app/internal/orders/service.go"
	repoFile = "demo-app/internal/orders/repository.go"
	items    = "SELECT * FROM items WHERE order_id = $1"
	orders   = "SELECT id FROM orders WHERE customer_id = $1"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func query(traceID, id, parent string, startUs int, q string, rows int64) trace.Span {
	return trace.Span{
		TraceID: traceID, SpanID: id, ParentSpanID: parent, Name: "query", Kind: 3,
		Start: t0.Add(time.Duration(startUs) * time.Microsecond), End: t0.Add(time.Duration(startUs+10) * time.Microsecond),
		Service: "demo-app",
		Attrs: map[string]any{
			"db.query.text": q, "pgx.rows_affected": rows, "rootline.db.phase": "query",
			"rootline.module.path": modPath,
			"rootline.app_stack": []string{
				repoFn + " internal/orders/repository.go:30",
				svcFn + " internal/orders/service.go:42",
			},
		},
	}
}

func server(traceID, version string) trace.Span {
	return trace.Span{
		TraceID: traceID, SpanID: traceID + "-srv", Name: "GET /customers/{id}/orders", Kind: 2,
		Start: t0, End: t0.Add(1300 * time.Microsecond), Service: "demo-app", Version: version,
	}
}

func badTrace(id string) []trace.Span {
	srv := server(id, bad)
	spans := []trace.Span{srv, query(id, id+"-plus", srv.SpanID, 100, orders, 10)}
	for i := 0; i < 10; i++ {
		spans = append(spans, query(id, fmt.Sprintf("%s-q%d", id, i), srv.SpanID, 200+i*20, items, 3))
	}
	return spans
}

func healthyTrace(id string) []trace.Span {
	srv := server(id, baseline)
	return []trace.Span{srv, query(id, id+"-a", srv.SpanID, 100, orders, 10), query(id, id+"-b", srv.SpanID, 200, items, 30)}
}

type source []trace.Span

func (s source) Spans() ([]trace.Span, error) { return s, nil }

func allSpans(nBad, nGood int, badFn func(string) []trace.Span) source {
	var out source
	for i := 0; i < nBad; i++ {
		out = append(out, badFn(fmt.Sprintf("bad%02d", i))...)
	}
	for i := 0; i < nGood; i++ {
		out = append(out, healthyTrace(fmt.Sprintf("good%02d", i))...)
	}
	return out
}

type fakeIndex struct{}

func (fakeIndex) InLoop(_, file string, line int) (bool, error) {
	return file == svcFile && line == 42, nil
}
func (fakeIndex) FuncRange(_, file, fn string) (int, int, bool, error) {
	switch {
	case file == svcFile && fn == svcFn:
		return 35, 50, true, nil
	case file == repoFile && fn == repoFn:
		return 25, 40, true, nil
	}
	return 0, 0, false, nil
}

type fakeGit struct{ changes map[string][][2]int }

func (fakeGit) ResolvePath(_, _, rel string) (string, error) { return "demo-app/" + rel, nil }
func (fakeGit) RevList(base, badSHA string) ([]string, error) {
	return []string{"c1"}, nil
}
func (g fakeGit) ChangedLines(string) (map[string][][2]int, error) { return g.changes, nil }

var symptom = analysis.Symptom{
	Service:         "demo-app",
	Operation:       trace.Operation{Method: "GET", Route: "/customers/{id}/orders", RouteSource: "span_name"},
	BadVersion:      bad,
	BaselineVersion: baseline,
}

func run(t *testing.T, src source, g fakeGit, s analysis.Symptom) map[string]any {
	t.Helper()
	res, err := analysis.Analyze(src, fakeIndex{}, g, s)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	checkEvidenceRefs(t, m)
	return m
}

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON object, got %#v", v)
	}
	return m
}

func list(m map[string]any, key string) []any { l, _ := m[key].([]any); return l }

// checkEvidenceRefs: every evidence ID cited by a candidate exists in evidence[].
func checkEvidenceRefs(t *testing.T, m map[string]any) {
	t.Helper()
	known := map[string]bool{}
	for _, e := range list(m, "evidence") {
		known[obj(t, e)["id"].(string)] = true
	}
	for _, c := range list(m, "candidates") {
		for _, id := range list(obj(t, c), "evidence") {
			if !known[id.(string)] {
				t.Errorf("candidate cites evidence %v which is not in evidence[]", id)
			}
		}
	}
}

func evidenceOfKind(t *testing.T, m map[string]any, kind string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, e := range list(m, "evidence") {
		if em := obj(t, e); em["kind"] == kind {
			out = append(out, em)
		}
	}
	return out
}

var touchingService = fakeGit{changes: map[string][][2]int{svcFile: {{40, 44}}}}

func TestAC34_CauseFoundWithCommit(t *testing.T) {
	m := run(t, allSpans(4, 4, badTrace), touchingService, symptom)

	if m["schema_version"] != "0" || m["mode"] != "detectors_only" || m["status"] != "cause_found" {
		t.Fatalf("header wrong: %v %v %v", m["schema_version"], m["mode"], m["status"])
	}
	sy := obj(t, m["symptom"])
	op := obj(t, sy["operation"])
	if sy["service"] != "demo-app" || sy["bad_version"] != bad || sy["baseline_version"] != baseline ||
		op["method"] != "GET" || op["route"] != "/customers/{id}/orders" || op["route_source"] != "span_name" {
		t.Errorf("symptom wrong: %v", sy)
	}

	cause := obj(t, m["cause"])
	if cause["candidate_id"] != "C1" || cause["type"] != "n_plus_one" || cause["confidence"] != "high" {
		t.Errorf("cause wrong: %v", cause)
	}
	loc := obj(t, cause["location"])
	if loc["level"] != float64(1) || loc["function"] != svcFn || loc["file"] != svcFile || loc["line"] != float64(42) || loc["in_loop"] != true {
		t.Errorf("location wrong: %v", loc)
	}
	commit := obj(t, cause["commit"])
	if commit["sha"] != "c1" || commit["touch"] != "location" {
		t.Errorf("commit wrong: %v", commit)
	}

	cands := list(m, "candidates")
	if len(cands) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(cands))
	}
	c1 := obj(t, cands[0])
	if c1["id"] != "C1" || c1["score"] != 2.0 || c1["type"] != "n_plus_one" || c1["confidence"] != "high" {
		t.Errorf("candidate wrong: %v", c1)
	}

	rq := evidenceOfKind(t, m, "repeated_query")
	if len(rq) != 1 {
		t.Fatalf("want 1 repeated_query evidence, got %d", len(rq))
	}
	e := rq[0]
	if e["query"] != items || e["group_size_median"] != float64(10) || e["rows_median"] != float64(3) ||
		e["plus_one_matched_ratio"] != 1.0 || e["traces_affected"] != float64(4) || e["traces_total"] != float64(4) ||
		e["phase_inferred"] != false || e["sample_trace_id"] == "" || e["sample_parent_span_id"] == "" {
		t.Errorf("repeated_query wrong: %v", e)
	}

	st := evidenceOfKind(t, m, "stack_touch")
	if len(st) != 1 || st[0]["commit"] != "c1" || st[0]["function"] != svcFn || st[0]["strength"] != "location" {
		t.Errorf("stack_touch wrong: %v", st)
	}

	stats := evidenceOfKind(t, m, "symptom_stats")
	if len(stats) != 2 {
		t.Fatalf("want one symptom_stats per analyzed version (2), got %d", len(stats))
	}
	byVersion := map[any]map[string]any{}
	for _, s := range stats {
		byVersion[s["version"]] = s
	}
	b := byVersion[bad]
	if b == nil || byVersion[baseline] == nil {
		t.Fatalf("symptom_stats must cover both versions: %v", stats)
	}
	if b["traces"] != float64(4) || b["duration_ms_median"] != 1.3 || b["db_query_spans_median"] != float64(11) {
		t.Errorf("bad version stats wrong: %v", b)
	}
	if g := byVersion[baseline]; g["traces"] != float64(4) || g["db_query_spans_median"] != float64(2) {
		t.Errorf("baseline stats wrong: %v", g)
	}

	for _, k := range []string{"observations", "warnings"} {
		if _, ok := m[k].([]any); !ok {
			t.Errorf("%s must be a JSON array (possibly empty), got %#v", k, m[k])
		}
	}
}

func TestAC34_CauseWithoutReference(t *testing.T) {
	s := symptom
	s.BaselineVersion = ""
	m := run(t, allSpans(4, 0, badTrace), touchingService, s)

	if m["status"] != "cause_found" {
		t.Fatalf("status = %v", m["status"])
	}
	if v, present := obj(t, m["symptom"])["baseline_version"]; !present || v != nil {
		t.Errorf("baseline_version must be present and null, got %v (present=%v)", v, present)
	}
	cause := obj(t, m["cause"])
	if v, present := cause["commit"]; !present || v != nil {
		t.Errorf("cause.commit must be present and null, got %v (present=%v)", v, present)
	}
	if c1 := obj(t, list(m, "candidates")[0]); c1["score"] != 1.5 {
		t.Errorf("score without commit bonus must be 1.5, got %v", c1["score"])
	}
	if n := len(evidenceOfKind(t, m, "symptom_stats")); n != 1 {
		t.Errorf("want 1 symptom_stats without a reference, got %d", n)
	}
	if n := len(evidenceOfKind(t, m, "stack_touch")); n != 0 {
		t.Errorf("want no stack_touch evidence without a reference, got %d", n)
	}
}

func TestAC34_NoCandidateIsInconclusive(t *testing.T) {
	m := run(t, allSpans(4, 4, healthyOnBadVersion), touchingService, symptom)

	if m["status"] != "inconclusive" {
		t.Fatalf("status = %v", m["status"])
	}
	if v, present := m["cause"]; !present || v != nil {
		t.Errorf("cause must be present and null, got %v (present=%v)", v, present)
	}
	if n := len(list(m, "candidates")); n != 0 {
		t.Errorf("want no candidates, got %d", n)
	}
}

func healthyOnBadVersion(id string) []trace.Span {
	srv := server(id, bad)
	return []trace.Span{srv, query(id, id+"-a", srv.SpanID, 100, orders, 10), query(id, id+"-b", srv.SpanID, 200, items, 30)}
}

func TestAC33_NoTouchKeepsCandidateWithoutCommitBonus(t *testing.T) {
	m := run(t, allSpans(4, 4, badTrace), fakeGit{changes: map[string][][2]int{"demo-app/README.md": {{1, 9}}}}, symptom)

	cause := obj(t, m["cause"])
	if v, present := cause["commit"]; !present || v != nil {
		t.Errorf("cause.commit must be null when no commit touched the stack, got %v", v)
	}
	if c1 := obj(t, list(m, "candidates")[0]); c1["score"] != 1.5 {
		t.Errorf("score = %v, want 1.5 (no commit bonus)", c1["score"])
	}
	if n := len(evidenceOfKind(t, m, "stack_touch")); n != 0 {
		t.Errorf("no stack_touch evidence expected, got %d", n)
	}
}

func TestAC33_StackOnlyTouchAddsQuarterBonus(t *testing.T) {
	m := run(t, allSpans(4, 4, badTrace), fakeGit{changes: map[string][][2]int{repoFile: {{31, 31}}}}, symptom)

	commit := obj(t, obj(t, m["cause"])["commit"])
	if commit["sha"] != "c1" || commit["touch"] != "stack" {
		t.Errorf("commit wrong: %v", commit)
	}
	if c1 := obj(t, list(m, "candidates")[0]); c1["score"] != 1.75 {
		t.Errorf("score = %v, want 1.75", c1["score"])
	}
}
