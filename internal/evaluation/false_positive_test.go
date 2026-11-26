// Detector false positive: a candidate of the Result that matches a distractor of the answer key
// (ADR-0022).
package evaluation_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

// keyWithDistractor has a distractor in the same function as the answer, on another line.
var keyWithDistractor = func() evaluation.Key {
	k := key
	k.Distractors = []evaluation.Distractor{{Function: key.Function, File: key.File, Line: 30}}
	return k
}()

func level1(function, file string, line int) evaluation.CandidateLocation {
	return evaluation.CandidateLocation{Level: 1, Function: function, File: file, Line: line}
}

func level2(function string) evaluation.CandidateLocation {
	return evaluation.CandidateLocation{Level: 2, Function: function}
}

func withCandidates(cs ...evaluation.CandidateLocation) evaluation.Found {
	f := perfect
	f.Candidates = cs
	return f
}

func falsePositive(o evaluation.Outcome) string {
	switch {
	case o.FalsePositive == nil:
		return "nil"
	case *o.FalsePositive:
		return "true"
	}
	return "false"
}

func TestScoreOne_FalsePositiveWhenACandidateMatchesADistractorByLevel(t *testing.T) {
	otherFunction := "orders.(*Service).Other"
	keyInOtherFunction := keyWithDistractor
	keyInOtherFunction.Function = otherFunction
	tests := []struct {
		name  string
		key   evaluation.Key
		found evaluation.Found
		want  string
	}{
		{"level 1 on the distractor line", keyWithDistractor,
			withCandidates(level1(key.Function, key.File, 42), level1(key.Function, key.File, 30)), "true"},
		{"level 1 on the answer line", keyWithDistractor,
			withCandidates(level1(key.Function, key.File, 42)), "false"},
		{"level 1 on another line of the same function", keyWithDistractor,
			withCandidates(level1(key.Function, key.File, 42), level1(key.Function, key.File, 31)), "false"},
		{"level 2 with the distractor function, answer in another function", keyInOtherFunction,
			withCandidates(level2(key.Function)), "true"},
		{"level 2 with the distractor function, which is the answer function", keyWithDistractor,
			withCandidates(level2(key.Function)), "false"},
		{"candidate without location", keyWithDistractor,
			withCandidates(evaluation.CandidateLocation{}), "false"},
		{"scenario without distractor", key,
			withCandidates(level1(key.Function, key.File, 30)), "nil"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluation.ScoreOne(tc.key, tc.found, labels)
			if falsePositive(got) != tc.want {
				t.Fatalf("FalsePositive = %s, want %s", falsePositive(got), tc.want)
			}
		})
	}
}

func TestScoreOne_DistractorsDoNotChangeTheOtherMetrics(t *testing.T) {
	found := withCandidates(level1(key.Function, key.File, 42), level1(key.Function, key.File, 30))
	for name, f := range map[string]evaluation.Found{"perfect": found, "level 2 cause": func() evaluation.Found {
		g := found
		g.HasLocation, g.Level, g.File, g.Line = false, 2, "", 0
		return g
	}()} {
		t.Run(name, func(t *testing.T) {
			with := answerOnly(evaluation.ScoreOne(keyWithDistractor, f, labels))
			without := answerOnly(evaluation.ScoreOne(key, f, labels))
			with.FalsePositive = nil
			if with != without {
				t.Fatalf("with distractor %+v, without %+v", with, without)
			}
		})
	}
}

func withFalsePositive(o evaluation.Outcome, fp bool) evaluation.Outcome {
	o.FalsePositive = &fp
	return o
}

func TestScoreScenarios_DetectorFalsePositiveCountsOnlyScenariosWithADistractor(t *testing.T) {
	got := evaluation.ScoreScenarios(map[string]evaluation.Outcome{
		"plain": allHits, "fp": withFalsePositive(allHits, true), "clean": withFalsePositive(allHits, false),
	})
	if got.DetectorFalsePositive == nil || *got.DetectorFalsePositive != (evaluation.Metric{Hits: 1, Total: 2}) {
		t.Fatalf("DetectorFalsePositive = %v, want 1/2", got.DetectorFalsePositive)
	}
	if got.Cause.Total != 3 {
		t.Fatalf("the other metrics still count every scenario, Cause = %+v", got.Cause)
	}
}

func TestScoreboard_WithoutDistractorsHasNoFalsePositiveFields(t *testing.T) {
	var buf bytes.Buffer
	if err := evaluation.WriteScoreboard(&buf, scenarioBoard(map[string]evaluation.Outcome{"a": allHits})); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "false_positive") {
		t.Fatalf("a scoreboard without distractors must not change:\n%s", buf.String())
	}
}

func TestScoreboard_FalsePositiveRoundTrip(t *testing.T) {
	in := scenarioBoard(map[string]evaluation.Outcome{"fp": withFalsePositive(allHits, true), "plain": allHits})
	var buf bytes.Buffer
	if err := evaluation.WriteScoreboard(&buf, in); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"detector_false_positive": {`, `"false_positive": true`} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("scoreboard lacks %s:\n%s", want, buf.String())
		}
	}
	out, err := evaluation.ReadScoreboard(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip lost data: %+v vs %+v", in, out)
	}
}

func TestRegressions_FalsePositiveThatAppearsIsARegression(t *testing.T) {
	absent, clean, fp := allHits, withFalsePositive(allHits, false), withFalsePositive(allHits, true)
	tests := []struct {
		name      string
		base, cur evaluation.Outcome
		want      []string
	}{
		{"false to true", clean, fp, []string{"detectors_only.A.false_positive"}},
		{"true to false", fp, clean, nil},
		{"true stays true", fp, fp, nil},
		{"absent to true", absent, fp, nil},
		{"absent to false", absent, clean, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluation.Regressions(scenarioBoard(map[string]evaluation.Outcome{"A": tc.base}), scenarioBoard(map[string]evaluation.Outcome{"A": tc.cur}))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Regressions = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRegressions_AggregateFalsePositiveThatRisesWithTheSameTotalIsARegression(t *testing.T) {
	agg := func(hits, total int) evaluation.Scoreboard {
		s := sb(one, one, one, one)
		mode := s["detectors_only"]
		mode.DetectorFalsePositive = &evaluation.Metric{Hits: hits, Total: total}
		s["detectors_only"] = mode
		return s
	}
	tests := []struct {
		name      string
		base, cur evaluation.Scoreboard
		want      []string
	}{
		{"rises with the same total", agg(0, 1), agg(1, 1), []string{"detectors_only.detector_false_positive"}},
		{"drops", agg(1, 1), agg(0, 1), nil},
		{"rises with another total", agg(0, 1), agg(1, 2), nil},
		{"absent in the baseline", sb(one, one, one, one), agg(1, 1), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := evaluation.Regressions(tc.base, tc.cur); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Regressions = %v, want %v", got, tc.want)
			}
		})
	}
}
