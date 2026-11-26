package analysis_test

import (
	"fmt"
	"testing"
)

func TestCommitCorrelation_KeepsTheHookHintWhenAnotherCandidateAlsoLacksAStack(t *testing.T) {
	src := append(traces("good", baseline, 40, func(int) (float64, float64) { return 5, 3 }),
		traces("bad", bad, 40, func(int) (float64, float64) { return 150, 148 })...)
	for i := 0; i < 40; i++ { // an N+1 recorded without the caller hook
		id := fmt.Sprintf("bad%02d", i)
		for j := 0; j < 6; j++ {
			q := query(id, fmt.Sprintf("%s-n%d", id, j), id+"-srv", 100+j*20, items, 3)
			delete(q.Attrs, "rootline.app_stack")
			src = append(src, q)
		}
	}
	m := run(t, src, fakeGit{}, symptom)

	if n := len(list(m, "candidates")); n != 2 {
		t.Fatalf("%d candidates in %v, want the N+1 and the slow_call", n, m["candidates"])
	}
	c := check(t, m, "commit_correlation")
	contains(t, c, "reason", "no candidate has an app stack")
	contains(t, c, "reason", "C2 slow_call")
	contains(t, c, "hint", "Install the caller hook")
}
