package detector_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

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
