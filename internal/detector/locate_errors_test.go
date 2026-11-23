// Errors from the Env ports must not be swallowed: the location falls back with a recorded reason.
package detector_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
)

type failingEnv struct {
	failResolve map[string]bool // module-relative file -> ResolvePath error
	failInLoop  bool
}

func (f failingEnv) ResolvePath(_, _, rel string) (string, error) {
	if f.failResolve[rel] {
		return "", errors.New("module not found")
	}
	return "demo-app/" + rel, nil
}

func (f failingEnv) InLoop(_, file string, line int) (bool, error) {
	if f.failInLoop {
		return false, errors.New("blob unreadable")
	}
	return file == "demo-app/internal/orders/service.go" && line == 42, nil
}

func hasWarning(ws []string, prefix string) bool {
	for _, w := range ws {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}

func TestLocateResolveFailureOnEveryFrameGivesPathUnresolved(t *testing.T) {
	env := failingEnv{failResolve: map[string]bool{
		"internal/orders/repository.go": true, "internal/orders/service.go": true,
	}}
	loc, reason, warns := detector.LocateWithWarnings(env, "abc1234", spansWithStack(defaultStack...))
	if loc != nil || reason != detector.ReasonPathUnresolved {
		t.Fatalf("got loc=%v reason=%q, want nil and path_unresolved", loc, reason)
	}
	if !hasWarning(warns, "path_unresolved:") {
		t.Fatalf("warnings must record the failure, got %v", warns)
	}
	if loc2, reason2 := detector.Locate(env, "abc1234", spansWithStack(defaultStack...)); loc2 != nil || reason2 != detector.ReasonPathUnresolved {
		t.Fatalf("Locate must agree: %v %q", loc2, reason2)
	}
}

func TestLocateResolveFailureOnOuterFrameFallsToTheInnerOne(t *testing.T) {
	env := failingEnv{failResolve: map[string]bool{"internal/orders/service.go": true}}
	loc, _, warns := detector.LocateWithWarnings(env, "abc1234", spansWithStack(defaultStack...))
	if loc == nil || loc.Line != 30 || loc.InLoop || loc.File != "demo-app/internal/orders/repository.go" {
		t.Fatalf("got %+v, want the inner frame with in_loop=false", loc)
	}
	// The inner frame is not in a loop either, so loop_not_found comes along (ADR-0017).
	want := []string{
		"path_unresolved: internal/orders/service.go (module github.com/x/demo-app) at abc1234: module not found",
		"loop_not_found: no app_stack frame inside a loop (2 frames)",
	}
	if !reflect.DeepEqual(warns, want) {
		t.Fatalf("want each warning once (5 spans, same frame), got %v", warns)
	}
}

func TestLocateInLoopFailureCountsAsNotInLoopAndIsReported(t *testing.T) {
	loc, _, warns := detector.LocateWithWarnings(failingEnv{failInLoop: true}, "abc1234", spansWithStack(defaultStack...))
	if loc == nil || loc.InLoop || loc.Line != 30 {
		t.Fatalf("got %+v, want innermost frame, in_loop=false", loc)
	}
	if !hasWarning(warns, "in_loop_unavailable:") {
		t.Fatalf("warnings = %v", warns)
	}
}

func TestDetectCarriesLocationWarningsToTheCandidate(t *testing.T) {
	env := failingEnv{failResolve: map[string]bool{
		"internal/orders/repository.go": true, "internal/orders/service.go": true,
	}}
	cands, _, err := detector.Detect(env, "abc1234", traces(4, 4, healthy))
	if err != nil {
		t.Fatal(err)
	}
	var c *detector.Candidate
	for i := range cands {
		if cands[i].Query == itemsQuery {
			c = &cands[i]
		}
	}
	if c == nil {
		t.Fatal("candidate still expected when the location is unavailable")
	}
	if c.Location != nil || c.LocationReason != detector.ReasonPathUnresolved || !hasWarning(c.Warnings, "path_unresolved:") {
		t.Fatalf("got location=%v reason=%q warnings=%v", c.Location, c.LocationReason, c.Warnings)
	}
}
