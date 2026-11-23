package detector_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func spansWithStack(entries ...string) []trace.Span {
	return repeated("t", "root", 0, 5, 3, stack(entries...))
}

func TestLocate_AppliesTheLevelOneRule(t *testing.T) {
	outer := modPath + "/internal/orders.(*Handler).Get"
	tests := []struct {
		name       string
		env        fakeEnv
		spans      []trace.Span
		wantFunc   string
		wantFile   string
		wantLine   int
		wantInLoop bool
	}{
		{
			name:       "loop in the innermost frame",
			env:        fakeEnv{loops: map[string]bool{"demo-app/internal/orders/repository.go:30": true}},
			spans:      spansWithStack(defaultStack...),
			wantFunc:   repoFunc,
			wantFile:   "demo-app/internal/orders/repository.go",
			wantLine:   30,
			wantInLoop: true,
		},
		{
			name: "loop in an outer frame (helper in another layer)",
			env:  loopAtService,
			spans: spansWithStack(append(append([]string{}, defaultStack...),
				outer+" internal/orders/handler.go:7")...),
			wantFunc:   svcFunc,
			wantFile:   "demo-app/internal/orders/service.go",
			wantLine:   42,
			wantInLoop: true,
		},
		{
			name:       "no frame in a loop falls back to the innermost frame",
			env:        fakeEnv{},
			spans:      spansWithStack(defaultStack...),
			wantFunc:   repoFunc,
			wantFile:   "demo-app/internal/orders/repository.go",
			wantLine:   30,
			wantInLoop: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loc, reason := detector.Locate(tc.env, "abc1234", tc.spans)
			if loc == nil {
				t.Fatalf("location is nil (reason %q)", reason)
			}
			if loc.Level != 1 || loc.Function != tc.wantFunc || loc.File != tc.wantFile || loc.Line != tc.wantLine || loc.InLoop != tc.wantInLoop {
				t.Fatalf("got %+v", *loc)
			}
		})
	}

	t.Run("no app stack gives nil and no_app_stack", func(t *testing.T) {
		loc, reason := detector.Locate(loopAtService, "abc1234", spansWithStack())
		if loc != nil || reason != "no_app_stack" {
			t.Fatalf("got loc=%v reason=%q", loc, reason)
		}
	})

	t.Run("most frequent location among spans wins", func(t *testing.T) {
		spans := append(spansWithStack(defaultStack...),
			dbSpan("t", "odd", "root", 90, itemsQuery, 3, stack(repoFunc+" internal/orders/repository.go:99")))
		loc, _ := detector.Locate(fakeEnv{}, "abc1234", spans)
		if loc == nil || loc.Line != 30 {
			t.Fatalf("got %+v, want the line-30 location (5 spans vs 1)", loc)
		}
	})
}

// eightFramesWithoutLoop is an app_stack cut at 8 frames before reaching the loop.
func eightFramesWithoutLoop() []string {
	var frames []string
	for i := 1; i <= 8; i++ {
		frames = append(frames, fmt.Sprintf("%s/internal/deep.F%d internal/deep/f%d.go:%d", modPath, i, i, 10+i))
	}
	return frames
}

const loopNotFound8 = "loop_not_found: no app_stack frame inside a loop (8 frames)"

func count(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}

// ADR-0017: without a frame in a loop the location stays at level 1 on the innermost resolvable
// frame, with InLoop false, and says so in a warning instead of lowering the level.
func TestLocateWithWarnings_NoFrameInALoopWarnsLoopNotFound(t *testing.T) {
	spans := spansWithStack(eightFramesWithoutLoop()...) // 5 spans with the same stack
	loc, reason, warnings := detector.LocateWithWarnings(fakeEnv{}, "abc1234", spans)
	if loc == nil {
		t.Fatalf("location is nil (reason %q)", reason)
	}
	if loc.Level != 1 || loc.InLoop || loc.File != "demo-app/internal/deep/f1.go" || loc.Line != 11 {
		t.Fatalf("got %+v, want level 1, not in loop, at demo-app/internal/deep/f1.go:11", *loc)
	}
	if n := count(warnings, loopNotFound8); n != 1 {
		t.Fatalf("warnings = %q, want %q exactly once", warnings, loopNotFound8)
	}
}

func TestLocateWithWarnings_AFrameInALoopGivesNoLoopNotFoundWarning(t *testing.T) {
	_, _, warnings := detector.LocateWithWarnings(loopAtService, "abc1234", spansWithStack(defaultStack...))
	for _, w := range warnings {
		if strings.HasPrefix(w, "loop_not_found:") {
			t.Fatalf("warnings = %q, want no loop_not_found", warnings)
		}
	}
}

// The warning describes the chosen location: a minority span without a loop frame does not raise
// it when the majority location is inside a loop.
func TestLocateWithWarnings_NoLoopNotFoundWhenTheChosenLocationIsInALoop(t *testing.T) {
	spans := append(spansWithStack(defaultStack...), // 5 spans through the loop in the service
		dbSpan("t", "odd", "root", 90, itemsQuery, 3, stack(repoFunc+" internal/orders/repository.go:99")))
	loc, _, warnings := detector.LocateWithWarnings(loopAtService, "abc1234", spans)
	if loc == nil || !loc.InLoop {
		t.Fatalf("got %+v, want the in-loop location (5 spans vs 1)", loc)
	}
	for _, w := range warnings {
		if strings.HasPrefix(w, "loop_not_found:") {
			t.Fatalf("warnings = %q, want no loop_not_found for an in-loop location", warnings)
		}
	}
}
