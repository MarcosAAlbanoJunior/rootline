// The score command per scenario: one line per scenario, per-scenario regressions and duplicate
// scenario names in the recordings.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
	"github.com/MarcosAAlbanoJunior/rootline/internal/recording"
)

var allHits = evaluation.Outcome{Cause: true, Line: true, Commit: true, Strict: true, LineLevel: 1}

func scenarioBoard(outcomes map[string]evaluation.Outcome) evaluation.Scoreboard {
	return evaluation.Scoreboard{"detectors_only": evaluation.ScoreScenarios(outcomes)}
}

func TestScore_PrintsOneLinePerScenarioInAlphabeticalOrderAfterTheAggregate(t *testing.T) {
	sb := scenarioBoard(map[string]evaluation.Outcome{
		"n1-helper": {Cause: true, Line: false, Commit: true, Strict: false, LineLevel: 2},
		"n1-basic":  allHits,
	})
	placar := writeBoard(t, sb)

	code, stdout, stderr := runScore(t, fixedScorer(sb, nil), "--recordings", "r", "--placar", placar)
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	basic := "  n1-basic: Causa=ok Linha=ok Commit=ok Estrito=ok Nível=1\n"
	helper := "  n1-helper: Causa=ok Linha=erro Commit=ok Estrito=erro Nível=2\n"
	levels := strings.Index(stdout, "Níveis da Linha")
	ib, ih := strings.Index(stdout, basic), strings.Index(stdout, helper)
	if levels < 0 || ib < 0 || ih < 0 {
		t.Fatalf("stdout must keep the aggregate and show both scenario lines, got:\n%s", stdout)
	}
	if levels >= ib || ib >= ih {
		t.Fatalf("scenario lines must follow the aggregate, alphabetically, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "(novo)") {
		t.Fatalf("no scenario is new, got:\n%s", stdout)
	}
}

func TestScore_ScenarioAbsentFromAReadableScoreboardIsMarkedNew(t *testing.T) {
	placar := writeBoard(t, scenarioBoard(map[string]evaluation.Outcome{"n1-basic": allHits}))
	current := scenarioBoard(map[string]evaluation.Outcome{"n1-basic": allHits, "n1-helper": allHits})

	code, stdout, stderr := runScore(t, fixedScorer(current, nil), "--recordings", "r", "--placar", placar)
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "  n1-helper: Causa=ok Linha=ok Commit=ok Estrito=ok Nível=1 (novo)\n") {
		t.Fatalf("the new scenario must end with (novo), got:\n%s", stdout)
	}
	if strings.Contains(stdout, "n1-basic: Causa=ok Linha=ok Commit=ok Estrito=ok Nível=1 (novo)") {
		t.Fatalf("a scenario in the scoreboard is not new, got:\n%s", stdout)
	}
}

func TestScore_OldFormatScoreboardMarksEveryScenarioNewAndPasses(t *testing.T) {
	placar := writeBoard(t, board(oneOfOne, oneOfOne, oneOfOne, oneOfOne))
	current := scenarioBoard(map[string]evaluation.Outcome{"n1-basic": allHits})

	code, stdout, stderr := runScore(t, fixedScorer(current, nil), "--recordings", "r", "--placar", placar)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d stderr %q, want 0", code, stderr)
	}
	if !strings.Contains(stdout, "  n1-basic: Causa=ok Linha=ok Commit=ok Estrito=ok Nível=1 (novo)\n") {
		t.Fatalf("got:\n%s", stdout)
	}
}

func TestScore_WithoutAReadableScoreboardNoScenarioIsMarkedNew(t *testing.T) {
	placar := filepath.Join(t.TempDir(), "placar.json")
	current := scenarioBoard(map[string]evaluation.Outcome{"n1-basic": allHits})

	code, stdout, stderr := runScore(t, fixedScorer(current, nil), "--recordings", "r", "--placar", placar, "--update")
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "  n1-basic: Causa=ok Linha=ok Commit=ok Estrito=ok Nível=1\n") {
		t.Fatalf("got:\n%s", stdout)
	}
}

func TestScore_ScenarioSwapIsARegressionEvenWithTheSameAggregate(t *testing.T) {
	placar := writeBoard(t, scenarioBoard(map[string]evaluation.Outcome{"A": allHits, "B": {}}))
	current := scenarioBoard(map[string]evaluation.Outcome{"A": {}, "B": allHits})

	code, _, stderr := runScore(t, fixedScorer(current, nil), "--recordings", "r", "--placar", placar)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, m := range []string{"cause", "line", "commit", "strict"} {
		if want := "rootline-eval: regression in detectors_only.A." + m + "\n"; !strings.Contains(stderr, want) {
			t.Errorf("stderr must contain %q, got %q", want, stderr)
		}
	}
	if strings.Contains(stderr, ".B") {
		t.Errorf("B improved, got %q", stderr)
	}
}

func TestScore_ScenarioMissingFromTheRecordingsIsARegression(t *testing.T) {
	placar := writeBoard(t, scenarioBoard(map[string]evaluation.Outcome{"A": allHits, "B": allHits}))
	current := scenarioBoard(map[string]evaluation.Outcome{"A": allHits})

	code, _, stderr := runScore(t, fixedScorer(current, nil), "--recordings", "r", "--placar", placar)
	if code != 1 || !strings.Contains(stderr, "rootline-eval: regression in detectors_only.B\n") {
		t.Fatalf("exit %d stderr %q, want 1 and detectors_only.B", code, stderr)
	}
}

func TestScore_UpdateWritesTheScenariosAndWarnsAboutAScenarioRegression(t *testing.T) {
	placar := writeBoard(t, scenarioBoard(map[string]evaluation.Outcome{"A": allHits}))
	current := scenarioBoard(map[string]evaluation.Outcome{"A": {Cause: true, Commit: true}, "B": allHits})

	code, _, stderr := runScore(t, fixedScorer(current, nil), "--recordings", "r", "--placar", placar, "--update")
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "warning: updating over a regression in detectors_only.A.line") {
		t.Errorf("stderr must warn about detectors_only.A.line, got %q", stderr)
	}
	data, err := os.ReadFile(placar)
	if err != nil {
		t.Fatal(err)
	}
	got, err := evaluation.ReadScoreboard(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if s := got["detectors_only"].Scenarios; s["A"] != current["detectors_only"].Scenarios["A"] || s["B"] != allHits {
		t.Fatalf("scenarios not written: %s", data)
	}
}

func writeRecording(t *testing.T, dir, scenario string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := manifestWith(fixture.Symptom{Service: "s", Method: "GET", Route: "/x", Bad: "ruim"})
	m.Scenario = scenario
	if err := recording.WriteManifest(filepath.Join(dir, "manifest.json"), m); err != nil {
		t.Fatal(err)
	}
}

func TestScoreRecordings_DuplicateScenarioNameIsAnErrorCitingTheNameAndBothPaths(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "a"), filepath.Join(root, "b")
	writeRecording(t, first, "n1-basic")
	writeRecording(t, second, "n1-basic")

	_, err := scoreRecordings(root)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"n1-basic", first, second} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must cite %q, got %v", want, err)
		}
	}
}
