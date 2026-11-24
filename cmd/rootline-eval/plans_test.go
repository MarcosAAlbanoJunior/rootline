package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlansOf_RecordingWithPlansServesThem(t *testing.T) {
	rec := t.TempDir()
	line := `{"version":"bad1234","query":"SELECT 1","plan":[{"Plan":{"Node Type":"Result"}}],"reltuples":{}}` + "\n"
	if err := os.WriteFile(filepath.Join(rec, "plans.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	plans, err := plansOf(rec)

	if err != nil || plans == nil {
		t.Fatalf("plans %v, err %v", plans, err)
	}
	if _, found, err := plans.Plan("bad1234", "SELECT 1"); !found || err != nil {
		t.Errorf("found %v, err %v; want the recorded plan", found, err)
	}
}

func TestPlansOf_RecordingWithoutPlansGivesNoExplainerAndNoError(t *testing.T) {
	plans, err := plansOf(t.TempDir())
	if err != nil || plans != nil {
		t.Fatalf("plans %v, err %v; want nil, nil (the analysis then warns)", plans, err)
	}
}

func TestPlansOf_MalformedPlansFileIsAnError(t *testing.T) {
	rec := t.TempDir()
	if err := os.WriteFile(filepath.Join(rec, "plans.jsonl"), []byte("{not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := plansOf(rec); err == nil {
		t.Fatal("want an error")
	}
}
