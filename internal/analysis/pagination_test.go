// A query discarded as pagination is visible in the observations of the Result (ADR-0021).
package analysis_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

const page = "SELECT id FROM items WHERE customer_id = $1 AND id > $2 ORDER BY id LIMIT 4"

func badTraceWithPagination(id string) []trace.Span {
	spans := badTrace(id)
	for i, rows := range []int64{4, 4, 4, 4, 4, 4, 4, 2} {
		spans = append(spans, query(id, fmt.Sprintf("%s-page%d", id, i), id+"-srv", 600+i*20, page, rows))
	}
	return spans
}

func TestAnalyze_DiscardedPaginationIsAnObservationWithItsTraceCount(t *testing.T) {
	m := run(t, allSpans(4, 4, badTraceWithPagination), fakeGit{}, symptom)

	want := []any{"pagination_shape: " + page + " (4 traces)"}
	if got := list(m, "observations"); !reflect.DeepEqual(got, want) {
		t.Fatalf("observations = %v, want %v", got, want)
	}
	if n := len(list(m, "candidates")); n != 1 {
		t.Fatalf("got %d candidates, want only the N+1", n)
	}
}

func TestAnalyze_WithoutPaginationTheObservationsDoNotChange(t *testing.T) {
	m := run(t, allSpans(4, 4, badTrace), fakeGit{}, symptom)
	if got := list(m, "observations"); len(got) != 0 {
		t.Fatalf("observations = %v, want none", got)
	}
}
