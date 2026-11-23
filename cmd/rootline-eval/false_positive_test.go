// The detector false positive in the score command: every candidate of the Result is scored
// against the distractors of the manifest (ADR-0022).
package main

import (
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

func TestScoreManifest_SecondCandidateOnTheDistractorLineIsAFalsePositive(t *testing.T) {
	m := manifestWith(fixture.Symptom{Service: "s", Method: "GET", Route: "/r", Bad: "ruim", Baseline: "boa"})
	m.AnswerKey = fixture.AnswerKey{Cause: "n_plus_one", Function: "f", File: "a.go", Line: 7, CommitLabel: "ruim",
		Distractors: []fixture.Distractor{{Function: "f", File: "a.go", Line: 3}}}
	cause := &analysis.Location{Level: 1, Function: "f", File: "a.go", Line: 7}
	analyze := func(rec string, s analysis.Symptom) (analysis.Result, error) {
		return analysis.Result{
			Cause: &analysis.Cause{Type: "n_plus_one", Location: cause, Commit: &analysis.CommitRef{SHA: s.BadVersion}},
			Candidates: []analysis.Candidate{
				{ID: "C1", Type: "n_plus_one", Location: cause},
				{ID: "C2", Type: "n_plus_one", Location: &analysis.Location{Level: 1, Function: "f", File: "a.go", Line: 3}},
			},
		}, nil
	}

	o, err := scoreManifest("rec", m, analyze)
	if err != nil {
		t.Fatal(err)
	}
	if o.FalsePositive == nil || !*o.FalsePositive {
		t.Fatalf("FalsePositive = %v, want true", o.FalsePositive)
	}
	if !o.Strict {
		t.Fatalf("the cause still scores: %+v", o)
	}
}

func TestFoundFromResult_HasTheLocationOfEveryCandidate(t *testing.T) {
	res := analysis.Result{Candidates: []analysis.Candidate{
		{ID: "C1", Location: &analysis.Location{Level: 1, Function: "f", File: "a.go", Line: 7}},
		{ID: "C2", Location: &analysis.Location{Level: 2, Function: "h", File: "h.go", Approximate: true}},
		{ID: "C3"},
	}}
	got := foundFromResult(res).Candidates
	want := []evaluation.CandidateLocation{
		{Level: 1, Function: "f", File: "a.go", Line: 7},
		{Level: 2, Function: "h", File: "h.go"},
		{},
	}
	if len(got) != len(want) {
		t.Fatalf("Candidates = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Candidates = %+v, want %+v", got, want)
		}
	}
}

func TestScore_OutputShowsTheDetectorFalsePositiveOnlyWithDistractors(t *testing.T) {
	yes, no := true, false
	withFP := evaluation.ScoreScenarios(map[string]evaluation.Outcome{
		"a-plain": {Cause: true, LineLevel: 1},
		"b-fp":    {Cause: true, LineLevel: 1, FalsePositive: &yes},
		"c-clean": {Cause: true, LineLevel: 1, FalsePositive: &no},
	})
	placar := writeBoard(t, evaluation.Scoreboard{"detectors_only": evaluation.ScoreScenarios(map[string]evaluation.Outcome{
		"a-plain": {Cause: true, LineLevel: 1}, "b-fp": {Cause: true, LineLevel: 1, FalsePositive: &yes},
	})})

	_, stdout, _ := runScore(t, fixedScorer(evaluation.Scoreboard{"detectors_only": withFP}, nil), "--recordings", "r", "--placar", placar)

	for _, want := range []string{
		"  Níveis da Linha: 1=3 2=0 ausente=0\n  Falso positivo de detector: 1/2, 50%\n",
		"  a-plain: Causa=ok Linha=erro Commit=erro Estrito=erro Nível=1\n",
		"  b-fp: Causa=ok Linha=erro Commit=erro Estrito=erro Nível=1 FalsoPositivo=sim\n",
		"  c-clean: Causa=ok Linha=erro Commit=erro Estrito=erro Nível=1 FalsoPositivo=não (novo)\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}

	_, plain, _ := runScore(t, fixedScorer(evaluation.Scoreboard{"detectors_only": evaluation.ScoreScenarios(map[string]evaluation.Outcome{
		"a-plain": {Cause: true, LineLevel: 1},
	})}, nil), "--recordings", "r", "--placar", placar)
	if strings.Contains(plain, "Falso positivo") || strings.Contains(plain, "FalsoPositivo") {
		t.Errorf("without distractors the output must not change:\n%s", plain)
	}
}
