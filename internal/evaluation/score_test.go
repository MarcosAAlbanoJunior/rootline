package evaluation_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

var key = evaluation.Key{
	CauseType: "n_plus_one", Function: "orders.(*Service).List",
	File: "demo-app/internal/orders/service.go", Line: 42, CommitLabel: "regressao",
}

var labels = map[string]string{"sha-ref": "referencia", "sha-reg": "regressao"}

var perfect = evaluation.Found{
	HasCause: true, CauseType: "n_plus_one", Function: "orders.(*Service).List",
	HasLocation: true, File: "demo-app/internal/orders/service.go", Line: 42, CommitSHA: "sha-reg",
}

func TestScoreOne_AllHitsIsStrict(t *testing.T) {
	got := evaluation.ScoreOne(key, perfect, labels)
	if want := (evaluation.Outcome{Cause: true, Line: true, Commit: true, Strict: true}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestScoreOne_Misses(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(f *evaluation.Found)
		want   evaluation.Outcome
	}{
		{"wrong cause type", func(f *evaluation.Found) { f.CauseType = "missing_index" }, evaluation.Outcome{Cause: false, Line: true, Commit: true}},
		{"wrong function", func(f *evaluation.Found) { f.Function = "orders.Other" }, evaluation.Outcome{Cause: false, Line: true, Commit: true}},
		{"wrong line", func(f *evaluation.Found) { f.Line = 43 }, evaluation.Outcome{Cause: true, Line: false, Commit: true}},
		{"wrong file", func(f *evaluation.Found) { f.File = "demo-app/internal/orders/handler.go" }, evaluation.Outcome{Cause: true, Line: false, Commit: true}},
		{"no level-1 location counts as a line error", func(f *evaluation.Found) { f.HasLocation = false; f.File = ""; f.Line = 0 }, evaluation.Outcome{Cause: true, Line: false, Commit: true}},
		{"wrong commit label", func(f *evaluation.Found) { f.CommitSHA = "sha-ref" }, evaluation.Outcome{Cause: true, Line: true, Commit: false}},
		{"unknown sha", func(f *evaluation.Found) { f.CommitSHA = "sha-unknown" }, evaluation.Outcome{Cause: true, Line: true, Commit: false}},
		{"no commit although one is expected", func(f *evaluation.Found) { f.CommitSHA = "" }, evaluation.Outcome{Cause: true, Line: true, Commit: false}},
		{"no cause at all", func(f *evaluation.Found) { *f = evaluation.Found{} }, evaluation.Outcome{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := perfect
			tc.mutate(&f)
			got := evaluation.ScoreOne(key, f, labels)
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestScoreOne_CommitBothNullIsAHit(t *testing.T) {
	k := key
	k.CommitLabel = ""
	f := perfect
	f.CommitSHA = ""
	if got := evaluation.ScoreOne(k, f, labels); !got.Commit || !got.Strict {
		t.Fatalf("both nil must be a commit hit, got %+v", got)
	}
	f.CommitSHA = "sha-reg"
	if got := evaluation.ScoreOne(k, f, labels); got.Commit {
		t.Fatalf("a commit where none is expected must miss, got %+v", got)
	}
}

func TestScore_AccumulatesAndFormats(t *testing.T) {
	var s evaluation.Score
	s.Add(evaluation.Outcome{Cause: true, Line: true, Commit: true, Strict: true})
	s.Add(evaluation.Outcome{Cause: true, Line: false, Commit: true, Strict: false})
	s.Add(evaluation.Outcome{})

	if s.Cause != (evaluation.Metric{Hits: 2, Total: 3}) || s.Line != (evaluation.Metric{Hits: 1, Total: 3}) ||
		s.Commit != (evaluation.Metric{Hits: 2, Total: 3}) || s.Strict != (evaluation.Metric{Hits: 1, Total: 3}) {
		t.Fatalf("got %+v", s)
	}
	if got := s.Cause.String(); got != "2/3, 66%" {
		t.Errorf("Cause.String() = %q", got)
	}
	if got := s.Strict.String(); got != "1/3, 33%" {
		t.Errorf("Strict.String() = %q", got)
	}
	if got := (evaluation.Metric{Hits: 1, Total: 1}).String(); got != "1/1, 100%" {
		t.Errorf("got %q", got)
	}
	if got := (evaluation.Metric{}).String(); got != "0/0, 0%" {
		t.Errorf("empty metric: got %q", got)
	}
}

// A Result without a level-1 location is a Line error that stays in the denominator: it is never
// dropped from the total, and it does not stop the other metrics from being scored.
func TestLineMissCountsInTheDenominatorWhenLocationIsAbsent(t *testing.T) {
	noLocation := perfect
	noLocation.HasLocation, noLocation.File, noLocation.Line = false, "", 0

	o := evaluation.ScoreOne(key, noLocation, labels)
	if o.Line || o.Strict || !o.Cause || !o.Commit {
		t.Fatalf("got %+v, want Line and Strict missed, Cause and Commit hit", o)
	}

	var s evaluation.Score
	s.Add(evaluation.ScoreOne(key, perfect, labels))
	s.Add(o)
	s.Add(evaluation.ScoreOne(key, evaluation.Found{}, labels)) // no cause at all
	if s.Line != (evaluation.Metric{Hits: 1, Total: 3}) {
		t.Fatalf("Line = %s, want 1/3: absent locations must stay in the denominator", s.Line)
	}
	if s.Strict != (evaluation.Metric{Hits: 1, Total: 3}) || s.Cause != (evaluation.Metric{Hits: 2, Total: 3}) {
		t.Fatalf("got %+v", s)
	}
}
