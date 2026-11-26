package detector_test

import (
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func TestDependencyOf_NamesTheDestinationOfAClientSpan(t *testing.T) {
	cases := []struct {
		name  string
		span  trace.Span
		want  string
		found bool
	}{
		{"database by db.system.name", clientSpan("SELECT", map[string]any{"db.system.name": "postgresql", "server.address": "pg", "server.port": int64(5432)}), "db:postgresql", true},
		{"database by the legacy db.system", clientSpan("get", map[string]any{"db.system": "redis", "server.address": "cache", "server.port": int64(6379)}), "db:redis", true},
		{"http by server.address and port", clientSpan("HTTP POST", map[string]any{"http.request.method": "POST", "server.address": "moderation", "server.port": int64(8090)}), "http:moderation:8090", true},
		{"http by the legacy net.peer attributes", clientSpan("HTTP GET", map[string]any{"http.method": "GET", "net.peer.name": "billing", "net.peer.port": int64(80)}), "http:billing:80", true},
		{"rpc by rpc.service", clientSpan("Charge", map[string]any{"rpc.service": "payments.Billing", "server.address": "billing", "server.port": int64(443)}), "rpc:payments.Billing", true},
		{"any other client span by its name", clientSpan("publish orders", nil), "span:publish orders", true},
		{"an internal span is not a dependency", trace.Span{Name: "orders.List", Kind: trace.KindInternal}, "", false},
		{"an internal database span is", trace.Span{Name: "pool.acquire", Kind: trace.KindInternal, Attrs: map[string]any{"db.system.name": "postgresql"}}, "db:postgresql", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := detector.DependencyOf(c.span)
			if ok != c.found || (ok && got.String() != c.want) {
				t.Errorf("DependencyOf = %q, %v; want %q, %v", got, ok, c.want, c.found)
			}
		})
	}
}

func clientSpan(name string, attrs map[string]any) trace.Span {
	return trace.Span{Name: name, Kind: trace.KindClient, Attrs: attrs, Start: time.Time{}, End: time.Time{}}
}

var pg = map[string]any{"db.system.name": "postgresql"}

var selectOne = map[string]any{"db.system.name": "postgresql", "db.query.text": "SELECT 1"}

func moderation() map[string]any {
	return map[string]any{"http.request.method": "POST", "server.address": "moderation", "server.port": int64(8090)}
}

func ms(v float64) time.Time { return time.Time{}.Add(time.Duration(v * float64(time.Millisecond))) }

// shapeTrace is a trace whose root lasts durMs, with the given spans under it (their TraceID is set).
func shapeTrace(id string, durMs float64, spans ...trace.Span) detector.VersionTrace {
	root := trace.Span{TraceID: id, SpanID: id + "-root", Kind: trace.KindServer, Name: "POST /messages", Start: ms(0), End: ms(durMs)}
	vt := detector.VersionTrace{Root: root, Spans: []trace.Span{root}}
	for i, s := range spans {
		s.TraceID = id
		if s.SpanID == "" {
			s.SpanID = id + "-" + string(rune('a'+i))
		}
		if s.ParentSpanID == "" {
			s.ParentSpanID = root.SpanID
		}
		vt.Spans = append(vt.Spans, s)
	}
	return vt
}

func call(name string, attrs map[string]any, fromMs, toMs float64) trace.Span {
	return trace.Span{Name: name, Kind: trace.KindClient, Attrs: attrs, Start: ms(fromMs), End: ms(toMs)}
}

func many(n int, f func(i int) detector.VersionTrace) []detector.VersionTrace {
	out := make([]detector.VersionTrace, n)
	for i := range out {
		out[i] = f(i)
	}
	return out
}

func row(t *testing.T, b detector.Breakdown, d string) detector.DependencyShare {
	t.Helper()
	for _, r := range b.Rows {
		if r.Dependency.String() == d {
			return r
		}
	}
	t.Fatalf("no row %q in %+v", d, b.Rows)
	return detector.DependencyShare{}
}

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if d := got - want; d > 1e-6 || d < -1e-6 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestTimeBreakdown_ConcurrentSpansOfADependencyCountOnce(t *testing.T) {
	tr := func(int) detector.VersionTrace {
		return shapeTrace("t", 100, call("SELECT", selectOne, 10, 50), call("SELECT", selectOne, 30, 70))
	}
	b := detector.TimeBreakdown(many(10, tr), many(10, tr))

	r := row(t, b, "db:postgresql")
	near(t, "mean ms", r.Bad.MeanMs, 60)
	near(t, "calls per trace", r.Bad.CallsPerTrace, 2)
	near(t, "ms per call", r.Bad.MsPerCall, 30)
}

func TestTimeBreakdown_AnAbsentDependencyCountsZero(t *testing.T) {
	good := many(10, func(int) detector.VersionTrace { return shapeTrace("g", 10) })
	slow := many(10, func(int) detector.VersionTrace {
		return shapeTrace("b", 150, call("HTTP POST", moderation(), 5, 145))
	})
	b := detector.TimeBreakdown(good, slow)

	r := row(t, b, "http:moderation:8090")
	near(t, "baseline mean", r.Baseline.MeanMs, 0)
	near(t, "baseline presence", r.Baseline.Presence, 0)
	near(t, "bad mean", r.Bad.MeanMs, 140)
	near(t, "bad presence", r.Bad.Presence, 1)
	near(t, "explained", r.Explained, 1)
	if len(r.Operations) != 1 || r.Operations[0].Name != "POST" {
		t.Errorf("operations = %+v, want one POST", r.Operations)
	}
}

