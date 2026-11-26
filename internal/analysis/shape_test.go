package analysis_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// callTrace is a trace of the symptom's operation lasting rootMs, with one HTTP call to moderation
// lasting callMs when callMs > 0.
func callTrace(id, version string, rootMs, callMs float64) []trace.Span {
	srv := server(id, version)
	srv.End = t0.Add(time.Duration(rootMs * float64(time.Millisecond)))
	spans := []trace.Span{srv}
	if callMs > 0 {
		spans = append(spans, trace.Span{
			TraceID: id, SpanID: id + "-http", ParentSpanID: srv.SpanID, Name: "HTTP POST", Kind: trace.KindClient,
			Start: t0, End: t0.Add(time.Duration(callMs * float64(time.Millisecond))), Service: "demo-app",
			Attrs: map[string]any{"http.request.method": "POST", "server.address": "moderation", "server.port": int64(8090)},
		})
	}
	return spans
}

// traces builds n traces of a version; f gives the root and call durations of the i-th.
func traces(prefix, version string, n int, f func(i int) (rootMs, callMs float64)) source {
	var out source
	for i := 0; i < n; i++ {
		r, c := f(i)
		out = append(out, callTrace(fmt.Sprintf("%s%02d", prefix, i), version, r, c)...)
	}
	return out
}

func slowCall(t *testing.T) map[string]any {
	src := append(traces("good", baseline, 40, func(int) (float64, float64) { return 5, 3 }),
		traces("bad", bad, 40, func(int) (float64, float64) { return 150, 148 })...)
	return run(t, src, fakeGit{}, symptom)
}

func breakdownOf(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	evs := evidenceOfKind(t, m, "time_breakdown")
	if len(evs) != 1 {
		t.Fatalf("%d time_breakdown evidences in %v, want 1", len(evs), m["evidence"])
	}
	return evs[0]
}

func TestTimeBreakdown_IsEvidenceOfALatencyRegression(t *testing.T) {
	ev := breakdownOf(t, slowCall(t))

	if ev["baseline_mean_ms"] != 5.0 || ev["bad_mean_ms"] != 150.0 {
		t.Errorf("means = %v → %v, want 5 → 150", ev["baseline_mean_ms"], ev["bad_mean_ms"])
	}
	deps := list(ev, "dependencies")
	first := obj(t, deps[0])
	if first["dependency"] != "http:moderation:8090" || first["kind"] != "http" || first["explained"] != 1.0 {
		t.Errorf("first row = %v, want http:moderation:8090 explaining all of it", first)
	}
	b := obj(t, first["bad"])
	for k, want := range map[string]float64{"mean_ms": 148, "presence": 1, "calls_per_trace": 1, "ms_per_call": 148} {
		if b[k] != want {
			t.Errorf("bad %s = %v, want %v", k, b[k], want)
		}
	}
	if ops := list(first, "operations"); len(ops) != 1 || obj(t, ops[0])["operation"] != "POST" {
		t.Errorf("operations = %v, want POST", ops)
	}
	var own bool
	for _, d := range deps {
		own = own || obj(t, d)["dependency"] == "own_time"
	}
	if !own {
		t.Errorf("no own_time row in %v", deps)
	}
}

func TestTraceShapeCheck_RanNamesTheDominantDependencyAndPromotesNothing(t *testing.T) {
	m := slowCall(t)
	c := check(t, m, "trace_shape")

	wantStatus(t, c, "ran")
	contains(t, c, "reason", "http:moderation:8090")
	contains(t, c, "reason", "nothing promoted")
	if len(list(m, "candidates")) != 0 {
		t.Errorf("candidates = %v, want none", m["candidates"])
	}
}

func TestTraceShapeCheck_DoesNotApplyWithoutALatencyRegression(t *testing.T) {
	same := func(int) (float64, float64) { return 5, 3 }
	m := run(t, append(traces("good", baseline, 40, same), traces("bad", bad, 40, same)...), fakeGit{}, symptom)

	wantStatus(t, check(t, m, "trace_shape"), "not_applicable")
	for _, e := range list(m, "evidence") {
		if obj(t, e)["kind"] == "time_breakdown" {
			t.Errorf("time_breakdown without a latency regression: %v", e)
		}
	}
}

func TestTraceShapeCheck_DoesNotApplyWithoutABaseline(t *testing.T) {
	s := symptom
	s.BaselineVersion = ""
	m := run(t, traces("bad", bad, 40, func(int) (float64, float64) { return 150, 147 }), fakeGit{}, s)

	wantStatus(t, check(t, m, "trace_shape"), "not_applicable")
}

func TestTraceShapeCheck_AppearsWithNoTraceOfTheBadVersion(t *testing.T) {
	m := run(t, traces("good", baseline, 40, func(int) (float64, float64) { return 5, 3 }), fakeGit{}, symptom)

	wantStatus(t, check(t, m, "trace_shape"), "not_applicable")
}

func TestTraceShapeCheck_SaysTheMeanDidNotFollowTheP50(t *testing.T) {
	// Bimodal baseline: the p50 is the fast half, the mean is pulled up by the slow half.
	good := traces("good", baseline, 40, func(i int) (float64, float64) {
		if i%2 == 0 {
			return 1, 0
		}
		return 100, 0
	})
	m := run(t, append(good, traces("bad", bad, 40, func(int) (float64, float64) { return 10, 0 })...), fakeGit{}, symptom)
	c := check(t, m, "trace_shape")

	wantStatus(t, c, "ran")
	contains(t, c, "reason", "the mean did not follow the p50")
	breakdownOf(t, m)
}

