package analysis_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

type brokenResolveGit struct{ fakeGit }

func (brokenResolveGit) ResolvePath(_, _, _ string) (string, error) {
	return "", errTest("no go.mod for module")
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestResolveFailureIsReportedInWarningsAndDropsToNoLocation(t *testing.T) {
	res, err := analysis.Analyze(allSpans(4, 4, badTrace), fakeIndex{}, brokenResolveGit{}, nil, symptom)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["status"] != "cause_found" {
		t.Fatalf("the candidate must survive without a location, status = %v", m["status"])
	}
	cause := obj(t, m["cause"])
	if v, present := cause["location"]; !present || v != nil {
		t.Errorf("cause.location must be null, got %v", v)
	}
	var joined string
	for _, w := range list(m, "warnings") {
		joined += w.(string) + "\n"
	}
	if !strings.Contains(joined, "path_unresolved") {
		t.Errorf("warnings must name the failure, got %q", joined)
	}
	if len(list(m, "observations")) == 0 {
		t.Error("an observation about the missing location is expected")
	}
}

func TestRouteSourceReflectsTheRealSourceOfTheRootSpan(t *testing.T) {
	withAttr := func(id string) []trace.Span {
		spans := badTrace(id)
		spans[0].Attrs = map[string]any{"http.route": "/customers/{id}/orders", "http.request.method": "GET"}
		return spans
	}
	s := symptom
	s.Operation.RouteSource = "" // the CLI does not know the source
	m := run(t, allSpans(4, 4, withAttr), touchingService, s)
	op := obj(t, obj(t, m["symptom"])["operation"])
	if op["route_source"] != "attribute" {
		t.Errorf("route_source = %v, want attribute (the traces carry http.route)", op["route_source"])
	}

	m = run(t, allSpans(4, 4, badTrace), touchingService, s)
	op = obj(t, obj(t, m["symptom"])["operation"])
	if op["route_source"] != "span_name" {
		t.Errorf("route_source = %v, want span_name", op["route_source"])
	}
}

type noLoopIndex struct{ fakeIndex }

func (noLoopIndex) InLoop(_, _ string, _ int) (bool, error) { return false, nil }

// ADR-0017: without a frame in a loop, the Result keeps the level-1 location and carries
// loop_not_found once in its warnings; with the loop found, the warning is absent.
func TestLoopNotFoundReachesTheResultWarningsOnce(t *testing.T) {
	const want = "loop_not_found: no app_stack frame inside a loop (2 frames)"
	res, err := analysis.Analyze(allSpans(4, 4, badTrace), noLoopIndex{}, fakeGit{}, nil, symptom)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range res.Warnings {
		if w == want {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("warnings = %q, want %q exactly once", res.Warnings, want)
	}
	if l := res.Cause.Location; l == nil || l.Level != 1 || l.InLoop == nil || *l.InLoop {
		t.Fatalf("location = %+v, want level 1 with in_loop false", l)
	}

	found, err := analysis.Analyze(allSpans(4, 4, badTrace), fakeIndex{}, fakeGit{}, nil, symptom)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range found.Warnings {
		if strings.HasPrefix(w, "loop_not_found:") {
			t.Fatalf("warnings = %q, want no loop_not_found when the loop is found", found.Warnings)
		}
	}
}
