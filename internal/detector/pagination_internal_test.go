package detector

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// pages builds one query span per entry of rows, started in order; a negative entry has no rows
// attribute.
func pages(query string, rows ...int) []trace.Span {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]trace.Span, len(rows))
	for i, r := range rows {
		attrs := map[string]any{attrQueryText: query}
		if r >= 0 {
			attrs[attrRowsAffected] = int64(r)
		}
		out[i] = trace.Span{SpanID: fmt.Sprintf("s%d", i), Start: t0.Add(time.Duration(i) * time.Millisecond), Attrs: attrs}
	}
	return out
}

func TestPaginationShape(t *testing.T) {
	const limit4 = "SELECT id FROM items WHERE customer_id = $1 AND id > $2 ORDER BY id LIMIT 4"
	unordered := pages(limit4, 4, 4, 4, 4, 4, 4, 4, 2)
	slices.Reverse(unordered) // each span keeps its start: the page of 2 rows still starts last
	tests := []struct {
		name    string
		spans   []trace.Span
		plusOne bool
		want    bool
	}{
		{"7 full pages of 4 and a last one of 2", pages(limit4, 4, 4, 4, 4, 4, 4, 4, 2), false, true},
		{"last page empty", pages(limit4, 4, 4, 4, 4, 4, 4, 4, 0), false, true},
		{"limit in lower case", pages("select id from items order by id limit 4", 4, 4, 4, 4, 4, 4, 4, 2), false, true},
		{"spans out of start order", unordered, false, true},
		{"constant rows, last equal to the others", pages("SELECT * FROM items WHERE order_id = $1 LIMIT 5", 3, 3, 3, 3, 3, 3, 3, 3, 3, 3), false, false},
		{"no LIMIT", pages("SELECT id FROM items WHERE customer_id = $1 ORDER BY id", 4, 4, 4, 4, 4, 4, 4, 2), false, false},
		{"with +1", pages(limit4, 4, 4, 4, 4, 4, 4, 4, 2), true, false},
		{"7 full pages of 1 and a last one of 0 (k < 2)", pages(limit4, 1, 1, 1, 1, 1, 1, 1, 0), false, false},
		{"a span without rows", pages(limit4, 4, 4, 4, -1, 4, 4, 4, 2), false, false},
		{"LIMIT only inside another word", pages("SELECT credit_limit FROM customers WHERE id = $1", 4, 4, 4, 4, 4, 4, 4, 2), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			occ := Occurrence{Query: Normalize(tc.spans[0].Attrs[attrQueryText].(string)), Spans: tc.spans, PlusOne: tc.plusOne}
			if got := paginationShape(occ); got != tc.want {
				t.Fatalf("paginationShape = %v, want %v", got, tc.want)
			}
		})
	}
}