// statsQuery is an aggregation span of the stats service, called from service.go:line.
func statsQuery(id, sql string, line int, fromMs, toMs float64) trace.Span {
	at := func(ms float64) time.Time { return t0.Add(time.Duration(ms * float64(time.Millisecond))) }
	return trace.Span{
		TraceID: id, SpanID: fmt.Sprintf("%s-%d", id, line), ParentSpanID: id + "-srv", Name: "sql.conn.query", Kind: trace.KindClient,
		Start: at(fromMs), End: at(toMs), Service: "demo-app",
		Attrs: map[string]any{
			"db.system.name": "postgresql", "db.query.text": sql, "rootline.module.path": modPath,
			"rootline.app_stack": []string{fmt.Sprintf("%s/internal/orders.(*Repo).Aggregate%d internal/orders/repository.go:%d", modPath, line, line-10), fmt.Sprintf("%s internal/orders/service.go:%d", svcFn, line)},
		},
	}
}

// cacheMiss: the baseline answers half of the requests without the database; the bad version runs
// both aggregations in every request.
func cacheMiss() source {
	trace := func(id, version string, ms float64, aggregations bool) []trace.Span {
		srv := server(id, version)
		srv.End = t0.Add(time.Duration(ms * float64(time.Millisecond)))
		spans := []trace.Span{srv}
		if aggregations {
			spans = append(spans, statsQuery(id, "SELECT count(*) FROM clicks", 42, 1, ms/2), statsQuery(id, "SELECT day, count(*) FROM clicks GROUP BY 1", 44, ms/2, ms-1))
		}
		return spans
	}
	var out source
	for i := 0; i < 40; i++ {
		out = append(out, trace(fmt.Sprintf("good%02d", i), baseline, 2+8*float64(i%2), i%2 == 1)...)
		out = append(out, trace(fmt.Sprintf("bad%02d", i), bad, 60+float64(i%3), true)...)
	}
	return out
}

func TestAddedWork_IsTheCauseWhenNoSpecificCandidateExists(t *testing.T) {
	m := run(t, cacheMiss(), touchingService, symptom)

	cause := obj(t, m["cause"])
	if cause["type"] != "added_work" || cause["target"] != "postgresql" || cause["confidence"] != "medium" {
		t.Fatalf("cause = %v, want added_work on postgresql, medium", cause)
	}
	loc := obj(t, cause["location"])
	if loc["level"] != 1.0 || loc["file"] != svcFile || loc["line"] != 42.0 {
		t.Errorf("location = %v, want level 1 at %s:42", loc, svcFile)
	}
	if obj(t, cause["commit"])["sha"] == nil {
		t.Errorf("commit = %v, want the commit that touched the common frame", cause["commit"])
	}
	cand := obj(t, list(m, "candidates")[0])
	ev := breakdownOf(t, m)
	if refs := list(cand, "evidence"); len(refs) == 0 || refs[0] != ev["id"] {
		t.Errorf("candidate evidence = %v, want the time breakdown %v first", refs, ev["id"])
	}
	c := check(t, m, "trace_shape")
	contains(t, c, "reason", "promoted added_work")
	contains(t, c, "reason", "db:postgresql")
}

func TestShapeCandidates_ComeAfterTheSpecificOnesWhateverTheScore(t *testing.T) {
	src := cacheMiss()
	// An N+1 without the "+1" in 60% of the bad traces: score 0.6, below the added_work's.
	for i := 0; i < 24; i++ {
		id := fmt.Sprintf("bad%02d", i)
		for j := 0; j < 6; j++ {
			src = append(src, query(id, fmt.Sprintf("%s-n%d", id, j), id+"-srv", 100+j*20, items, 3))
		}
	}
	m := run(t, src, touchingService, symptom)

	var types []string
	var scores []float64
	for _, c := range list(m, "candidates") {
		types = append(types, obj(t, c)["type"].(string))
		scores = append(scores, obj(t, c)["score"].(float64))
	}
	if fmt.Sprint(types) != "[n_plus_one added_work]" {
		t.Fatalf("candidates = %v, want the N+1 first", types)
	}
	if scores[0] >= scores[1] {
		t.Fatalf("scores = %v: the test needs the N+1 to score lower", scores)
	}
	if obj(t, m["cause"])["type"] != "n_plus_one" {
		t.Errorf("cause = %v, want the N+1", m["cause"])
	}
}

func TestAddedWork_WithoutAStackFallsBackToTheRouteHandler(t *testing.T) {
	src := cacheMiss()
	for _, sp := range src {
		delete(sp.Attrs, "rootline.app_stack")
	}
	var calls []string
	m := analyze(t, routeIndex{fn: handlerFn, file: handlerFile, found: true, calls: &calls}, fakeGit{}, src)

	loc := obj(t, obj(t, m["cause"])["location"])
	if loc["level"] != 2.0 || loc["function"] != handlerFn || loc["approximate"] != true {
		t.Errorf("location = %v, want the approximate level 2 at the handler", loc)
	}
	var warned bool
	for _, w := range list(m, "warnings") {
		warned = warned || strings.Contains(w.(string), "level 1 unavailable for dependency postgresql (no_app_stack)")
	}
	if !warned {
		t.Errorf("warnings = %v, want the level-2 notice naming the dependency", m["warnings"])
	}
}
