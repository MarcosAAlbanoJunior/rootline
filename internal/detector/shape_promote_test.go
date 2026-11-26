package detector_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

var postgres = detector.Dependency{Kind: detector.KindDatabase, Name: "postgresql"}

// grew is a breakdown whose root mean went from 10 to 10+deltaMs with one dependency row.
func grew(deltaMs float64, r detector.DependencyShare) detector.Breakdown {
	return detector.Breakdown{
		Baseline: detector.RootShare{MeanMs: 10, Traces: 100, MeanTraces: 95},
		Bad:      detector.RootShare{MeanMs: 10 + deltaMs, Traces: 100, MeanTraces: 95},
		Rows:     []detector.DependencyShare{r},
	}
}

func addedWork(presenceFrom, presenceTo, explained float64) detector.DependencyShare {
	return detector.DependencyShare{
		Dependency: postgres, Explained: explained,
		Baseline: detector.Share{MeanMs: 5, Presence: presenceFrom, CallsPerTrace: 3, MsPerCall: 3},
		Bad:      detector.Share{MeanMs: 50, Presence: presenceTo, CallsPerTrace: 5, MsPerCall: 10},
	}
}

func TestShapeMechanism_AddedWorkAtTheThresholds(t *testing.T) {
	cases := []struct {
		name                string
		from, to, explained float64
		want                bool
	}{
		{"presence up 25 points, explains 80%", 0.50, 0.75, 0.80, true},
		{"presence up 24 points", 0.50, 0.74, 0.95, false},
		{"explains 79%", 0.50, 0.90, 0.79, false},
		{"presence up 44 points, explains all", 0.56, 1, 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			typ, row, ok := detector.ShapeMechanism(grew(100, addedWork(c.from, c.to, c.explained)))
			if ok != c.want || (ok && (typ != detector.TypeAddedWork || row.Dependency != postgres)) {
				t.Errorf("ShapeMechanism = %q, %v, %v; want added_work on postgresql: %v", typ, row.Dependency, ok, c.want)
			}
		})
	}
}

func TestShapeMechanism_TheGuardHoldsWhenTheMeanGrewLessThanFiveMs(t *testing.T) {
	for _, delta := range []float64{4.9, 0, -20} {
		if typ, _, ok := detector.ShapeMechanism(grew(delta, addedWork(0.5, 1, 1))); ok {
			t.Errorf("delta %v ms: promoted %s, want the guard to hold", delta, typ)
		}
	}
}

func TestShapeMechanism_MoreCallsPerTraceWithTheSamePresenceIsNotAddedWork(t *testing.T) {
	nPlusOne := addedWork(1, 1, 1)
	nPlusOne.Baseline.CallsPerTrace, nPlusOne.Bad.CallsPerTrace = 4, 104

	if typ, _, ok := detector.ShapeMechanism(grew(100, nPlusOne)); ok {
		t.Errorf("promoted %s, want nothing: more calls in the same traces is the N+1's", typ)
	}
}

// aggregation is a query span of the given aggregation with an app stack whose service frame calls
// it at serviceLine, running from fromMs to toMs.
func aggregation(query, repoFn string, serviceLine int, fromMs, toMs float64) trace.Span {
	s := call("sql.conn.query", map[string]any{
		"db.system.name": "postgresql", "db.query.text": query, "rootline.module.path": modPath,
		"rootline.app_stack": []string{
			modPath + "/internal/stats." + repoFn + " internal/stats/repository.go:" + strconv.Itoa(serviceLine-30),
			modPath + "/internal/stats.(*Service).Stats internal/stats/service.go:" + strconv.Itoa(serviceLine),
			modPath + "/internal/stats.(*Handler).stats internal/stats/handler.go:20",
		},
	}, fromMs, toMs)
	return s
}

