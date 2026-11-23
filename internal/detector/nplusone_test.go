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

func itemsOccurrences(t *testing.T, spans []trace.Span) []detector.Occurrence {
	t.Helper()
	var out []detector.Occurrence
	for _, o := range detector.FindOccurrences(spans) {
		if o.Query == itemsQuery {
			out = append(out, o)
		}
	}
	return out
}

func TestFindOccurrences_PlusOneOccurrence(t *testing.T) {
	occ := itemsOccurrences(t, plusOneThenRepeats("t1", 10, 10, 3))
	if len(occ) != 1 {
		t.Fatalf("want 1 occurrence, got %d", len(occ))
	}
	if !occ[0].PlusOne {
		t.Error("PlusOne = false, want true")
	}
	if len(occ[0].Spans) != 10 || occ[0].ParentSpanID != "root" || occ[0].RowsMedian != 3 {
		t.Errorf("unexpected occurrence: %d spans, parent %q, rows median %d", len(occ[0].Spans), occ[0].ParentSpanID, occ[0].RowsMedian)
	}
}

func TestFindOccurrences_NoOccurrence(t *testing.T) {
	split := append(repeated("t", "p1", 0, 4, 3), repeated("t", "p2", 10, 3, 3)...)
	split = append(split, repeated("t", "p3", 20, 3, 3)...)
	tests := []struct {
		name  string
		spans []trace.Span
	}{
		{"4 repetitions is below MinRepetitions", repeated("t", "root", 0, 4, 3)},
		{"median rows 11 is above MaxMedianRows", repeated("t", "root", 0, 10, 11)},
		{"repetitions under different parents", split},
		{"pagination: 10 equal queries with 50 rows", repeated("t", "root", 0, 10, 50)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if occ := itemsOccurrences(t, tc.spans); len(occ) != 0 {
				t.Fatalf("want no occurrence, got %d", len(occ))
			}
		})
	}
}

func TestFindOccurrences_BoundariesAreInclusive(t *testing.T) {
	if occ := itemsOccurrences(t, repeated("t", "root", 0, 5, 3)); len(occ) != 1 {
		t.Errorf("5 repetitions must be reported, got %d occurrences", len(occ))
	}
	if occ := itemsOccurrences(t, repeated("t", "root", 0, 6, 10)); len(occ) != 1 {
		t.Errorf("median rows 10 must be reported, got %d occurrences", len(occ))
	}
}

func TestFindOccurrences_PlusOneFalse(t *testing.T) {
	tests := []struct {
		name  string
		spans []trace.Span
		want  bool
	}{
		{"no +1 query before the group", repeated("t", "root", 0, 10, 3), false},
		{"+1 with rows 20 for 10 repetitions", plusOneThenRepeats("t", 20, 10, 3), false},
		{"+1 query started after the group", append(repeated("t", "root", 0, 10, 3), dbSpan("t", "late", "root", 50, ordersQuery, 10)), false},
		{"+1 with rows 11 (tolerance 1)", plusOneThenRepeats("t", 11, 10, 3), true},
		{"+1 with rows 9 (tolerance 1)", plusOneThenRepeats("t", 9, 10, 3), true},
		{"+1 under a different parent", append([]trace.Span{dbSpan("t", "plus", "other", 0, ordersQuery, 10)}, repeated("t", "root", 10, 10, 3)...), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			occ := itemsOccurrences(t, tc.spans)
			if len(occ) != 1 {
				t.Fatalf("want 1 occurrence, got %d", len(occ))
			}
			if occ[0].PlusOne != tc.want {
				t.Fatalf("PlusOne = %v, want %v", occ[0].PlusOne, tc.want)
			}
		})
	}
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
	cands, err := detector.Detect(loopAtService, "abc1234", trs)
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

