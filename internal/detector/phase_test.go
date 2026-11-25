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
		{"postgresql query", dbSpan("t", "s", "p", 0, itemsQuery, 1, attr("db.system.name", "postgresql")), detector.PhaseQuery},
		{"legacy postgresql query", dbSpan("t", "s", "p", 0, itemsQuery, 1, attr("db.system", "postgresql")), detector.PhaseQuery},
		{"postgres alias in another case", dbSpan("t", "s", "p", 0, itemsQuery, 1, attr("db.system", "Postgres")), detector.PhaseQuery},
		{"query of another database", dbSpan("t", "s", "p", 0, itemsQuery, 1, attr("db.system.name", "mysql")), detector.PhaseNone},
		{"legacy query of another database", dbSpan("t", "s", "p", 0, itemsQuery, 1, noPhase(), attr("db.system", "mysql")), detector.PhaseNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detector.PhaseOf(c.span); got != c.want {
				t.Fatalf("PhaseOf = %v, want %v", got, c.want)
			}
		})
	}
}

func attr(k, v string) opt { return func(s *trace.Span) { s.Attrs[k] = v } }
