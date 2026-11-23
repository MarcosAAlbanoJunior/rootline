// ADR-0020: the effective parent skips one level of repeated wrapper spans that only have database
// children.
package detector_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

const jobName = "workpool.job"

func plainSpan(traceID, id, parent, name string, kind, startMs int) trace.Span {
	return trace.Span{
		TraceID: traceID, SpanID: id, ParentSpanID: parent, Name: name, Kind: kind,
		Start:   base.Add(time.Duration(startMs) * time.Millisecond),
		End:     base.Add(time.Duration(startMs)*time.Millisecond + time.Millisecond),
		Attrs:   map[string]any{},
		Service: "demo-app",
	}
}

// dbClientSpan is a database client span without a query, like otelpgx's pool.acquire and connect;
// attr is db.system.name or the legacy db.system.
func dbClientSpan(traceID, id, parent, name, attr string, startMs int) trace.Span {
	s := plainSpan(traceID, id, parent, name, trace.KindClient, startMs)
	s.Attrs[attr] = "postgresql"
	return s
}

func serverSpan(traceID string) trace.Span {
	return plainSpan(traceID, traceID+"-srv", "", "GET /customers/{id}/shipments", trace.KindServer, 0)
}

// pool builds a trace with n workpool.job spans under the server span, each with a pool.acquire
// span and one query, as otelpgx records them; with plusOne, the orders query (10 rows) runs
// directly under the server before the jobs.
func pool(traceID string, n int, plusOne bool) []trace.Span {
	srv := serverSpan(traceID)
	spans := []trace.Span{srv}
	if plusOne {
		spans = append(spans, dbSpan(traceID, traceID+"-plus", srv.SpanID, 1, ordersQuery, 10))
	}
	for i := 0; i < n; i++ {
		job := plainSpan(traceID, fmt.Sprintf("%s-job%d", traceID, i), srv.SpanID, jobName, trace.KindInternal, 10+i*3)
		spans = append(spans, job,
			dbClientSpan(traceID, fmt.Sprintf("%s-acq%d", traceID, i), job.SpanID, "pool.acquire", "db.system.name", 10+i*3),
			dbSpan(traceID, fmt.Sprintf("%s-q%d", traceID, i), job.SpanID, 11+i*3, itemsQuery, 3))
	}
	return spans
}

// direct is pool without the job spans: the queries are children of the server span.
func direct(traceID string, n int, plusOne bool) []trace.Span {
	var out []trace.Span
	for _, s := range pool(traceID, n, plusOne) {
		switch {
		case s.Name == jobName || s.Name == "pool.acquire":
			continue
		case s.ParentSpanID != "" && s.ParentSpanID != traceID+"-srv":
			s.ParentSpanID = traceID + "-srv"
		}
		out = append(out, s)
	}
	return out
}

func queriesOf(spans []trace.Span) []trace.Span {
	var out []trace.Span
	for _, s := range spans {
		if detector.PhaseOf(s) == detector.PhaseQuery {
			out = append(out, s)
		}
	}
	return out
}

type effective struct {
	parent, name string
	count        int
}

func effectiveParents(spans []trace.Span) map[string]effective {
	tree := trace.NewTree(spans)
	out := map[string]effective{}
	for _, q := range queriesOf(spans) {
		p, name, n := detector.EffectiveParent(tree, q)
		out[q.SpanID] = effective{p, name, n}
	}
	return out
}

func noCollapse(t *testing.T, spans []trace.Span) {
	t.Helper()
	byID := map[string]trace.Span{}
	for _, s := range spans {
		byID[s.SpanID] = s
	}
	for id, got := range effectiveParents(spans) {
		if want := (effective{byID[id].ParentSpanID, "", 0}); got != want {
			t.Errorf("query %s: got %+v, want %+v (no collapse)", id, got, want)
		}
	}
}

func TestEffectiveParent_PoolOfJobsCollapsesToTheGrandparent(t *testing.T) {
	got := effectiveParents(pool("t", 10, false))
	if len(got) != 10 {
		t.Fatalf("got %d queries, want 10", len(got))
	}
	for id, e := range got {
		if want := (effective{"t-srv", jobName, 10}); e != want {
			t.Errorf("query %s: got %+v, want %+v", id, e, want)
		}
	}
}

func TestEffectiveParent_SingleWrapperDoesNotCollapse(t *testing.T) {
	noCollapse(t, pool("t", 1, false))
}

func TestEffectiveParent_ServerParentDoesNotCollapse(t *testing.T) {
	noCollapse(t, plusOneThenRepeats("t", 10, 10, 3))
	noCollapse(t, direct("t", 10, true))
}

