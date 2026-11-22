package evaluation_test

import (
	"reflect"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

func TestRegressionsModeMissingFromCurrentCountsAsRegression(t *testing.T) {
	base := evaluation.Scoreboard{
		"detectors_only": {Cause: one, Line: one, Commit: one, Strict: one},
		"pipeline":       {Cause: one, Line: one, Commit: one, Strict: one},
	}
	cur := evaluation.Scoreboard{"detectors_only": {Cause: one, Line: one, Commit: one, Strict: one}}

	got := evaluation.Regressions(base, cur)
	want := []string{"pipeline.cause", "pipeline.line", "pipeline.commit", "pipeline.strict"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRegressionsExtraModeInCurrentIsFine(t *testing.T) {
	base := evaluation.Scoreboard{"detectors_only": {Cause: one, Line: one, Commit: one, Strict: one}}
	cur := evaluation.Scoreboard{
		"detectors_only": {Cause: one, Line: one, Commit: one, Strict: one},
		"pipeline":       {},
	}
	if got := evaluation.Regressions(base, cur); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}
