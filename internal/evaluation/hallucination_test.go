package evaluation_test

import (
	"reflect"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

var noCause = evaluation.Key{CauseType: evaluation.CauseInconclusive}

func no() *bool { b := false; return &b }

func TestScoreOne_AnInconclusiveResultHitsEveryMetricOfAScenarioWithoutCause(t *testing.T) {
	got := evaluation.ScoreOne(noCause, evaluation.Found{}, labels)

	want := evaluation.Outcome{Cause: true, Line: true, Commit: true, Strict: true, Hallucination: no()}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestScoreOne_ACauseInAScenarioWithoutCauseIsAHallucination(t *testing.T) {
	got := evaluation.ScoreOne(noCause, perfect, labels)

	if got.Cause || got.Line || got.Commit || got.Strict || got.Hallucination == nil || !*got.Hallucination {
		t.Fatalf("got %+v, want every metric missed and a hallucination", got)
	}
}

func TestScoreOne_ACommitInAScenarioWithoutCommitIsAHallucination(t *testing.T) {
	k := key
	k.CommitLabel = ""

	got := evaluation.ScoreOne(k, perfect, labels)

	if got.Hallucination == nil || !*got.Hallucination {
		t.Fatalf("hallucination = %v, want true for an invented commit", got.Hallucination)
	}
}

func TestScoreOne_InconclusiveInAScenarioWithACauseIsAnUnwarrantedAbstention(t *testing.T) {
	got := evaluation.ScoreOne(key, evaluation.Found{}, labels)
	if got.UnwarrantedAbstention == nil || !*got.UnwarrantedAbstention {
		t.Errorf("abstention = %v, want true", got.UnwarrantedAbstention)
	}
	if got := evaluation.ScoreOne(key, perfect, labels); got.UnwarrantedAbstention == nil || *got.UnwarrantedAbstention {
		t.Errorf("abstention = %v, want false when the Result has a cause", got.UnwarrantedAbstention)
	}
	if got.Hallucination != nil {
		t.Errorf("hallucination = %v, want nil: the scenario has a cause and a commit", *got.Hallucination)
	}
}

func TestScoreScenarios_HallucinationAndAbstentionAreCountedSideBySide(t *testing.T) {
	s := evaluation.ScoreScenarios(map[string]evaluation.Outcome{
		"cpu":   evaluation.ScoreOne(noCause, perfect, labels),
		"n1":    evaluation.ScoreOne(key, perfect, labels),
		"n1-no": evaluation.ScoreOne(key, evaluation.Found{}, labels),
	})

	if s.Hallucination != (evaluation.Metric{Hits: 1, Total: 1}) {
		t.Errorf("hallucination = %+v, want 1/1", s.Hallucination)
	}
	if s.UnwarrantedAbstention != (evaluation.Metric{Hits: 1, Total: 2}) {
		t.Errorf("abstention = %+v, want 1/2", s.UnwarrantedAbstention)
	}
}

func TestRegressions_AHallucinationOrAnAbstentionThatAppearsIsARegression(t *testing.T) {
	base := evaluation.Scoreboard{"m": evaluation.ScoreScenarios(map[string]evaluation.Outcome{
		"cpu": evaluation.ScoreOne(noCause, evaluation.Found{}, labels),
		"n1":  evaluation.ScoreOne(key, perfect, labels),
	})}
	cur := evaluation.Scoreboard{"m": evaluation.ScoreScenarios(map[string]evaluation.Outcome{
		"cpu": evaluation.ScoreOne(noCause, perfect, labels),
		"n1":  evaluation.ScoreOne(key, evaluation.Found{}, labels),
	})}

	got := evaluation.Regressions(base, cur)

	for _, want := range []string{"m.cpu.hallucination", "m.n1.unwarranted_abstention"} {
		found := false
		for _, r := range got {
			found = found || r == want
		}
		if !found {
			t.Errorf("regressions = %v, want %s", got, want)
		}
	}
}

func TestRegressions_OldFormatBaselineFlagsAHallucinationThatRose(t *testing.T) {
	base := evaluation.Scoreboard{"m": {Hallucination: evaluation.Metric{Hits: 0, Total: 1}}}
	cur := evaluation.Scoreboard{"m": {Hallucination: evaluation.Metric{Hits: 1, Total: 1}}}

	if got := evaluation.Regressions(base, cur); !reflect.DeepEqual(got, []string{"m.hallucination"}) {
		t.Errorf("regressions = %v, want [m.hallucination]", got)
	}
}

func TestScoreOne_ACandidateOnADistractorOfAScenarioWithoutCauseIsAFalsePositive(t *testing.T) {
	k := noCause
	k.Distractors = []evaluation.Distractor{{Function: "orders.F", File: "a.go", Line: 3}}

	got := evaluation.ScoreOne(k, evaluation.Found{
		Candidates: []evaluation.CandidateLocation{{Level: 1, Function: "orders.F", File: "a.go", Line: 3}},
	}, labels)

	if got.FalsePositive == nil || !*got.FalsePositive {
		t.Errorf("false positive = %v, want true", got.FalsePositive)
	}
}