func TestEffectiveParent_RepeatedSpansWithANonDatabaseChildDoNotCollapse(t *testing.T) {
	spans := pool("t", 3, false)
	spans = append(spans, plainSpan("t", "t-http", "t-job1", "GET", trace.KindClient, 15))
	noCollapse(t, spans)
}

func TestEffectiveParent_ConnectChildCountsAsDatabase(t *testing.T) {
	spans := pool("t", 3, false)
	spans = append(spans, dbClientSpan("t", "t-connect", "t-job1", "connect", "db.system.name", 10))
	for id, e := range effectiveParents(spans) {
		if want := (effective{"t-srv", jobName, 3}); e != want {
			t.Errorf("query %s: got %+v, want %+v", id, e, want)
		}
	}
}

func TestEffectiveParent_ClientChildWithLegacyDBSystemCountsAsDatabase(t *testing.T) {
	spans := pool("t", 3, false)
	spans = append(spans, dbClientSpan("t", "t-legacy", "t-job2", "pool.acquire", "db.system", 15))
	for id, e := range effectiveParents(spans) {
		if want := (effective{"t-srv", jobName, 3}); e != want {
			t.Errorf("query %s: got %+v, want %+v", id, e, want)
		}
	}
}

func TestEffectiveParent_WrapperThatIsNotAPlainInternalSpanDoesNotCollapse(t *testing.T) {
	t.Run("wrapper kind CLIENT", func(t *testing.T) {
		spans := pool("t", 3, false)
		for i := range spans {
			if spans[i].Name == jobName {
				spans[i].Kind = trace.KindClient
			}
		}
		noCollapse(t, spans)
	})
	t.Run("wrapper is a database span", func(t *testing.T) {
		spans := pool("t", 3, false)
		for i := range spans {
			if spans[i].Name == jobName {
				spans[i].Attrs["db.query.text"] = "BEGIN"
			}
		}
		noCollapse(t, spans)
	})
}

func TestEffectiveParent_PrepareChildrenAndChildlessHomonymsStillCollapse(t *testing.T) {
	spans := pool("t", 3, false)
	spans = append(spans,
		dbSpan("t", "t-prep", "t-job0", 10, itemsQuery, 0, phase("prepare")),
		plainSpan("t", "t-job-empty", "t-srv", jobName, trace.KindInternal, 30))
	for id, e := range effectiveParents(spans) {
		if want := (effective{"t-srv", jobName, 4}); e != want {
			t.Errorf("query %s: got %+v, want %+v", id, e, want)
		}
	}
}

// Limitation (ADR-0020): a pool inside a pool collapses only the inner jobs, one level up to the
// outer job, never to the server span.
func TestEffectiveParentClimbsOnlyOneLevel(t *testing.T) {
	srv := serverSpan("t")
	spans := []trace.Span{srv}
	outerOf := map[string]string{}
	for o := 0; o < 2; o++ {
		outer := plainSpan("t", fmt.Sprintf("outer%d", o), srv.SpanID, "outer.job", trace.KindInternal, o*20)
		spans = append(spans, outer)
		for i := 0; i < 3; i++ {
			inner := plainSpan("t", fmt.Sprintf("inner%d-%d", o, i), outer.SpanID, "inner.job", trace.KindInternal, o*20+i*3+1)
			q := dbSpan("t", fmt.Sprintf("q%d-%d", o, i), inner.SpanID, o*20+i*3+2, itemsQuery, 3)
			spans = append(spans, inner, q)
			outerOf[q.SpanID] = outer.SpanID
		}
	}
	for id, e := range effectiveParents(spans) {
		if want := (effective{outerOf[id], "inner.job", 3}); e != want {
			t.Errorf("query %s: got %+v, want %+v", id, e, want)
		}
	}
}

func TestEffectiveParent_MissingGrandparentKeepsTheDirectParent(t *testing.T) {
	var spans []trace.Span
	for _, s := range pool("t", 5, false) {
		if s.SpanID != "t-srv" {
			spans = append(spans, s)
		}
	}
	noCollapse(t, spans)
}

func TestFindOccurrences_JobsWithTwoQueriesGiveOneGroupPerQueryUnderTheGrandparent(t *testing.T) {
	const otherQuery = "SELECT sum(total) FROM payments WHERE order_id = $1"
	spans := pool("t", 6, false)
	for i := 0; i < 6; i++ {
		spans = append(spans, dbSpan("t", fmt.Sprintf("t-other%d", i), fmt.Sprintf("t-job%d", i), 12+i*3, otherQuery, 1))
	}
	occ := detector.FindOccurrences(spans)
	if len(occ) != 2 {
		t.Fatalf("got %d occurrences, want 2 (one per query)", len(occ))
	}
	seen := map[string]bool{}
	for _, o := range occ {
		seen[o.Query] = true
		if o.ParentSpanID != "t-srv" || len(o.Spans) != 6 || o.CollapsedParentName != jobName || o.CollapsedParentCount != 6 {
			t.Errorf("occurrence %q: parent %q, %d spans, collapsed %q x%d", o.Query, o.ParentSpanID, len(o.Spans), o.CollapsedParentName, o.CollapsedParentCount)
		}
	}
	if !seen[itemsQuery] || !seen[otherQuery] {
		t.Fatalf("got queries %v, want both", seen)
	}
}

