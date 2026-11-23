package detector_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

var base = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

const (
	itemsQuery  = "SELECT * FROM items WHERE order_id = $1"
	ordersQuery = "SELECT id FROM orders WHERE customer_id = $1"
	modPath     = "github.com/x/demo-app"
	svcFunc     = modPath + "/internal/orders.(*Service).List"
	repoFunc    = modPath + "/internal/orders.(*Repo).ItemsByOrder"
)

var defaultStack = []string{
	repoFunc + " internal/orders/repository.go:30",
	svcFunc + " internal/orders/service.go:42",
}

type opt func(*trace.Span)

func noPhase() opt {
	return func(s *trace.Span) { delete(s.Attrs, "rootline.db.phase") }
}
func phase(p string) opt { return func(s *trace.Span) { s.Attrs["rootline.db.phase"] = p } }
func noRows() opt        { return func(s *trace.Span) { delete(s.Attrs, "pgx.rows_affected") } }
func prepName() opt {
	return func(s *trace.Span) { s.Attrs["pgx.prepare_stmt.name"] = "stmt_1" }
}
func stack(entries ...string) opt {
	return func(s *trace.Span) {
		if len(entries) == 0 {
			delete(s.Attrs, "rootline.app_stack")
			return
		}
		s.Attrs["rootline.app_stack"] = entries
	}
}

func dbSpan(traceID, id, parent string, startMs int, query string, rows int64, opts ...opt) trace.Span {
	s := trace.Span{
		TraceID: traceID, SpanID: id, ParentSpanID: parent, Name: "query", Kind: 3,
		Start: base.Add(time.Duration(startMs) * time.Millisecond),
		End:   base.Add(time.Duration(startMs)*time.Millisecond + time.Millisecond),
		Attrs: map[string]any{
			"db.query.text":        query,
			"pgx.rows_affected":    rows,
			"rootline.db.phase":    "query",
			"rootline.module.path": modPath,
			"rootline.app_stack":   defaultStack,
		},
		Service: "demo-app",
	}
	for _, o := range opts {
		o(&s)
	}
	return s
}

// repeated builds n identical query spans under parent, starting at startMs.
func repeated(traceID, parent string, startMs, n int, rows int64, opts ...opt) []trace.Span {
	var out []trace.Span
	for i := 0; i < n; i++ {
		out = append(out, dbSpan(traceID, fmt.Sprintf("%s-%s-r%d", traceID, parent, i), parent, startMs+i, itemsQuery, rows, opts...))
	}
	return out
}

func plusOneThenRepeats(traceID string, plusRows int64, n int, rows int64, opts ...opt) []trace.Span {
	spans := []trace.Span{dbSpan(traceID, traceID+"-plus", "root", 0, ordersQuery, plusRows, opts...)}
	return append(spans, repeated(traceID, "root", 10, n, rows, opts...)...)
}

// fakeEnv: every file resolves to demo-app/<rel>; only the listed "file:line" are in a loop.
type fakeEnv struct{ loops map[string]bool }

func (f fakeEnv) ResolvePath(_, _, rel string) (string, error) { return "demo-app/" + rel, nil }
func (f fakeEnv) InLoop(_, file string, line int) (bool, error) {
	return f.loops[fmt.Sprintf("%s:%d", file, line)], nil
}

var loopAtService = fakeEnv{loops: map[string]bool{"demo-app/internal/orders/service.go:42": true}}

// traces builds n traces of which `with` contain the N+1 (built by mk).
func traces(n, with int, mk func(id string) []trace.Span) [][]trace.Span {
	var out [][]trace.Span
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("tr%d", i)
		if i < with {
			out = append(out, mk(id))
		} else {
			out = append(out, []trace.Span{dbSpan(id, id+"-only", "root", 0, ordersQuery, 10)})
		}
	}
	return out
}

func healthy(id string) []trace.Span { return plusOneThenRepeats(id, 10, 10, 3) }

