//go:build e2e

// End to end: needs Docker, network for module downloads, and takes minutes.
// Run with: go test -tags e2e ./internal/recording/
package recording_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/goindex"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/otlpfile"
	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
	"github.com/MarcosAAlbanoJunior/rootline/internal/recording"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

type fileSource []trace.Span

func (f fileSource) Spans() ([]trace.Span, error) { return f, nil }

func TestRecordAnalyzeScore_N1BasicEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	err = recording.Record(ctx, recording.RecordOptions{
		ScenarioDir: filepath.Join(root, "eval", "fixtures", "scenarios", "n1-basic"),
		SourceDir:   root,
		OutDir:      out,
	})
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := recording.ReadManifest(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	shaOf := map[string]string{}
	labelOf := map[string]string{}
	for _, c := range manifest.Commits {
		shaOf[c.Label] = c.SHA
		labelOf[c.SHA] = c.Label
	}

	spans, err := otlpfile.ReadFile(filepath.Join(out, "traces.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	git := gitcli.New(filepath.Join(out, "repo"))
	res, err := analysis.Analyze(fileSource(spans), goindex.New(git), git, nil, analysis.Symptom{
		Service:         "demo-app",
		Operation:       trace.Operation{Method: "GET", Route: "/customers/{id}/orders", RouteSource: "span_name"},
		BadVersion:      shaOf["regressao"],
		BaselineVersion: shaOf["referencia"],
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(res)
	var m struct {
		Status string `json:"status"`
		Cause  *struct {
			Type       string `json:"type"`
			Confidence string `json:"confidence"`
			Location   *struct {
				Function string `json:"function"`
				File     string `json:"file"`
				Line     int    `json:"line"`
			} `json:"location"`
			Commit *struct {
				SHA   string `json:"sha"`
				Touch string `json:"touch"`
			} `json:"commit"`
		} `json:"cause"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Status != "cause_found" || m.Cause == nil || m.Cause.Location == nil || m.Cause.Commit == nil {
		t.Fatalf("incomplete result: %s", raw)
	}
	key := manifest.AnswerKey
	if m.Cause.Location.File != "demo-app/internal/orders/service.go" || m.Cause.Location.File != key.File || m.Cause.Location.Line != key.Line {
		t.Errorf("location %s:%d, want %s:%d", m.Cause.Location.File, m.Cause.Location.Line, key.File, key.Line)
	}
	if m.Cause.Confidence != "high" {
		t.Errorf("confidence = %q, want high", m.Cause.Confidence)
	}
	if labelOf[m.Cause.Commit.SHA] != "regressao" || m.Cause.Commit.Touch != "location" {
		t.Errorf("commit = %+v (label %q), want regressao with touch location", m.Cause.Commit, labelOf[m.Cause.Commit.SHA])
	}

	outcome := evaluation.ScoreOne(
		evaluation.Key{CauseType: key.Cause, Function: key.Function, File: key.File, Line: key.Line, CommitLabel: key.CommitLabel},
		evaluation.Found{
			HasCause: true, CauseType: m.Cause.Type, Function: m.Cause.Location.Function,
			HasLocation: true, File: m.Cause.Location.File, Line: m.Cause.Location.Line, CommitSHA: m.Cause.Commit.SHA,
		}, labelOf)
	var score evaluation.Score
	score.Add(outcome)
	one := evaluation.Metric{Hits: 1, Total: 1}
	if score.Cause != one || score.Line != one || score.Commit != one || score.Strict != one {
		t.Fatalf("score = %+v (cause %s, line %s, commit %s, strict %s), want 1/1 everywhere",
			score, score.Cause, score.Line, score.Commit, score.Strict)
	}
}