func TestFindOccurrences_PoolGroupFindsThePlusOneUnderTheGrandparent(t *testing.T) {
	occ := itemsOccurrences(t, pool("t", 10, true))
	if len(occ) != 1 {
		t.Fatalf("want 1 occurrence, got %d", len(occ))
	}
	o := occ[0]
	if len(o.Spans) != 10 || !o.PlusOne || o.ParentSpanID != "t-srv" {
		t.Fatalf("got %d spans, PlusOne %v, parent %q; want 10, true, t-srv", len(o.Spans), o.PlusOne, o.ParentSpanID)
	}
	if o.CollapsedParentName != jobName || o.CollapsedParentCount != 10 {
		t.Fatalf("collapsed %q x%d, want %q x10", o.CollapsedParentName, o.CollapsedParentCount, jobName)
	}
}

func TestFindOccurrences_DirectGroupHasNoCollapse(t *testing.T) {
	occ := itemsOccurrences(t, direct("t", 10, true))
	if len(occ) != 1 || occ[0].CollapsedParentName != "" || occ[0].CollapsedParentCount != 0 {
		t.Fatalf("got %+v, want one occurrence without collapse", occ)
	}
}

// The healthy route of the pool: two jobs with different queries collapse to the server span, and
// that alone creates no candidate.
func TestDetect_HealthyPoolRouteCollapsesWithoutACandidate(t *testing.T) {
	const totalQuery = "SELECT sum(total) FROM orders WHERE customer_id = $1"
	healthyPool := func(id string) []trace.Span {
		srv := serverSpan(id)
		j0 := plainSpan(id, id+"-job0", srv.SpanID, jobName, trace.KindInternal, 1)
		j1 := plainSpan(id, id+"-job1", srv.SpanID, jobName, trace.KindInternal, 1)
		return []trace.Span{srv, j0, j1,
			dbClientSpan(id, id+"-acq0", j0.SpanID, "pool.acquire", "db.system.name", 1),
			dbClientSpan(id, id+"-acq1", j1.SpanID, "pool.acquire", "db.system.name", 1),
			dbSpan(id, id+"-recent", j0.SpanID, 2, ordersQuery, 10),
			dbSpan(id, id+"-total", j1.SpanID, 2, totalQuery, 1)}
	}
	for id, e := range effectiveParents(healthyPool("h")) {
		if want := (effective{"h-srv", jobName, 2}); e != want {
			t.Errorf("query %s: got %+v, want %+v", id, e, want)
		}
	}
	cands, _, err := detector.Detect(loopAtService, "abc1234", traces(5, 5, healthyPool))
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 0 {
		t.Fatalf("got %d candidates, want 0", len(cands))
	}
}

func TestDetect_CollapseIsEvidenceWithoutChangingConfidenceOrScore(t *testing.T) {
	pooled := mustOne(t, traces(4, 4, func(id string) []trace.Span { return pool(id, 10, true) }))
	flat := mustOne(t, traces(4, 4, func(id string) []trace.Span { return direct(id, 10, true) }))
	if pooled.Confidence != flat.Confidence || pooled.Score != flat.Score {
		t.Fatalf("pool: %q %v; direct: %q %v; want the same confidence and score", pooled.Confidence, pooled.Score, flat.Confidence, flat.Score)
	}
	if pooled.Confidence != "high" || pooled.Score != 1.5 {
		t.Fatalf("got %q %v, want high 1.5", pooled.Confidence, pooled.Score)
	}
	if pooled.CollapsedParentName != jobName || pooled.CollapsedParentCount != 10 || pooled.SampleParentSpanID != "tr0-srv" {
		t.Fatalf("pool candidate: collapsed %q x%d, sample parent %q", pooled.CollapsedParentName, pooled.CollapsedParentCount, pooled.SampleParentSpanID)
	}
	if flat.CollapsedParentName != "" || flat.CollapsedParentCount != 0 {
		t.Fatalf("direct candidate: collapsed %q x%d, want none", flat.CollapsedParentName, flat.CollapsedParentCount)
	}
}