func TestFindOccurrences_PrepareSpansAreNotCounted(t *testing.T) {
	t.Run("explicit prepare phase", func(t *testing.T) {
		spans := append(repeated("t", "root", 0, 10, 3, phase("prepare")), repeated("t2", "root", 20, 3, 3)...)
		if occ := itemsOccurrences(t, spans); len(occ) != 0 {
			t.Fatalf("10 prepare + 3 query spans must not reach MinRepetitions, got %d occurrences", len(occ))
		}
	})
	t.Run("heuristic: prepare_stmt.name and no rows_affected", func(t *testing.T) {
		spans := append(repeated("t", "root", 0, 10, 0, noPhase(), noRows(), prepName()), repeated("t2", "root", 20, 3, 3)...)
		if occ := itemsOccurrences(t, spans); len(occ) != 0 {
			t.Fatalf("heuristic prepare spans must not count, got %d occurrences", len(occ))
		}
	})
	t.Run("missing phase attribute marks phase_inferred", func(t *testing.T) {
		occ := itemsOccurrences(t, repeated("t", "root", 0, 6, 3, noPhase()))
		if len(occ) != 1 || !occ[0].PhaseInferred {
			t.Fatalf("want 1 occurrence with PhaseInferred, got %+v", occ)
		}
	})
	t.Run("explicit phase is not inferred", func(t *testing.T) {
		occ := itemsOccurrences(t, repeated("t", "root", 0, 6, 3))
		if len(occ) != 1 || occ[0].PhaseInferred {
			t.Fatalf("want 1 occurrence without PhaseInferred, got %+v", occ)
		}
	})
	t.Run("inferred phase gives phase_inferred and medium confidence", func(t *testing.T) {
		c := mustOne(t, traces(4, 4, func(id string) []trace.Span { return plusOneThenRepeats(id, 10, 10, 3, noPhase()) }))
		if !c.PhaseInferred || c.Confidence != "medium" {
			t.Fatalf("got PhaseInferred=%v confidence=%q", c.PhaseInferred, c.Confidence)
		}
	})
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
			cands, err := detector.Detect(loopAtService, "abc1234", traces(tc.total, tc.with, healthy))
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

func spansWithStack(entries ...string) []trace.Span {
	return repeated("t", "root", 0, 5, 3, stack(entries...))
}

func TestLocate_AppliesTheLevelOneRule(t *testing.T) {
	outer := modPath + "/internal/orders.(*Handler).Get"
	tests := []struct {
		name       string
		env        fakeEnv
		spans      []trace.Span
		wantFunc   string
		wantFile   string
		wantLine   int
		wantInLoop bool
	}{
		{
			name:       "loop in the innermost frame",
			env:        fakeEnv{loops: map[string]bool{"demo-app/internal/orders/repository.go:30": true}},
			spans:      spansWithStack(defaultStack...),
			wantFunc:   repoFunc,
			wantFile:   "demo-app/internal/orders/repository.go",
			wantLine:   30,
			wantInLoop: true,
		},
		{
			name: "loop in an outer frame (helper in another layer)",
			env:  loopAtService,
			spans: spansWithStack(append(append([]string{}, defaultStack...),
				outer+" internal/orders/handler.go:7")...),
			wantFunc:   svcFunc,
			wantFile:   "demo-app/internal/orders/service.go",
			wantLine:   42,
			wantInLoop: true,
		},
		{
			name:       "no frame in a loop falls back to the innermost frame",
			env:        fakeEnv{},
			spans:      spansWithStack(defaultStack...),
			wantFunc:   repoFunc,
			wantFile:   "demo-app/internal/orders/repository.go",
			wantLine:   30,
			wantInLoop: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loc, reason := detector.Locate(tc.env, "abc1234", tc.spans)
			if loc == nil {
				t.Fatalf("location is nil (reason %q)", reason)
			}
			if loc.Level != 1 || loc.Function != tc.wantFunc || loc.File != tc.wantFile || loc.Line != tc.wantLine || loc.InLoop != tc.wantInLoop {
				t.Fatalf("got %+v", *loc)
			}
		})
	}

	t.Run("no app stack gives nil and no_app_stack", func(t *testing.T) {
		loc, reason := detector.Locate(loopAtService, "abc1234", spansWithStack())
		if loc != nil || reason != "no_app_stack" {
			t.Fatalf("got loc=%v reason=%q", loc, reason)
		}
	})

	t.Run("most frequent location among spans wins", func(t *testing.T) {
		spans := append(spansWithStack(defaultStack...),
			dbSpan("t", "odd", "root", 90, itemsQuery, 3, stack(repoFunc+" internal/orders/repository.go:99")))
		loc, _ := detector.Locate(fakeEnv{}, "abc1234", spans)
		if loc == nil || loc.Line != 30 {
			t.Fatalf("got %+v, want the line-30 location (5 spans vs 1)", loc)
		}
	})
}