func TestTimeBreakdown_OwnTimeIsTheRootLessTheUnionOfItsDirectChildren(t *testing.T) {
	wrapper := trace.Span{SpanID: "w", Name: "orders.List", Kind: trace.KindInternal, Start: ms(10), End: ms(60)}
	inner := call("SELECT", pg, 20, 30)
	inner.ParentSpanID = "w"
	tr := func(int) detector.VersionTrace {
		return shapeTrace("t", 100, wrapper, inner, call("SELECT", pg, 50, 80))
	}
	b := detector.TimeBreakdown(many(10, tr), many(10, tr))

	near(t, "own time", row(t, b, "own_time").Bad.MeanMs, 30) // 100 - [10,80]
	near(t, "own presence", row(t, b, "own_time").Bad.Presence, 1)
	near(t, "db time", row(t, b, "db:postgresql").Bad.MeanMs, 40)
}

func TestTimeBreakdown_TheMeanLeavesOutTheSlowestFivePercentButPresenceCountsThem(t *testing.T) {
	good := many(20, func(int) detector.VersionTrace { return shapeTrace("g", 10) })
	bad := many(20, func(i int) detector.VersionTrace {
		if i == 7 { // the slowest trace, the only one calling the dependency
			return shapeTrace("b", 1000, call("HTTP POST", moderation(), 0, 990))
		}
		return shapeTrace("b", 20)
	})
	b := detector.TimeBreakdown(good, bad)

	near(t, "bad root mean", b.Bad.MeanMs, 20)
	if b.Bad.Traces != 20 || b.Bad.MeanTraces != 19 {
		t.Errorf("traces = %d, in the mean %d; want 20 and 19", b.Bad.Traces, b.Bad.MeanTraces)
	}
	r := row(t, b, "http:moderation:8090")
	near(t, "bad mean", r.Bad.MeanMs, 0)
	near(t, "bad presence", r.Bad.Presence, 0.05)
}

func TestTimeBreakdown_TheExplainedFractionIsCappedAtOne(t *testing.T) {
	good := many(10, func(int) detector.VersionTrace { return shapeTrace("g", 50, call("HTTP POST", moderation(), 0, 5)) })
	bad := many(10, func(int) detector.VersionTrace { return shapeTrace("b", 60, call("HTTP POST", moderation(), 0, 55)) })
	b := detector.TimeBreakdown(good, bad)

	near(t, "delta", b.DeltaMs(), 10)
	near(t, "explained", row(t, b, "http:moderation:8090").Explained, 1)
	if b.Rows[0].Dependency.String() != "http:moderation:8090" {
		t.Errorf("first row = %s, want the dependency that grew most", b.Rows[0].Dependency)
	}
}

func TestTimeBreakdown_DatabaseOperationsAreTheNormalizedQueries(t *testing.T) {
	q := map[string]any{"db.system.name": "postgresql", "db.query.text": "SELECT  count(*)\n FROM clicks;"}
	acquire := trace.Span{Name: "pool.acquire", Kind: trace.KindClient, Attrs: pg, Start: ms(1), End: ms(2)}
	good := many(10, func(i int) detector.VersionTrace {
		if i%2 == 0 {
			return shapeTrace("g", 10)
		}
		return shapeTrace("g", 10, acquire, call("sql.conn.query", q, 2, 8))
	})
	bad := many(10, func(int) detector.VersionTrace { return shapeTrace("b", 10, acquire, call("sql.conn.query", q, 2, 8)) })
	b := detector.TimeBreakdown(good, bad)

	r := row(t, b, "db:postgresql")
	near(t, "calls per trace", r.Bad.CallsPerTrace, 1) // the pool span is time, not a call
	var found bool
	for _, op := range r.Operations {
		if op.Name == "SELECT count(*) FROM clicks" {
			found = true
			near(t, "baseline presence", op.BaselinePresence, 0.5)
			near(t, "bad presence", op.BadPresence, 1)
		}
	}
	if !found {
		t.Errorf("operations = %+v, want the normalized query", r.Operations)
	}
}

func TestTimeBreakdown_AStatementWithLiteralValuesIsNotAnOperation(t *testing.T) {
	get := func(key string) trace.Span {
		return call("get", map[string]any{"db.system": "redis", "db.statement": "get link:" + key}, 1, 2)
	}
	tr := func(i int) detector.VersionTrace { return shapeTrace("t", 10, get(string(rune('a'+i)))) }
	b := detector.TimeBreakdown(many(10, tr), many(10, tr))

	r := row(t, b, "db:redis")
	if len(r.Operations) != 1 || r.Operations[0].Name != "get" {
		t.Errorf("operations = %+v, want only the command get", r.Operations)
	}
	near(t, "calls per trace", r.Bad.CallsPerTrace, 1)
}

func TestTimeBreakdown_LeavesOutAnotherRequestOfTheServiceInTheTrace(t *testing.T) {
	other := call("SELECT", selectOne, 10, 90)
	other.ParentSpanID = "another-root"
	tr := func(int) detector.VersionTrace { return shapeTrace("t", 100, other) }
	b := detector.TimeBreakdown(many(10, tr), many(10, tr))

	for _, r := range b.Rows {
		if r.Dependency.Kind == detector.KindDatabase {
			t.Errorf("row %s counts a span outside the root: %+v", r.Dependency, r)
		}
	}
}
