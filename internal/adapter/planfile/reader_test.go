package planfile_test

import (
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/planfile"
)

const ordersQuery = "SELECT id FROM orders WHERE customer_id = $1"

func TestReadFile_ServesThePlanAndTableSizesOfAVersion(t *testing.T) {
	plans, err := planfile.ReadFile("testdata/plans.jsonl")
	if err != nil {
		t.Fatal(err)
	}

	p, found, err := plans.Plan("bad1234", ordersQuery)

	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if !strings.Contains(string(p.Plan), `"Seq Scan"`) || p.RelTuples["orders"] != 50000 || p.Unexplainable != "" {
		t.Errorf("got %+v", p)
	}
	other, _, _ := plans.Plan("ba5e123", ordersQuery)
	if !strings.Contains(string(other.Plan), `"Index Scan"`) {
		t.Errorf("baseline plan = %s, want the Index Scan of its own version", other.Plan)
	}
}

func TestReadFile_UnexplainableQueryComesWithItsReasonAndNoError(t *testing.T) {
	plans, err := planfile.ReadFile("testdata/plans.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	p, found, err := plans.Plan("bad1234", "INSERT INTO orders (id) VALUES ($1)")
	if err != nil || !found || p.Unexplainable != "not a single SELECT" || len(p.Plan) != 0 {
		t.Fatalf("got %+v, found %v, err %v", p, found, err)
	}
}

func TestReadFile_QueryNotRecordedIsNotFound(t *testing.T) {
	plans, err := planfile.ReadFile("testdata/plans.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := plans.Plan("bad1234", "SELECT 1"); found || err != nil {
		t.Fatalf("found %v, err %v; want not found", found, err)
	}
}

func TestRead_MalformedLineFailsNamingTheLine(t *testing.T) {
	_, err := planfile.Read(strings.NewReader("{\"version\":\"a\",\"query\":\"q\"}\n{not json\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v, want an error naming line 2", err)
	}
}

func TestRead_LineWithoutVersionOrQueryFails(t *testing.T) {
	_, err := planfile.Read(strings.NewReader("{\"query\":\"q\"}\n"))
	if err == nil {
		t.Fatal("want an error for a line without version")
	}
}