// cacheMissTraces: the baseline answers half of the requests from the cache (only the cache lookup),
// the bad version runs the two aggregations in every request.
func cacheMissTraces() (good, bad []detector.VersionTrace) {
	lookup := call("get", map[string]any{"db.system": "redis", "db.statement": "get k"}, 0, 1)
	full := func(id string, ms float64) detector.VersionTrace {
		return shapeTrace(id, ms, lookup,
			aggregation("SELECT count(*) FROM clicks", "(*Repo).Count", 40, 1, ms/2),
			aggregation("SELECT country, count(*) FROM clicks GROUP BY 1", "(*Repo).ByCountry", 42, ms/2, ms-1))
	}
	good = many(20, func(i int) detector.VersionTrace {
		if i%2 == 0 {
			return shapeTrace("g", 2, lookup)
		}
		return full("g", 10)
	})
	bad = many(20, func(int) detector.VersionTrace { return full("b", 60) })
	return good, bad
}

func TestPromoteShape_AddedWorkIsLocatedAtTheInnermostFrameCommonToTheAddedOperations(t *testing.T) {
	good, bad := cacheMissTraces()
	b := detector.TimeBreakdown(good, bad)

	c, ok := detector.PromoteShape(fakeEnv{}, "abc1234", b, bad)

	if !ok {
		t.Fatalf("nothing promoted from %+v", b.Rows)
	}
	if c.Type != detector.TypeAddedWork || c.Target != "postgresql" || c.Confidence != "medium" {
		t.Errorf("candidate = %s on %q, %s; want added_work on postgresql, medium", c.Type, c.Target, c.Confidence)
	}
	if c.Score != row(t, b, "db:postgresql").Explained {
		t.Errorf("score = %v, want the explained fraction", c.Score)
	}
	want := detector.Location{Level: 1, Function: modPath + "/internal/stats.(*Service).Stats", File: "demo-app/internal/stats/service.go", Line: 40}
	if c.Location == nil || *c.Location != want {
		t.Errorf("location = %+v, want %+v", c.Location, want)
	}
	if len(c.Stack) == 0 || c.Stack[0].Function != want.Function || c.ModulePath != modPath {
		t.Errorf("stack = %+v (module %q), want it to start at the common frame", c.Stack, c.ModulePath)
	}
}

func TestPromoteShape_AddedWorkWithoutAStackHasNoLocation(t *testing.T) {
	good, bad := cacheMissTraces()
	for _, vt := range bad {
		for _, s := range vt.Spans {
			delete(s.Attrs, "rootline.app_stack")
		}
	}

	c, ok := detector.PromoteShape(fakeEnv{}, "abc1234", detector.TimeBreakdown(good, bad), bad)

	if !ok || c.Location != nil || c.LocationReason != detector.ReasonNoAppStack {
		t.Errorf("candidate = %+v, %v; want added_work without location, reason no_app_stack", c, ok)
	}
}

var moderationDep = detector.Dependency{Kind: detector.KindHTTP, Name: "moderation:8090"}

func slowCall(d detector.Dependency, explained, slowdown, presenceTo float64) detector.DependencyShare {
	return detector.DependencyShare{
		Dependency: d, Explained: explained,
		Baseline: detector.Share{MeanMs: 3.5, Presence: 1, CallsPerTrace: 1, MsPerCall: 10},
		Bad:      detector.Share{MeanMs: 100, Presence: presenceTo, CallsPerTrace: 1, MsPerCall: 10 * slowdown},
	}
}

func TestShapeMechanism_SlowCallAtTheThresholds(t *testing.T) {
	rpc := detector.Dependency{Kind: detector.KindRPC, Name: "payments.Billing"}
	cases := []struct {
		name string
		row  detector.DependencyShare
		want bool
	}{
		{"explains 80%, 2x per call", slowCall(moderationDep, 0.80, 2, 1), true},
		{"explains 79%", slowCall(moderationDep, 0.79, 30, 1), false},
		{"1.9x per call", slowCall(moderationDep, 0.99, 1.9, 1), false},
		{"presence down 24 points", slowCall(moderationDep, 1, 30, 0.76), true},
		{"presence down 25 points", slowCall(moderationDep, 1, 30, 0.75), false},
		{"an RPC service", slowCall(rpc, 1, 30, 1), true},
		{"a database that got slower", slowCall(postgres, 1, 30, 1), false},
		{"a client span known only by its name", slowCall(detector.Dependency{Kind: detector.KindSpanName, Name: "publish"}, 1, 30, 1), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			typ, row, ok := detector.ShapeMechanism(grew(100, c.row))
			if ok != c.want || (ok && (typ != detector.TypeSlowCall || row.Dependency != c.row.Dependency)) {
				t.Errorf("ShapeMechanism = %q, %v, %v; want slow_call: %v", typ, row.Dependency, ok, c.want)
			}
		})
	}
}

