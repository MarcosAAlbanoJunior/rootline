package recording

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func dbSpan(version, text, phase string) trace.Span {
	return trace.Span{Version: version, Attrs: map[string]any{"db.query.text": text, "rootline.db.phase": phase}}
}

func TestQueriesOf_DistinctNormalizedQueriesOfTheVersion(t *testing.T) {
	spans := []trace.Span{
		dbSpan("v1", "SELECT id FROM orders\n  WHERE customer_id = $1", "query"),
		dbSpan("v1", "SELECT id FROM orders WHERE customer_id = $1", "query"),
		dbSpan("v1", "SELECT * FROM items WHERE order_id = $1", "prepare"),
		dbSpan("v1", "BEGIN", "query"),
		dbSpan("v2", "SELECT 2", "query"),
		{Version: "v1", Attrs: map[string]any{}},
	}

	got := queriesOf(spans, "v1")

	want := "[{BEGIN BEGIN} {SELECT id FROM orders WHERE customer_id = $1 SELECT id FROM orders\n  WHERE customer_id = $1}]"
	if fmt.Sprint(got) != want {
		t.Fatalf("got %q\nwant %q", fmt.Sprint(got), want)
	}
}

func TestPlanLine_KeepsTheRawPlanAndTheSizesOfItsTables(t *testing.T) {
	out := `[
  {
    "Plan": {"Node Type": "Nested Loop", "Plans": [
      {"Node Type": "Seq Scan", "Relation Name": "orders", "Plan Rows": 10, "Filter": "(customer_id = $1)"},
      {"Node Type": "Index Scan", "Relation Name": "customers", "Plan Rows": 1}]}
  }
]
`
	sizes := map[string]float64{"orders": 50000, "customers": 2000, "order_items": 150000}

	l, err := planLine("v1", "q", out, sizes)

	if err != nil {
		t.Fatal(err)
	}
	if l.Version != "v1" || l.Query != "q" || !l.GenericPlan || l.Unexplainable != "" {
		t.Errorf("line = %+v", l)
	}
	if fmt.Sprint(l.RelTuples) != "map[customers:2000 orders:50000]" {
		t.Errorf("reltuples = %v, want only the tables of the plan", l.RelTuples)
	}
	if strings.Contains(string(l.Plan), "\n") || !strings.Contains(string(l.Plan), `"Seq Scan"`) {
		t.Errorf("plan = %s, want the raw plan compacted to one line", l.Plan)
	}
}

func TestPlanLine_OutputThatIsNotJSONFails(t *testing.T) {
	if _, err := planLine("v1", "q", "ERROR: syntax error", nil); err == nil {
		t.Fatal("want an error")
	}
}

// fakePostgres answers ANALYZE + reltuples, and EXPLAIN of the known queries; anything else fails.
func fakePostgres(explained map[string]string) sqlRunner {
	return func(_ context.Context, statements ...string) (string, error) {
		last := statements[len(statements)-1]
		switch {
		case statements[0] == "ANALYZE":
			return `{"orders": 50000}` + "\n", nil
		case strings.HasPrefix(last, "EXPLAIN (GENERIC_PLAN, FORMAT JSON) "):
			if statements[0] != "SET default_transaction_read_only = on" {
				return "", errors.New("EXPLAIN outside a read-only session")
			}
			if out, ok := explained[strings.TrimPrefix(last, "EXPLAIN (GENERIC_PLAN, FORMAT JSON) ")]; ok {
				return out, nil
			}
			return "", errors.New("psql: ERROR:  relation \"nope\" does not exist\nLINE 1: ...")
		}
		return "", errors.New("unexpected statements")
	}
}

func TestExplainVersion_OneLinePerQueryWithAPlanOrAReason(t *testing.T) {
	ordersQ := "SELECT id FROM orders WHERE customer_id = $1"
	spans := []trace.Span{
		dbSpan("v1", ordersQ, "query"),
		dbSpan("v1", "INSERT INTO orders (id) VALUES ($1)", "query"),
		dbSpan("v1", "SELECT * FROM nope", "query"),
	}
	run := fakePostgres(map[string]string{ordersQ: `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"orders"}}]`})

	lines, err := explainVersion(context.Background(), run, "v1", spans)

	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %+v", len(lines), lines)
	}
	byQuery := map[string]int{}
	for i, l := range lines {
		byQuery[l.Query] = i
	}
	if l := lines[byQuery[ordersQ]]; len(l.Plan) == 0 || l.RelTuples["orders"] != 50000 || l.Unexplainable != "" {
		t.Errorf("orders line = %+v, want the plan with the table size", l)
	}
	if l := lines[byQuery["INSERT INTO orders (id) VALUES ($1)"]]; l.Unexplainable != "not a SELECT: INSERT" || len(l.Plan) != 0 {
		t.Errorf("insert line = %+v, want the validator reason", l)
	}
	if l := lines[byQuery["SELECT * FROM nope"]]; !strings.HasPrefix(l.Unexplainable, "explain failed: ") || strings.Contains(l.Unexplainable, "\n") {
		t.Errorf("failed line = %+v, want 'explain failed:' on one line", l)
	}
}

func TestExplainVersion_AnalyzeFailureStopsTheRecording(t *testing.T) {
	run := func(context.Context, ...string) (string, error) { return "", errors.New("connection refused") }
	if _, err := explainVersion(context.Background(), run, "v1", nil); err == nil {
		t.Fatal("want an error")
	}
}

func TestVersionTraces_AVersionWithoutItsOwnSpansIsAnError(t *testing.T) {
	if err := versionTraces([]trace.Span{dbSpan("v1", "SELECT 1", "query")}, "v1"); err != nil {
		t.Errorf("v1 has spans: %v", err)
	}
	for _, spans := range [][]trace.Span{nil, {dbSpan("v0", "SELECT 1", "query")}} {
		if err := versionTraces(spans, "v1"); err == nil {
			t.Errorf("spans %v: want an error for v1", spans)
		}
	}
}
