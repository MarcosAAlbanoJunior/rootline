package detector

import (
	"regexp"
	"slices"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// limitWord matches LIMIT as a whole word, in any case; credit_limit does not match.
var limitWord = regexp.MustCompile(`(?i)\bLIMIT\b`)

// paginationShape reports whether an occurrence is a pagination, not an N+1 (ADR-0021): the query
// has LIMIT, the spans ordered by start are full pages of the same k ≥ 2 rows followed by a smaller
// last page (0 included), every span has its row count, and there is no "+1".
func paginationShape(occ Occurrence) bool {
	if occ.PlusOne || len(occ.Spans) < 2 || !limitWord.MatchString(occ.Query) {
		return false
	}
	spans := slices.Clone(occ.Spans)
	slices.SortStableFunc(spans, func(a, b trace.Span) int { return a.Start.Compare(b.Start) })
	rows := make([]int, len(spans))
	for i, s := range spans {
		r, ok := s.Int(attrRowsAffected)
		if !ok {
			return false
		}
		rows[i] = int(r)
	}
	k := rows[0]
	if k < 2 {
		return false
	}
	for _, r := range rows[:len(rows)-1] {
		if r != k {
			return false
		}
	}
	return rows[len(rows)-1] < k
}
