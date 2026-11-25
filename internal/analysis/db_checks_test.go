package analysis_test

import (
	"fmt"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// withoutStack drops the app stack from the database spans, as when the hook is not installed.
func withoutStack(fn func(string) []trace.Span) func(string) []trace.Span {
	return func(id string) []trace.Span {
		spans := fn(id)
		for i, s := range spans {
			if _, ok := s.Attrs["rootline.app_stack"]; ok {
				attrs := map[string]any{}
				for k, v := range s.Attrs {
					if k != "rootline.app_stack" {
						attrs[k] = v
					}
				}
				spans[i].Attrs = attrs
			}
		}
		return spans
	}
}

// withHookVersion marks the database spans with the hook version that annotated them.
func withHookVersion(v string, fn func(string) []trace.Span) func(string) []trace.Span {
	return func(id string) []trace.Span {
		spans := fn(id)
		for _, s := range spans {
			if _, ok := s.Attrs["rootline.app_stack"]; ok {
				s.Attrs["rootline.hook.version"] = v
			}
		}
		return spans
	}
}

// mongoTrace has client spans of MongoDB, with no query text, and no Postgres span.
func mongoTrace(id string) []trace.Span {
	srv := server(id, bad)
	spans := []trace.Span{srv}
	for i := 0; i < 3; i++ {
		spans = append(spans, trace.Span{
			TraceID: id, SpanID: fmt.Sprintf("%s-m%d", id, i), ParentSpanID: srv.SpanID, Name: "find", Kind: trace.KindClient,
			Service: "demo-app", Attrs: map[string]any{"db.system.name": "mongodb"},
		})
	}
	spans = append(spans, trace.Span{
		TraceID: id, SpanID: id + "-r", ParentSpanID: srv.SpanID, Name: "GET", Kind: trace.KindClient,
		Service: "demo-app", Attrs: map[string]any{"db.system": "redis"},
	})
	return spans
}

func TestCallerHookCheck_AllDatabaseSpansWithStack(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, badTrace), fakeGit{}, symptom), "caller_hook")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "44 of 44")
	contains(t, c, "reason", "100%")
	contains(t, c, "reason", "version not reported")
	if c["hint"] != "" {
		t.Errorf("hint = %q, want none with the hook everywhere", c["hint"])
	}
}

func TestCallerHookCheck_WithoutHookPointsToTheGuide(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, withoutStack(badTrace)), fakeGit{}, symptom), "caller_hook")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "0 of 44")
	contains(t, c, "hint", "not installed")
	contains(t, c, "hint", "docs/guia-instrumentacao.md")
}

func TestCallerHookCheck_MixedReportsTheFractionAndTheVersionsSeen(t *testing.T) {
	var src source
	for i := 0; i < 4; i++ {
		fn := withHookVersion("v0.1.0", badTrace)
		switch i {
		case 2:
			fn = withHookVersion("v0.2.0", badTrace)
		case 3:
			fn = withoutStack(badTrace)
		}
		src = append(src, fn(fmt.Sprintf("bad%02d", i))...)
	}
	c := check(t, run(t, src, fakeGit{}, symptom), "caller_hook")
	contains(t, c, "reason", "33 of 44")
	contains(t, c, "reason", "75%")
	contains(t, c, "reason", "hook versions v0.1.0, v0.2.0")
}

func TestCallerHookCheck_NotApplicableWithoutDatabaseSpans(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, mongoTrace), fakeGit{}, symptom), "caller_hook")
	wantStatus(t, c, analysis.CheckNotApplicable)
}

func TestDBSpansCheck_CountsTheRecognizedSpans(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, badTrace), fakeGit{}, symptom), "db_spans")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "44 database spans recognized")
	contains(t, c, "reason", "4 traces")
}

func TestDBSpansCheck_ReportsTheIgnoredSpansBySystem(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, mongoTrace), fakeGit{}, symptom), "db_spans")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "0 database spans recognized")
	contains(t, c, "reason", "mongodb: 12")
	contains(t, c, "reason", "redis: 4")
	contains(t, c, "hint", "Only PostgreSQL")
}

func TestCallerHookCheck_WarnsWhenTheHookVersionIsIncompatible(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, withHookVersion("0.2.0", badTrace)), fakeGit{}, symptom), "caller_hook")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "hint", "0.2.0 is incompatible")
	contains(t, c, "hint", "0.1.x")
}

func TestCallerHookCheck_APatchOfTheExpectedHookVersionIsCompatible(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, withHookVersion("0.1.3", badTrace)), fakeGit{}, symptom), "caller_hook")
	contains(t, c, "reason", "hook version 0.1.3")
	if c["hint"] != "" {
		t.Errorf("hint = %q, want none for a compatible hook", c["hint"])
	}
}

func TestCallerHookCheck_ANewerHookAsksToUpgradeRootline(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, withHookVersion("0.2.0", badTrace)), fakeGit{}, symptom), "caller_hook")
	contains(t, c, "hint", "upgrade rootline")
}

func TestCallerHookCheck_AnOlderHookAsksToUpdateTheHook(t *testing.T) {
	c := check(t, run(t, allSpans(4, 4, withHookVersion("0.0.9", badTrace)), fakeGit{}, symptom), "caller_hook")
	contains(t, c, "hint", "0.0.9 is incompatible")
	contains(t, c, "hint", "go get github.com/MarcosAAlbanoJunior/rootline/hook@v0.1")
}
