// The detector exports the span phase classification.
package detector_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func TestPhaseOf_ClassifiesDatabaseSpans(t *testing.T) {
	notDB := dbSpan("t", "s", "p", 0, itemsQuery, 1)
	delete(notDB.Attrs, "db.query.text")

	cases := []struct {
		name string
		span trace.Span
		want detector.Phase
	}{
		{"not a database span", notDB, detector.PhaseNone},
		{"phase attribute query", dbSpan("t", "s", "p", 0, itemsQuery, 1, phase("query")), detector.PhaseQuery},
		{"phase attribute prepare", dbSpan("t", "s", "p", 0, itemsQuery, 1, phase("prepare")), detector.PhasePrepare},
		{"phase attribute wins over the heuristic", dbSpan("t", "s", "p", 0, itemsQuery, 1, phase("query"), prepName(), noRows()), detector.PhaseQuery},
		{"no phase, plain query", dbSpan("t", "s", "p", 0, itemsQuery, 1, noPhase()), detector.PhaseQuery},
		{"no phase, name without rows is prepare", dbSpan("t", "s", "p", 0, itemsQuery, 1, noPhase(), prepName(), noRows()), detector.PhasePrepare},
		{"no phase, name with rows is query", dbSpan("t", "s", "p", 0, itemsQuery, 1, noPhase(), prepName()), detector.PhaseQuery},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detector.PhaseOf(c.span); got != c.want {
				t.Fatalf("PhaseOf = %v, want %v", got, c.want)
			}
		})
	}
}
