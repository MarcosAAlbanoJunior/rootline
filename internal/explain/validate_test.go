package explain_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/explain"
)

func TestUnexplainable_AcceptsASingleSelect(t *testing.T) {
	for _, q := range []string{
		"SELECT id FROM orders WHERE customer_id = $1",
		"  select 1",
		"WITH recent AS (SELECT id FROM orders) SELECT count(*) FROM recent",
		"SELECT id FROM orders;",
	} {
		if reason := explain.Unexplainable(q); reason != "" {
			t.Errorf("Unexplainable(%q) = %q, want accepted", q, reason)
		}
	}
}

func TestUnexplainable_RefusesWithAReason(t *testing.T) {
	for _, q := range []string{
		"INSERT INTO orders (id) VALUES ($1)",
		"UPDATE orders SET placed_at = now()",
		"DELETE FROM orders",
		"SELECT 1; DROP TABLE orders",
		"SET search_path = public",
		"BEGIN",
		"",
	} {
		if reason := explain.Unexplainable(q); reason == "" {
			t.Errorf("Unexplainable(%q) accepted, want a reason", q)
		}
	}
}
