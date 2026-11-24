package evaluation_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

var indexKey = evaluation.Key{
	CauseType: "missing_index", Function: "orders.(*Repo).ByStatus", Target: "orders",
	File: "demo-app/internal/orders/repository.go", Line: 30, CommitLabel: "regressao",
}

var indexFound = evaluation.Found{
	HasCause: true, CauseType: "missing_index", Function: "orders.(*Repo).ByStatus", Target: "orders",
	HasLocation: true, File: "demo-app/internal/orders/repository.go", Line: 30, CommitSHA: "sha-reg",
}

func TestScoreOne_MissingIndexWithTypeFunctionAndTableRightIsACauseHit(t *testing.T) {
	got := evaluation.ScoreOne(indexKey, indexFound, labels)
	if !got.Cause || !got.Strict {
		t.Fatalf("got %+v, want Cause and Strict hits", got)
	}
}

func TestScoreOne_MissingIndexOnTheWrongTableIsACauseError(t *testing.T) {
	found := indexFound
	found.Target = "order_items"

	got := evaluation.ScoreOne(indexKey, found, labels)

	if got.Cause || got.Strict || !got.Line || !got.Commit {
		t.Fatalf("got %+v, want a Cause and Strict error with Line and Commit hits", got)
	}
}

func TestScoreOne_MissingIndexWithoutTargetIsACauseError(t *testing.T) {
	found := indexFound
	found.Target = ""
	if got := evaluation.ScoreOne(indexKey, found, labels); got.Cause {
		t.Fatalf("got %+v, want a Cause error", got)
	}
}

func TestScoreOne_NPlusOneCauseDoesNotDependOnATarget(t *testing.T) {
	if got := evaluation.ScoreOne(key, perfect, labels); !got.Cause {
		t.Fatalf("got %+v, want a Cause hit without target", got)
	}
}
