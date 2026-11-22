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
	res, err := analysis.Analyze(allSpans(4, 4, badTrace), fakeIndex{}, brokenResolveGit{}, symptom)
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
