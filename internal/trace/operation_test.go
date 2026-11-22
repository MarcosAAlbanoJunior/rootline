// Expected API (package trace, stdlib only):
//
//	type Span struct {
//	    TraceID, SpanID, ParentSpanID, Name string
//	    Kind    int            // OTLP span kind: 1 internal, 2 server, 3 client
//	    Scope   string         // instrumentation scope name
//	    Start, End time.Time
//	    Attrs   map[string]any // values: string, int64, bool, []string
//	    Service, Version string // resource attrs service.name / service.version
//	}
//	type Operation struct { Method, Route, RouteSource string }
//	const RouteSourceAttribute = "attribute"; RouteSourceSpanName = "span_name"
//	func OperationOf(s Span) (Operation, bool)   // entry operation of a server span
package trace_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func TestAC20_OperationFromHTTPRouteAttribute(t *testing.T) {
	s := trace.Span{
		Kind: 2,
		Name: "anything at all",
		Attrs: map[string]any{
			"http.route":          "/customers/{id}/orders",
			"http.request.method": "GET",
		},
	}
	op, ok := trace.OperationOf(s)
	if !ok {
		t.Fatal("expected an operation")
	}
	want := trace.Operation{Method: "GET", Route: "/customers/{id}/orders", RouteSource: "attribute"}
	if op != want {
		t.Fatalf("got %+v, want %+v", op, want)
	}
}

func TestAC20_OperationFromSpanName(t *testing.T) {
	for _, m := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE"} {
		t.Run(m, func(t *testing.T) {
			op, ok := trace.OperationOf(trace.Span{Kind: 2, Name: m + " /customers/{id}/orders"})
			if !ok {
				t.Fatal("expected an operation")
			}
			want := trace.Operation{Method: m, Route: "/customers/{id}/orders", RouteSource: "span_name"}
			if op != want {
				t.Fatalf("got %+v, want %+v", op, want)
			}
		})
	}
}

func TestAC20_SpanNamesThatProduceNoOperation(t *testing.T) {
	for _, name := range []string{"server", "get /x", "FOO /x", "GET x", "GET", "", "/x", "GET  /x extra", "GET/x"} {
		t.Run(name, func(t *testing.T) {
			if op, ok := trace.OperationOf(trace.Span{Kind: 2, Name: name}); ok {
				t.Fatalf("name %q must not produce an operation, got %+v", name, op)
			}
		})
	}
}
