package analysis_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

type fakePlans map[string]detector.QueryPlan

func (f fakePlans) Plan(_, q string) (detector.QueryPlan, bool, error) {
	p, ok := f[q]
	return p, ok, nil
}

var seqScanOnOrders = fakePlans{orders: {
	Plan:      json.RawMessage(`[{"Plan":{"Node Type":"Seq Scan","Relation Name":"orders","Plan Rows":10,"Filter":"(customer_id = $1)"}}]`),
	RelTuples: map[string]float64{"orders": 50000},
	Generic:   true,
}}

// singleQueryTrace runs the orders query once, with no repetition.
func singleQueryTrace(id string) []trace.Span {
	srv := server(id, bad)
	return []trace.Span{srv, query(id, id+"-q", srv.SpanID, 100, orders, 10)}
}

func analyzeJSON(t *testing.T, src source, plans analysis.Explainer) map[string]any {
	t.Helper()
	res, err := analysis.Analyze(src, fakeIndex{}, fakeGit{}, plans, symptom)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	checkEvidenceRefs(t, m)
	return m
}

func TestAnalyze_SelectiveSeqScanBecomesAMissingIndexCauseWithTheTableAsTarget(t *testing.T) {
	m := analyzeJSON(t, allSpans(4, 4, singleQueryTrace), seqScanOnOrders)

	cause := obj(t, m["cause"])
	if cause["type"] != "missing_index" || cause["target"] != "orders" {
		t.Fatalf("cause = %v, want missing_index on orders", cause)
	}
	loc := obj(t, cause["location"])
	if loc["level"] != 1.0 || loc["file"] != repoFile || loc["line"] != 30.0 || loc["function"] != repoFn {
		t.Errorf("location = %v, want level 1 at the repository call", loc)
	}
	if _, has := loc["in_loop"]; has {
		t.Errorf("location = %v, want no in_loop (the strategy does not check loops)", loc)
	}
	for _, w := range m["warnings"].([]any) {
		if s, _ := w.(string); len(s) >= 15 && s[:15] == "loop_not_found:" {
			t.Errorf("warnings = %v, want no loop_not_found", m["warnings"])
		}
	}
	cand := obj(t, m["candidates"].([]any)[0])
	if cand["target"] != "orders" {
		t.Errorf("candidate = %v, want target orders", cand)
	}
	var plan map[string]any
	for _, e := range m["evidence"].([]any) {
		if ev := obj(t, e); ev["kind"] == "query_plan" {
			plan = ev
		}
	}
	if plan == nil {
		t.Fatalf("no query_plan evidence in %v", m["evidence"])
	}
	if plan["query"] != orders || plan["node_type"] != "Seq Scan" || plan["table"] != "orders" ||
		plan["reltuples"] != 50000.0 || plan["plan_rows"] != 10.0 || plan["selectivity"] != 10.0/50000 {
		t.Errorf("query_plan evidence = %v", plan)
	}
	if plan["filter"] != "(customer_id = $1)" || fmt.Sprint(plan["filter_columns"]) != "[customer_id]" || plan["generic_plan"] != true {
		t.Errorf("query_plan evidence = %v, want the raw filter, its columns and the generic mark", plan)
	}
}

func TestAnalyze_WithoutPlansThereIsNoMissingIndexCandidateAndItSaysSo(t *testing.T) {
	m := analyzeJSON(t, allSpans(4, 4, singleQueryTrace), nil)
	if len(m["candidates"].([]any)) != 0 {
		t.Fatalf("candidates = %v, want none without plans", m["candidates"])
	}
	if n := countString(m["warnings"], "missing_index_skipped: no query plans"); n != 1 {
		t.Errorf("warnings = %v, want missing_index_skipped: no query plans once", m["warnings"])
	}
	if n := countString(m["observations"], "missing_index_skipped: no query plans"); n != 0 {
		t.Errorf("observations = %v, want the notice only in warnings", m["observations"])
	}
}

func TestAnalyze_WithoutPlansTheNPlusOneCandidatesDoNotChange(t *testing.T) {
	without := analyzeJSON(t, allSpans(4, 4, badTrace), nil)
	with := analyzeJSON(t, allSpans(4, 4, badTrace), fakePlans{})
	if fmt.Sprint(without["candidates"]) != fmt.Sprint(with["candidates"]) {
		t.Fatalf("candidates without plans %v, with plans %v", without["candidates"], with["candidates"])
	}
}

func TestAnalyze_QueriesWithoutAPlanAreReportedOnceEach(t *testing.T) {
	plans := fakePlans{items: {Unexplainable: "not a single SELECT"}}
	m := analyzeJSON(t, allSpans(4, 4, badTrace), plans)

	for _, w := range []string{
		"missing_index_skipped: " + orders + ": no recorded plan",
		"missing_index_skipped: " + items + ": not a single SELECT",
	} {
		if n := countString(m["warnings"], w); n != 1 {
			t.Errorf("warnings = %v, want %q once", m["warnings"], w)
		}
	}
	if n := countString(m["warnings"], "missing_index_skipped: no query plans"); n != 0 {
		t.Errorf("warnings = %v, want no 'no query plans' when plans were given", m["warnings"])
	}
}

func countString(list any, s string) int {
	n := 0
	for _, v := range list.([]any) {
		if v == s {
			n++
		}
	}
	return n
}

func TestAnalyze_NPlusOneHasNoTarget(t *testing.T) {
	m := analyzeJSON(t, allSpans(4, 4, badTrace), nil)
	cause := obj(t, m["cause"])
	if _, has := cause["target"]; has || cause["type"] != "n_plus_one" {
		t.Fatalf("cause = %v, want n_plus_one without target", cause)
	}
	if _, has := obj(t, cause["location"])["in_loop"]; !has {
		t.Errorf("n+1 location = %v, want in_loop", cause["location"])
	}
}