func TestShapeMechanism_ALargerOwnTimeIsNeverPromoted(t *testing.T) {
	own := slowCall(detector.OwnTime, 1, 30, 1)
	own.Baseline.Presence = 0

	if typ, _, ok := detector.ShapeMechanism(grew(100, own)); ok {
		t.Errorf("promoted %s from the own time", typ)
	}
}

func TestPromoteShape_SlowCallHasTheDependencyAsTargetAndNoStack(t *testing.T) {
	c, ok := detector.PromoteShape(fakeEnv{}, "abc1234", grew(100, slowCall(moderationDep, 0.99, 30, 1)), nil)

	if !ok || c.Type != detector.TypeSlowCall || c.Target != "moderation:8090" || c.Score != 0.99 || c.Confidence != "medium" {
		t.Fatalf("candidate = %+v, %v; want slow_call on moderation:8090, score 0.99, medium", c, ok)
	}
	if c.Location != nil || len(c.Stack) != 0 || c.LocationReason != detector.ReasonNoAppStack {
		t.Errorf("location = %+v, stack %v, reason %q; want none: the HTTP span has no app stack", c.Location, c.Stack, c.LocationReason)
	}
}

func TestTraceShapeCheck_SaysTheOriginIsOutsideTheTraceWhenNothingIsPromoted(t *testing.T) {
	db := slowCall(postgres, 1, 30, 1)
	own := slowCall(detector.OwnTime, 1, 30, 1)
	for name, c := range map[string]struct {
		row  detector.DependencyShare
		want string
	}{
		"slower database": {db, "a slower database is not promoted: its origin is outside the trace (index, lock, pool)"},
		"larger own time": {own, "a larger own time is not promoted: its origin is outside the trace (CPU, lock, uninstrumented wait)"},
	} {
		t.Run(name, func(t *testing.T) {
			got := detector.TraceShapeCheck(grew(100, c.row), nil)
			if !strings.Contains(got.Reason, c.want) {
				t.Errorf("reason = %q, want it to contain %q", got.Reason, c.want)
			}
		})
	}
}

func TestTraceShapeCheck_NamesThePromotedSlowCall(t *testing.T) {
	b := grew(100, slowCall(moderationDep, 0.99, 30, 1))
	c, _ := detector.PromoteShape(fakeEnv{}, "abc1234", b, nil)

	got := detector.TraceShapeCheck(b, &c)

	if want := "promoted slow_call on http:moderation:8090: 10.0 → 300.0 ms per call, 99% of the difference"; !strings.Contains(got.Reason, want) {
		t.Errorf("reason = %q, want it to contain %q", got.Reason, want)
	}
}

func TestTraceShapeCheck_CitesTheGuardWhenTheMeanGrewLessThanIt(t *testing.T) {
	for _, delta := range []float64{0.5, 4.7} {
		got := detector.TraceShapeCheck(grew(delta, addedWork(0.5, 1, 1)), nil)

		if want := "the mean grew less than the 5 ms guard, so no mechanism is promoted"; !strings.Contains(got.Reason, want) {
			t.Errorf("delta %v ms: reason = %q, want it to contain %q", delta, got.Reason, want)
		}
		if strings.Contains(got.Hint, "bimodal") {
			t.Errorf("delta %v ms: hint = %q, want no mention of a bimodal latency", delta, got.Hint)
		}
	}
}

func TestTraceShapeCheck_SaysTheMeanDidNotFollowTheP50WhenItDidNotGrow(t *testing.T) {
	for _, delta := range []float64{0, -20} {
		got := detector.TraceShapeCheck(grew(delta, addedWork(0.5, 1, 1)), nil)

		if want := "the mean did not follow the p50"; !strings.Contains(got.Reason, want) {
			t.Errorf("delta %v ms: reason = %q, want it to contain %q", delta, got.Reason, want)
		}
		if !strings.Contains(got.Hint, "bimodal") {
			t.Errorf("delta %v ms: hint = %q, want it to mention a bimodal latency", delta, got.Hint)
		}
	}
}
