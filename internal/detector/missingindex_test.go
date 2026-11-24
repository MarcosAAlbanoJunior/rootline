package detector_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// fakePlans serves one plan per normalized query.
type fakePlans map[string]detector.QueryPlan

func (f fakePlans) Plan(_, query string) (detector.QueryPlan, bool, error) {
	p, ok := f[query]
	return p, ok, nil
}

func scanPlan(node, table string, rows float64, extra string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`[{"Plan":{"Node Type":%q,"Relation Name":%q,"Alias":%q,"Plan Rows":%v%s}}]`,
		node, table, table, rows, extra))
}

const filter = `,"Filter":"(customer_id = $1)"`

func plansFor(plan json.RawMessage, reltuples float64) fakePlans {
	return fakePlans{ordersQuery: {Plan: plan, RelTuples: map[string]float64{"orders": reltuples}}}
}

// singleQuery builds n traces, `with` of which run ordersQuery once.
func singleQuery(n, with int) [][]trace.Span {
	var out [][]trace.Span
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("tr%d", i)
		q := ordersQuery
		if i >= with {
			q = "SELECT 1"
		}
		out = append(out, []trace.Span{dbSpan(id, id+"-q", "root", 0, q, 1)})
	}
	return out
}

func TestDetectMissingIndex_SelectiveSeqScanOnALargeTableIsACandidate(t *testing.T) {
	plans := plansFor(scanPlan("Seq Scan", "orders", 10, filter), 20000)

	cands, err := detector.DetectMissingIndex(loopAtService, plans, "abc1234", singleQuery(4, 4))

	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1", len(cands))
	}
	c := cands[0]
	if c.Type != detector.TypeMissingIndex || c.Target != "orders" || c.Query != ordersQuery {
		t.Errorf("got type %q target %q query %q", c.Type, c.Target, c.Query)
	}
	if c.Location == nil || c.Location.Function != repoFunc || c.Location.Line != 30 || c.Location.LoopChecked {
		t.Errorf("location = %+v, want the innermost frame, loop not checked", c.Location)
	}
	if hasPrefix(c.Warnings, "loop_not_found:") {
		t.Errorf("warnings = %q, want no loop_not_found", c.Warnings)
	}
	if c.Score != 1 || c.TracesAffected != 4 || c.TracesTotal != 4 {
		t.Errorf("score %v, affected %d/%d; want 1, 4/4", c.Score, c.TracesAffected, c.TracesTotal)
	}
	p := c.Plan
	if p == nil || p.NodeType != "Seq Scan" || p.Table != "orders" || p.RelTuples != 20000 || p.PlanRows != 10 || p.Selectivity != 10.0/20000 {
		t.Errorf("plan finding = %+v", p)
	}
}

func TestDetectMissingIndex_NotACandidate(t *testing.T) {
	tests := []struct {
		name      string
		plan      json.RawMessage
		reltuples float64
	}{
		{"index scan on a large table", scanPlan("Index Scan", "orders", 10, `,"Index Cond":"(customer_id = $1)"`), 20000},
		{"seq scan not selective", scanPlan("Seq Scan", "orders", 6000, `,"Filter":"(amount > $1)"`), 20000},
		{"selective seq scan on a small table", scanPlan("Seq Scan", "orders", 1, filter), 9999},
		{"seq scan without filter", scanPlan("Seq Scan", "orders", 10, ""), 20000},
		{"table never analyzed", scanPlan("Seq Scan", "orders", 10, filter), -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cands, err := detector.DetectMissingIndex(fakeEnv{}, plansFor(tc.plan, tc.reltuples), "abc1234", singleQuery(4, 4))
			if err != nil {
				t.Fatal(err)
			}
			if len(cands) != 0 {
				t.Fatalf("got %d candidates, want none: %+v", len(cands), cands[0])
			}
		})
	}
}

func TestDetectMissingIndex_SeqScanNestedUnderAJoinIsFound(t *testing.T) {
	plan := json.RawMessage(`[{"Plan":{"Node Type":"Nested Loop","Plan Rows":10,"Plans":[
		{"Node Type":"Index Scan","Relation Name":"customers","Plan Rows":1,"Index Cond":"(id = $1)"},
		{"Node Type":"Seq Scan","Relation Name":"orders","Plan Rows":10,"Filter":"(customer_id = $1)"}]}}]`)
	plans := fakePlans{ordersQuery: {Plan: plan, RelTuples: map[string]float64{"orders": 50000, "customers": 20000}}}

	cands, err := detector.DetectMissingIndex(fakeEnv{}, plans, "abc1234", singleQuery(4, 4))

	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Target != "orders" {
		t.Fatalf("got %+v, want one candidate on orders", cands)
	}
}

func TestDetectMissingIndex_UsesTheN1AffectedTraceMinimums(t *testing.T) {
	plans := plansFor(scanPlan("Seq Scan", "orders", 10, filter), 20000)
	tests := []struct {
		n, with int
		want    int
	}{
		{4, 3, 1}, // 3 affected, 75%
		{4, 2, 0}, // below MinAffectedTraces
		{8, 3, 0}, // below MinAffectedRatio
	}
	for _, tc := range tests {
		cands, err := detector.DetectMissingIndex(fakeEnv{}, plans, "abc1234", singleQuery(tc.n, tc.with))
		if err != nil {
			t.Fatal(err)
		}
		if len(cands) != tc.want {
			t.Errorf("%d of %d traces: got %d candidates, want %d", tc.with, tc.n, len(cands), tc.want)
		}
	}
}

func TestDetectMissingIndex_QueryWithoutPlanIsSkipped(t *testing.T) {
	cands, err := detector.DetectMissingIndex(fakeEnv{}, fakePlans{}, "abc1234", singleQuery(4, 4))
	if err != nil || len(cands) != 0 {
		t.Fatalf("got %v, %v; want no candidates and no error", cands, err)
	}
}

func TestDetectMissingIndex_PrepareSpansAreNotCounted(t *testing.T) {
	plans := plansFor(scanPlan("Seq Scan", "orders", 10, filter), 20000)
	var trs [][]trace.Span
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("tr%d", i)
		trs = append(trs, []trace.Span{dbSpan(id, id+"-p", "root", 0, ordersQuery, 0, phase("prepare"))})
	}
	cands, err := detector.DetectMissingIndex(fakeEnv{}, plans, "abc1234", trs)
	if err != nil || len(cands) != 0 {
		t.Fatalf("got %v, %v; want no candidates from prepare spans", cands, err)
	}
}

func TestDetect_NPlusOneCandidatesHaveTheirType(t *testing.T) {
	c := mustOne(t, traces(4, 4, healthy))
	if c.Type != detector.TypeNPlusOne || c.Target != "" {
		t.Fatalf("got type %q target %q, want n_plus_one and no target", c.Type, c.Target)
	}
}
