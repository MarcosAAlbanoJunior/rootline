package main

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestSchemaScripts_AllSQLFilesInLexicographicOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"schema/100_seed.sql":  {Data: []byte("-- seed")},
		"schema/000_base.sql":  {Data: []byte("-- base")},
		"schema/050_extra.sql": {Data: []byte("-- extra")},
		"schema/README.md":     {Data: []byte("not sql")},
	}

	got, err := schemaScripts(fsys)

	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "-- base|-- extra|-- seed" {
		t.Fatalf("got %q, want base, extra, seed", got)
	}
}

func TestSchemaScripts_EmbeddedBaseIndexesTheForeignKeys(t *testing.T) {
	got, err := schemaScripts(schemaFS)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(got, "\n")
	for _, idx := range []string{"ON orders (customer_id)", "ON order_items (order_id)"} {
		if !strings.Contains(all, idx) {
			t.Errorf("embedded schema has no index %q", idx)
		}
	}
}
