// ADR-0020: a group that came from collapsing repeated wrapper spans says so in the repeated_query
// evidence, without changing confidence, score or db_query_spans_median.
package analysis_test

import (
	"fmt"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// pooledBadTrace is badTrace with each items query inside its own workpool.job span.
func pooledBadTrace(id string) []trace.Span {
	var out []trace.Span
	for _, s := range badTrace(id) {
		if q, _ := s.String("db.query.text"); q == items {
			job := trace.Span{
				TraceID: id, SpanID: s.SpanID + "-job", ParentSpanID: s.ParentSpanID, Name: "workpool.job",
				Kind: trace.KindInternal, Start: s.Start, End: s.End, Service: "demo-app", Version: bad,
			}
			s.ParentSpanID = job.SpanID
			out = append(out, job)
		}
		out = append(out, s)
	}
	return out
}

func TestAnalyze_CollapsedParentIsEvidence(t *testing.T) {
	m := run(t, allSpans(4, 4, pooledBadTrace), touchingService, symptom)
	rq := evidenceOfKind(t, m, "repeated_query")
	if len(rq) != 1 {
		t.Fatalf("want 1 repeated_query evidence, got %d", len(rq))
	}
	e := rq[0]
	if e["collapsed_parent_name"] != "workpool.job" || e["collapsed_parent_count"] != float64(10) {
		t.Fatalf("collapse evidence wrong: %v", e)
	}
	if e["sample_parent_span_id"] != fmt.Sprintf("%s-srv", e["sample_trace_id"]) {
		t.Fatalf("sample_parent_span_id = %v, want the server span (effective parent)", e["sample_parent_span_id"])
	}
}

func TestAnalyze_NoCollapseLeavesTheFieldsOut(t *testing.T) {
	e := evidenceOfKind(t, run(t, allSpans(4, 4, badTrace), touchingService, symptom), "repeated_query")[0]
	for _, k := range []string{"collapsed_parent_name", "collapsed_parent_count"} {
		if _, ok := e[k]; ok {
			t.Errorf("%s present without collapse: %v", k, e)
		}
	}
}

func TestAnalyze_CollapseKeepsConfidenceScoreAndDBSpansMedian(t *testing.T) {
	pooled := run(t, allSpans(4, 4, pooledBadTrace), touchingService, symptom)
	flat := run(t, allSpans(4, 4, badTrace), touchingService, symptom)
	pc, fc := obj(t, list(pooled, "candidates")[0]), obj(t, list(flat, "candidates")[0])
	if pc["confidence"] != fc["confidence"] || pc["score"] != fc["score"] {
		t.Fatalf("pool %v %v, direct %v %v: want the same confidence and score", pc["confidence"], pc["score"], fc["confidence"], fc["score"])
	}
	median := func(m map[string]any) any {
		for _, s := range evidenceOfKind(t, m, "symptom_stats") {
			if s["version"] == bad {
				return s["db_query_spans_median"]
			}
		}
		return nil
	}
	if median(pooled) != float64(11) || median(flat) != float64(11) {
		t.Fatalf("db_query_spans_median pool %v, direct %v, want 11 in both", median(pooled), median(flat))
	}
}
