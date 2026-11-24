// Level-2 location (ADR-0002): when level 1 is unavailable, the route's handler is used as an
// approximate location, with the reason in the warnings; null only when level 2 is impossible too.
package analysis_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

const (
	handlerFn   = modPath + "/internal/orders.(*Handler).listOrders"
	handlerFile = "demo-app/internal/orders/handler.go"
)

// routeIndex is fakeIndex plus the optional RouteLocator capability.
type routeIndex struct {
	fakeIndex
	fn, file string
	found    bool
	err      error
	calls    *[]string
}

func (r routeIndex) FindRouteHandler(sha, method, route string) (string, string, bool, error) {
	*r.calls = append(*r.calls, sha+"|"+method+"|"+route)
	return r.fn, r.file, r.found, r.err
}

func analyze(t *testing.T, idx analysis.CodeIndex, g analysis.Git, src source) map[string]any {
	t.Helper()
	res, err := analysis.Analyze(src, idx, g, nil, symptom)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func joinedWarnings(m map[string]any) string {
	var b strings.Builder
	for _, w := range list(m, "warnings") {
		b.WriteString(w.(string) + "\n")
	}
	return b.String()
}

func noStackTrace(id string) []trace.Span {
	spans := badTrace(id)
	for _, s := range spans {
		delete(s.Attrs, "rootline.app_stack")
	}
	return spans
}

func TestLevelTwoReplacesAMissingLevelOneLocation(t *testing.T) {
	var calls []string
	idx := routeIndex{fn: handlerFn, file: handlerFile, found: true, calls: &calls}

	m := analyze(t, idx, brokenResolveGit{}, allSpans(4, 4, badTrace))

	if m["status"] != "cause_found" {
		t.Fatalf("status = %v", m["status"])
	}
	loc := obj(t, obj(t, m["cause"])["location"])
	if loc["level"] != float64(2) || loc["function"] != handlerFn || loc["file"] != handlerFile || loc["approximate"] != true {
		t.Fatalf("location = %v, want level 2, approximate, the handler", loc)
	}
	if _, hasLine := loc["line"]; hasLine {
		t.Errorf("a level-2 location has no line, got %v", loc)
	}
	if _, hasLoop := loc["in_loop"]; hasLoop {
		t.Errorf("in_loop was never checked at level 2 and must be omitted, got %v", loc)
	}
	if c1 := obj(t, list(m, "candidates")[0]); obj(t, c1["location"])["level"] != float64(2) {
		t.Errorf("the candidate must carry the level-2 location too: %v", c1["location"])
	}
	w := joinedWarnings(m)
	if !strings.Contains(w, "approximate") || !strings.Contains(w, "path_unresolved") {
		t.Errorf("warnings must say it is approximate and why level 1 failed, got:\n%s", w)
	}
	if len(calls) != 1 || calls[0] != bad+"|GET|/customers/{id}/orders" {
		t.Errorf("level 2 must look up the bad version's route once per candidate, got %v", calls)
	}
}

func TestLevelTwoWorksWithoutAnyAppStack(t *testing.T) {
	var calls []string
	idx := routeIndex{fn: handlerFn, file: handlerFile, found: true, calls: &calls}

	m := analyze(t, idx, touchingService, allSpans(4, 0, noStackTrace))

	loc := obj(t, obj(t, m["cause"])["location"])
	if loc["level"] != float64(2) {
		t.Fatalf("location = %v", loc)
	}
	if !strings.Contains(joinedWarnings(m), "no_app_stack") {
		t.Errorf("the warning must carry the reason, got:\n%s", joinedWarnings(m))
	}
}

func TestLevelTwoIsNotUsedWhenLevelOneWorks(t *testing.T) {
	var calls []string
	idx := routeIndex{fakeIndex: fakeIndex{}, fn: handlerFn, file: handlerFile, found: true, calls: &calls}

	m := analyze(t, idx, touchingService, allSpans(4, 4, badTrace))

	loc := obj(t, obj(t, m["cause"])["location"])
	if loc["level"] != float64(1) || loc["line"] != float64(42) {
		t.Fatalf("location = %v, want the level-1 location", loc)
	}
	if _, approx := loc["approximate"]; approx {
		t.Errorf("a level-1 location is not approximate: %v", loc)
	}
	if len(calls) != 0 {
		t.Errorf("level 2 must not be consulted when level 1 works, got %v", calls)
	}
}

func TestLocationIsNullOnlyWhenLevelTwoIsImpossibleToo(t *testing.T) {
	tests := []struct {
		name        string
		idx         func(calls *[]string) analysis.CodeIndex
		wantWarning string
	}{
		{"no registration found", func(c *[]string) analysis.CodeIndex { return routeIndex{calls: c} }, "found no registration"},
		{"the lookup fails", func(c *[]string) analysis.CodeIndex {
			return routeIndex{calls: c, err: errors.New("ambiguous handlers")}
		}, "level 2 failed: ambiguous handlers"},
		{"the index has no level-2 support", func(*[]string) analysis.CodeIndex { return fakeIndex{} }, "no level-2 support"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			m := analyze(t, tc.idx(&calls), brokenResolveGit{}, allSpans(4, 4, badTrace))

			if m["status"] != "cause_found" {
				t.Fatalf("the candidate must survive, status = %v", m["status"])
			}
			if v, present := obj(t, m["cause"])["location"]; !present || v != nil {
				t.Errorf("cause.location must be null, got %v", v)
			}
			if !strings.Contains(joinedWarnings(m), tc.wantWarning) {
				t.Errorf("warnings must contain %q, got:\n%s", tc.wantWarning, joinedWarnings(m))
			}
		})
	}
}

func TestLevelOneAlwaysReportsInLoopEvenWhenFalse(t *testing.T) {
	var calls []string
	idx := routeIndex{fakeIndex: fakeIndex{}, calls: &calls}
	// fakeIndex only reports a loop at service.go:42; this stack has no frame there.
	noLoop := func(id string) []trace.Span {
		spans := badTrace(id)
		for _, s := range spans {
			if _, ok := s.Attrs["rootline.app_stack"]; ok {
				s.Attrs["rootline.app_stack"] = []string{repoFn + " internal/orders/repository.go:30"}
			}
		}
		return spans
	}
	m := analyze(t, idx, touchingService, allSpans(4, 0, noLoop))
	loc := obj(t, obj(t, m["cause"])["location"])
	v, present := loc["in_loop"]
	if loc["level"] != float64(1) || !present || v != false {
		t.Fatalf("location = %v, want level 1 with in_loop=false present", loc)
	}
}
