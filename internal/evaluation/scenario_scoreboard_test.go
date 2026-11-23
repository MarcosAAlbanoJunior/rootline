// The scoreboard keeps one outcome per scenario; the aggregate is derived from them, and
// regressions are found scenario by scenario so a swap between scenarios is not masked.
package evaluation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

var (
	allHits  = evaluation.Outcome{Cause: true, Line: true, Commit: true, Strict: true, LineLevel: 1}
	allMiss  = evaluation.Outcome{}
	causeHit = evaluation.Outcome{Cause: true, LineLevel: 2}
)

func scenarioBoard(outcomes map[string]evaluation.Outcome) evaluation.Scoreboard {
	return evaluation.Scoreboard{"detectors_only": evaluation.ScoreScenarios(outcomes)}
}

func TestScoreScenarios_AggregateIsTheSumOfTheScenarios(t *testing.T) {
	got := evaluation.ScoreScenarios(map[string]evaluation.Outcome{"a": allHits, "b": causeHit, "c": allMiss})

	var want evaluation.Score
	for _, o := range []evaluation.Outcome{allHits, causeHit, allMiss} {
		want.Add(o)
	}
	if got.Cause != want.Cause || got.Line != want.Line || got.Commit != want.Commit ||
		got.Strict != want.Strict || got.LineLevels != want.LineLevels {
		t.Fatalf("aggregate = %+v, want %+v", got, want)
	}
	if len(got.Scenarios) != 3 || got.Scenarios["b"] != causeHit {
		t.Fatalf("Scenarios = %+v", got.Scenarios)
	}
}

func TestScoreboard_ScenariosJSONShapeAndRoundTrip(t *testing.T) {
	in := scenarioBoard(map[string]evaluation.Outcome{"n1-basic": allHits, "n1-x": causeHit})
	var buf bytes.Buffer
	if err := evaluation.WriteScoreboard(&buf, in); err != nil {
		t.Fatal(err)
	}
	var raw map[string]struct {
		Cause     evaluation.Metric         `json:"cause"`
		Scenarios map[string]map[string]any `json:"scenarios"`
	}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	mode := raw["detectors_only"]
	if mode.Cause != (evaluation.Metric{Hits: 2, Total: 2}) {
		t.Fatalf("aggregate cause = %+v, want 2/2:\n%s", mode.Cause, buf.String())
	}
	want := map[string]any{"cause": true, "line": false, "commit": false, "strict": false, "line_level": 2.0}
	if !reflect.DeepEqual(mode.Scenarios["n1-x"], want) {
		t.Fatalf("scenarios.n1-x = %v, want %v:\n%s", mode.Scenarios["n1-x"], want, buf.String())
	}
	out, err := evaluation.ReadScoreboard(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip lost data: %+v vs %+v", in, out)
	}
}

func TestRegressions_ASwapBetweenScenariosIsARegressionEvenWithTheSameAggregate(t *testing.T) {
	base := scenarioBoard(map[string]evaluation.Outcome{"A": {Cause: true}, "B": {}})
	cur := scenarioBoard(map[string]evaluation.Outcome{"A": {}, "B": {Cause: true}})

	got := evaluation.Regressions(base, cur)
	if want := []string{"detectors_only.A.cause"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Regressions = %v, want %v", got, want)
	}
}

func TestRegressions_EachDroppedMetricOfAScenarioIsListed(t *testing.T) {
	base := scenarioBoard(map[string]evaluation.Outcome{"A": allHits, "B": allHits})
	cur := scenarioBoard(map[string]evaluation.Outcome{"A": allHits, "B": causeHit})

	got := evaluation.Regressions(base, cur)
	want := []string{"detectors_only.B.line", "detectors_only.B.commit", "detectors_only.B.strict"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Regressions = %v, want %v", got, want)
	}
}

func TestRegressions_ScenarioMissingFromTheCurrentRunIsARegression(t *testing.T) {
	base := scenarioBoard(map[string]evaluation.Outcome{"A": allMiss, "B": allHits})
	cur := scenarioBoard(map[string]evaluation.Outcome{"A": allMiss})

	got := evaluation.Regressions(base, cur)
	if want := []string{"detectors_only.B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Regressions = %v, want %v", got, want)
	}
}

func TestRegressions_NewScenarioIsNotARegressionEvenWithErrors(t *testing.T) {
	base := scenarioBoard(map[string]evaluation.Outcome{"A": allHits})
	cur := scenarioBoard(map[string]evaluation.Outcome{"A": allHits, "new": allMiss})

	if got := evaluation.Regressions(base, cur); len(got) != 0 {
		t.Fatalf("Regressions = %v, want none", got)
	}
}

func TestRegressions_OnlyALineLevelChangeIsNotARegression(t *testing.T) {
	lvl2 := allHits
	lvl2.LineLevel = 2
	base := scenarioBoard(map[string]evaluation.Outcome{"A": allHits})
	cur := scenarioBoard(map[string]evaluation.Outcome{"A": lvl2})

	if got := evaluation.Regressions(base, cur); len(got) != 0 {
		t.Fatalf("Regressions = %v, want none", got)
	}
}

// The scoreboard committed before scores were kept per scenario still reads, and its modes are
// compared with the aggregate rule (same total, fewer hits).
func TestRegressions_OldFormatBaselineUsesTheAggregateRule(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "placar-aggregate-only.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // read-only
	base, err := evaluation.ReadScoreboard(f)
	if err != nil {
		t.Fatal(err)
	}
	if base["detectors_only"].Scenarios != nil {
		t.Fatalf("old format must read without scenarios, got %+v", base["detectors_only"].Scenarios)
	}

	same := scenarioBoard(map[string]evaluation.Outcome{"n1-basic": allHits})
	if got := evaluation.Regressions(base, same); len(got) != 0 {
		t.Fatalf("Regressions = %v, want none", got)
	}
	worse := scenarioBoard(map[string]evaluation.Outcome{"n1-basic": causeHit})
	want := []string{"detectors_only.line", "detectors_only.commit", "detectors_only.strict"}
	if got := evaluation.Regressions(base, worse); !reflect.DeepEqual(got, want) {
		t.Fatalf("Regressions = %v, want %v", got, want)
	}
	if got := evaluation.Regressions(base, evaluation.Scoreboard{}); len(got) != 4 {
		t.Fatalf("a mode missing from the current run regresses every metric, got %v", got)
	}
}
