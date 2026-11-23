// db_query_spans_median excludes prepare spans with the detector's rule.
package analysis_test

import (
	"fmt"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// mixedTrace has 3 query spans and 2 prepare spans. mark tells whether rootline.db.phase is set.
func mixedTrace(id string, mark bool) []trace.Span {
	srv := server(id, bad)
	spans := []trace.Span{srv}
	for i := 0; i < 3; i++ {
		spans = append(spans, query(id, fmt.Sprintf("%s-q%d", id, i), srv.SpanID, 100+i*20, orders, 10))
	}
	for i := 0; i < 2; i++ {
		p := query(id, fmt.Sprintf("%s-p%d", id, i), srv.SpanID, 300+i*20, items, 0)
		p.Attrs["pgx.prepare_stmt.name"] = fmt.Sprintf("stmt_%d", i)
		delete(p.Attrs, "pgx.rows_affected")
		p.Attrs["rootline.db.phase"] = "prepare"
		spans = append(spans, p)
	}
	if !mark {
		for i := range spans {
			delete(spans[i].Attrs, "rootline.db.phase")
		}
	}
	return spans
}

func badStats(t *testing.T, mark bool) float64 {
	t.Helper()
	var src source
	for i := 0; i < 3; i++ {
		src = append(src, mixedTrace(fmt.Sprintf("bad%02d", i), mark)...)
	}
	m := run(t, src, fakeGit{}, analysis.Symptom{Service: "demo-app", Operation: symptom.Operation, BadVersion: bad})
	stats := evidenceOfKind(t, m, "symptom_stats")
	if len(stats) != 1 {
		t.Fatalf("want one symptom_stats, got %d", len(stats))
	}
	return stats[0]["db_query_spans_median"].(float64)
}

func TestAnalyze_DBSpansMedianExcludesInferredPrepare(t *testing.T) {
	if got := badStats(t, false); got != 3 {
		t.Fatalf("db_query_spans_median = %v, want 3", got)
	}
}

func TestAnalyze_DBSpansMedianWithPhaseAttributeUnchanged(t *testing.T) {
	if got := badStats(t, true); got != 3 {
		t.Fatalf("db_query_spans_median = %v, want 3", got)
	}
}
