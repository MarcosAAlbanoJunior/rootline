package detector_test

import (
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
)

func hasPrefix(ws []string, prefix string) bool {
	for _, w := range ws {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}

// ADR-0024: without a frame in a loop, both strategies point at the innermost frame, but only the
// N+1 one says the loop was not found.
func TestLocateWith_WithoutLoopFrameOnlyTheLoopStrategyWarns(t *testing.T) {
	spans := spansWithStack(defaultStack...)

	nLoc, _, nWarn := detector.LocateWith(detector.FirstFrameInLoop, fakeEnv{}, "abc1234", spans)
	iLoc, _, iWarn := detector.LocateWith(detector.InnermostFrame, fakeEnv{}, "abc1234", spans)

	for name, loc := range map[string]*detector.Location{"n+1": nLoc, "innermost": iLoc} {
		if loc == nil || loc.Level != 1 || loc.File != "demo-app/internal/orders/repository.go" || loc.Line != 30 {
			t.Fatalf("%s: got %+v, want level 1 at demo-app/internal/orders/repository.go:30", name, loc)
		}
	}
	if !hasPrefix(nWarn, "loop_not_found:") {
		t.Errorf("n+1 warnings = %q, want loop_not_found", nWarn)
	}
	if hasPrefix(iWarn, "loop_not_found:") {
		t.Errorf("innermost warnings = %q, want no loop_not_found", iWarn)
	}
	if !nLoc.LoopChecked || iLoc.LoopChecked {
		t.Errorf("LoopChecked: n+1 %v, innermost %v; want true, false", nLoc.LoopChecked, iLoc.LoopChecked)
	}
}

func TestLocateWith_LoopFrameAboveTheInnermostSplitsTheStrategies(t *testing.T) {
	spans := spansWithStack(defaultStack...)

	nLoc, _, _ := detector.LocateWith(detector.FirstFrameInLoop, loopAtService, "abc1234", spans)
	iLoc, _, _ := detector.LocateWith(detector.InnermostFrame, loopAtService, "abc1234", spans)

	if nLoc == nil || nLoc.Function != svcFunc || !nLoc.InLoop {
		t.Errorf("n+1: got %+v, want the service frame in a loop", nLoc)
	}
	if iLoc == nil || iLoc.Function != repoFunc || iLoc.Line != 30 || iLoc.InLoop {
		t.Errorf("innermost: got %+v, want the repository frame at line 30", iLoc)
	}
}

func TestLocateWith_InnermostWithoutStackGivesNoAppStack(t *testing.T) {
	loc, reason, _ := detector.LocateWith(detector.InnermostFrame, fakeEnv{}, "abc1234", spansWithStack())
	if loc != nil || reason != detector.ReasonNoAppStack {
		t.Fatalf("got loc=%v reason=%q, want nil and no_app_stack", loc, reason)
	}
}