func TestDetect_CandidateConfidenceAndScore(t *testing.T) {
	t.Run("with +1: high and 1.5", func(t *testing.T) {
		c := mustOne(t, traces(4, 4, healthy))
		if c.Confidence != "high" || c.Score != 1.5 || c.PlusOneMatchedRatio != 1.0 {
			t.Fatalf("got confidence=%q score=%v ratio=%v", c.Confidence, c.Score, c.PlusOneMatchedRatio)
		}
	})
	t.Run("without +1: medium and 1.0", func(t *testing.T) {
		c := mustOne(t, traces(4, 4, func(id string) []trace.Span { return repeated(id, "root", 0, 10, 3) }))
		if c.Confidence != "medium" || c.Score != 1.0 || c.PlusOneMatchedRatio != 0 {
			t.Fatalf("got confidence=%q score=%v ratio=%v", c.Confidence, c.Score, c.PlusOneMatchedRatio)
		}
	})
	t.Run("+1 rows 20: medium, no bonus", func(t *testing.T) {
		c := mustOne(t, traces(4, 4, func(id string) []trace.Span { return plusOneThenRepeats(id, 20, 10, 3) }))
		if c.Confidence != "medium" || c.Score != 1.0 {
			t.Fatalf("got confidence=%q score=%v", c.Confidence, c.Score)
		}
	})
	t.Run("score uses affected/total", func(t *testing.T) {
		c := mustOne(t, traces(4, 3, healthy)) // 3 of 4 -> 0.75 + 0.5
		if c.TracesAffected != 3 || c.TracesTotal != 4 || c.Score != 1.25 {
			t.Fatalf("got affected=%d total=%d score=%v", c.TracesAffected, c.TracesTotal, c.Score)
		}
	})
}

func mustOne(t *testing.T, trs [][]trace.Span) detector.Candidate {
	t.Helper()
	cands, _, err := detector.Detect(loopAtService, "abc1234", trs)
	if err != nil {
		t.Fatal(err)
	}
	var items []detector.Candidate
	for _, c := range cands {
		if c.Query == itemsQuery {
			items = append(items, c)
		}
	}
	if len(items) != 1 {
		t.Fatalf("want 1 candidate for the items query, got %d (all: %d)", len(items), len(cands))
	}
	return items[0]
}

func TestDetect_CandidateThresholds(t *testing.T) {
	tests := []struct {
		name         string
		total, with  int
		wantCandidat int
	}{
		{"2 of 2 traces", 2, 2, 0},
		{"3 of 7 traces", 3 + 4, 3, 0},
		{"3 of 3 traces", 3, 3, 1},
		{"3 of 6 traces (exactly half)", 6, 3, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cands, _, err := detector.Detect(loopAtService, "abc1234", traces(tc.total, tc.with, healthy))
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, c := range cands {
				if c.Query == itemsQuery {
					n++
				}
			}
			if n != tc.wantCandidat {
				t.Fatalf("got %d candidates, want %d", n, tc.wantCandidat)
			}
		})
	}
}

func TestDetect_LoopNotFoundIsReportedOncePerDistinctStack(t *testing.T) {
	// Same innermost frame (same candidate), two distinct stacks: 8 frames and 3 frames.
	shorter := eightFramesWithoutLoop()[:3]
	trs := traces(5, 5, func(id string) []trace.Span {
		if id == "tr3" || id == "tr4" {
			return plusOneThenRepeats(id, 10, 10, 3, stack(shorter...))
		}
		return plusOneThenRepeats(id, 10, 10, 3, stack(eightFramesWithoutLoop()...))
	})
	cands, _, err := detector.Detect(fakeEnv{}, "abc1234", trs)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cands))
	}
	all := cands[0].Warnings
	if n := count(all, loopNotFound8); n != 1 {
		t.Fatalf("warnings = %q, want %q exactly once", all, loopNotFound8)
	}
	if n := count(all, "loop_not_found: no app_stack frame inside a loop (3 frames)"); n != 1 {
		t.Fatalf("warnings = %q, want the 3-frame stack reported once too", all)
	}
}
