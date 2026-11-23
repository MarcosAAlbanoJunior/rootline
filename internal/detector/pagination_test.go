// An occurrence with the pagination shape leaves before the aggregation (ADR-0021).
package detector_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

const pageQuery = "SELECT id FROM items WHERE customer_id = $1 AND id > $2 ORDER BY id LIMIT 4"

// pagination adds, after startMs, one page query per entry of rows under the root.
func pagination(traceID string, startMs int, rows ...int64) []trace.Span {
	var out []trace.Span
	for i, r := range rows {
		out = append(out, dbSpan(traceID, fmt.Sprintf("%s-page%d", traceID, i), "root", startMs+i, pageQuery, r))
	}
	return out
}

func paginated(id string) []trace.Span { return pagination(id, 100, 4, 4, 4, 4, 4, 4, 4, 2) }

func detect(t *testing.T, trs [][]trace.Span) ([]detector.Candidate, []detector.Discarded) {
	t.Helper()
	cands, discarded, err := detector.Detect(loopAtService, "abc1234", trs)
	if err != nil {
		t.Fatal(err)
	}
	return cands, discarded
}

func TestDetect_PaginationLeavesBeforeTheAggregationAndTheN1StaysTheSame(t *testing.T) {
	without, _ := detect(t, traces(4, 4, healthy))
	with, discarded := detect(t, traces(4, 4, func(id string) []trace.Span {
		return append(healthy(id), paginated(id)...)
	}))

	if len(with) != 1 || with[0].Query != itemsQuery {
		t.Fatalf("want only the items candidate, got %+v", with)
	}
	w, wo := with[0], without[0]
	if w.Confidence != wo.Confidence || w.Score != wo.Score || w.TracesAffected != wo.TracesAffected || w.TracesTotal != 4 {
		t.Fatalf("with pagination: %s %v %d/%d; without: %s %v %d/%d",
			w.Confidence, w.Score, w.TracesAffected, w.TracesTotal, wo.Confidence, wo.Score, wo.TracesAffected, wo.TracesTotal)
	}
	if want := []detector.Discarded{{Query: pageQuery, Traces: 4}}; !reflect.DeepEqual(discarded, want) {
		t.Fatalf("discarded = %+v, want %+v", discarded, want)
	}
}

func TestDetect_OnlyPaginationGivesNoCandidate(t *testing.T) {
	cands, discarded := detect(t, traces(4, 4, paginated))
	if len(cands) != 0 {
		t.Fatalf("got %d candidates, want 0: %+v", len(cands), cands)
	}
	if want := []detector.Discarded{{Query: pageQuery, Traces: 4}}; !reflect.DeepEqual(discarded, want) {
		t.Fatalf("discarded = %+v, want %+v", discarded, want)
	}
}

func TestDetect_PaginationIsDecidedPerTrace(t *testing.T) {
	cands, discarded := detect(t, traces(6, 6, func(id string) []trace.Span {
		if id < "tr3" {
			return paginated(id)
		}
		return pagination(id, 100, 4, 4, 4, 4, 4, 4, 4, 4) // constant rows: not the shape
	}))
	if len(cands) != 1 || cands[0].Query != pageQuery || cands[0].TracesAffected != 3 || cands[0].TracesTotal != 6 {
		t.Fatalf("want one candidate with 3 of 6 traces, got %+v", cands)
	}
	if want := []detector.Discarded{{Query: pageQuery, Traces: 3}}; !reflect.DeepEqual(discarded, want) {
		t.Fatalf("discarded = %+v, want %+v", discarded, want)
	}
}

func TestDetect_DiscardedQueriesKeepTheOrderInWhichTheyAppeared(t *testing.T) {
	const other = "SELECT id FROM orders WHERE customer_id = $1 AND id > $2 ORDER BY id LIMIT 3"
	_, discarded := detect(t, traces(4, 4, func(id string) []trace.Span {
		spans := paginated(id)
		for i, r := range []int64{3, 3, 3, 3, 3, 1} {
			spans = append(spans, dbSpan(id, fmt.Sprintf("%s-o%d", id, i), "root", 200+i, other, r))
		}
		return spans
	}))
	want := []detector.Discarded{{Query: pageQuery, Traces: 4}, {Query: other, Traces: 4}}
	if !reflect.DeepEqual(discarded, want) {
		t.Fatalf("discarded = %+v, want %+v", discarded, want)
	}
}

func TestDetect_DiscardedCountsTracesNotOccurrences(t *testing.T) {
	_, discarded := detect(t, traces(4, 4, func(id string) []trace.Span {
		spans := paginated(id)
		for i, r := range []int64{4, 4, 4, 4, 4, 4, 4, 2} {
			spans = append(spans, dbSpan(id, fmt.Sprintf("%s-other%d", id, i), "other-parent", 300+i, pageQuery, r))
		}
		return spans
	}))
	if want := []detector.Discarded{{Query: pageQuery, Traces: 4}}; !reflect.DeepEqual(discarded, want) {
		t.Fatalf("discarded = %+v, want %+v", discarded, want)
	}
}
