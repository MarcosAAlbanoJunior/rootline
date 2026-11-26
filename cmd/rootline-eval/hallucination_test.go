package main

import (
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

func TestScore_OutputShowsHallucinationAndAbstentionSideBySide(t *testing.T) {
	yes, no := true, false
	board := evaluation.Scoreboard{"detectors_only": evaluation.ScoreScenarios(map[string]evaluation.Outcome{
		"cpu": {Cause: true, Line: true, Commit: true, Strict: true, Hallucination: &no},
		"n1":  {Cause: false, LineLevel: 0, UnwarrantedAbstention: &yes},
	})}
	placar := writeBoard(t, board)

	_, stdout, _ := runScore(t, fixedScorer(board, nil), "--recordings", "r", "--placar", placar)

	for _, want := range []string{
		"  Alucinação: 0/1, 0% | Abstenção indevida: 1/1, 100%\n",
		"  cpu: Causa=ok Linha=ok Commit=ok Estrito=ok Nível=0 Alucinação=não\n",
		"  n1: Causa=erro Linha=erro Commit=erro Estrito=erro Nível=0 AbstençãoIndevida=sim\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}
