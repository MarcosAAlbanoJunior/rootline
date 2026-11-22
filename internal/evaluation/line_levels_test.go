// Slice 2, AC23 to AC27: the distribution of location levels in the Line metric.
//
// Expected API (package evaluation):
//
//	Found gains: Level int // 0 = absent, 1, 2
//	type LineLevels struct { Level1, Level2, None int } // json: level_1, level_2, none
//	Score gains: LineLevels LineLevels `json:"line_levels"`
package evaluation_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

var levelKey = evaluation.Key{CauseType: "n_plus_one", Function: "f", File: "a.go", Line: 7, CommitLabel: "regressao"}

var levelLabels = map[string]string{"sha": "regressao"}

func foundAt(level int, hasLocation bool, line int) evaluation.Found {
	return evaluation.Found{HasCause: true, CauseType: "n_plus_one", Function: "f", Level: level,
		HasLocation: hasLocation, File: "a.go", Line: line, CommitSHA: "sha"}
}

func TestAC24_EachAddIncrementsExactlyOneLevel(t *testing.T) {
	var s evaluation.Score
	s.Add(evaluation.ScoreOne(levelKey, foundAt(1, true, 7), levelLabels))
	s.Add(evaluation.ScoreOne(levelKey, foundAt(1, true, 9), levelLabels))
	s.Add(evaluation.ScoreOne(levelKey, foundAt(2, false, 0), levelLabels))
	s.Add(evaluation.ScoreOne(levelKey, foundAt(0, false, 0), levelLabels))
	s.Add(evaluation.ScoreOne(levelKey, evaluation.Found{}, levelLabels))

	want := evaluation.LineLevels{Level1: 2, Level2: 1, None: 2}
	if s.LineLevels != want {
		t.Fatalf("LineLevels = %+v, want %+v", s.LineLevels, want)
	}
	if sum := s.LineLevels.Level1 + s.LineLevels.Level2 + s.LineLevels.None; sum != s.Line.Total {
		t.Fatalf("levels sum %d != line.total %d", sum, s.Line.Total)
	}
}

func TestAC25_OnlyLevelOneWithCorrectFileAndLineIsAHit(t *testing.T) {
	var s evaluation.Score
	s.Add(evaluation.ScoreOne(levelKey, foundAt(1, true, 7), levelLabels))  // hit
	s.Add(evaluation.ScoreOne(levelKey, foundAt(1, true, 8), levelLabels))  // wrong line
	s.Add(evaluation.ScoreOne(levelKey, foundAt(2, false, 0), levelLabels)) // level 2
	s.Add(evaluation.ScoreOne(levelKey, foundAt(0, false, 0), levelLabels)) // absent
	if s.Line != (evaluation.Metric{Hits: 1, Total: 4}) {
		t.Fatalf("Line = %s, want 1/4", s.Line)
	}
}

func scoreWithLevels(l evaluation.LineLevels) evaluation.Score {
	return evaluation.Score{Cause: one, Line: one, Commit: one, Strict: one, LineLevels: l}
}

func TestAC26_RegressionsIgnoreLineLevels(t *testing.T) {
	base := evaluation.Scoreboard{"detectors_only": scoreWithLevels(evaluation.LineLevels{Level1: 1})}
	cur := evaluation.Scoreboard{"detectors_only": scoreWithLevels(evaluation.LineLevels{Level2: 1})}
	if reg := evaluation.Regressions(base, cur); len(reg) != 0 {
		t.Fatalf("a change only in the distribution must not regress, got %v", reg)
	}
}

func TestAC27_ReadsScoreboardWithoutLineLevels(t *testing.T) {
	old := `{"detectors_only":{"cause":{"hits":1,"total":1},"line":{"hits":1,"total":1},"commit":{"hits":1,"total":1},"strict":{"hits":1,"total":1}}}`
	got, err := evaluation.ReadScoreboard(strings.NewReader(old))
	if err != nil {
		t.Fatal(err)
	}
	if got["detectors_only"].LineLevels != (evaluation.LineLevels{}) {
		t.Fatalf("LineLevels = %+v, want zero", got["detectors_only"].LineLevels)
	}
	if got["detectors_only"].Line != one {
		t.Fatalf("the metrics must still be read: %+v", got)
	}
}

func TestAC24_LineLevelsJSONShape(t *testing.T) {
	in := evaluation.Scoreboard{"detectors_only": scoreWithLevels(evaluation.LineLevels{Level1: 1})}
	var buf bytes.Buffer
	if err := evaluation.WriteScoreboard(&buf, in); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"line_levels"`, `"level_1": 1`, `"level_2": 0`, `"none": 0`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("scoreboard JSON lacks %s:\n%s", want, buf.String())
		}
	}
}
