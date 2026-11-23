package trace_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func TestOperationOf_FromHTTPRouteAttribute(t *testing.T) {
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

func TestOperationOf_FromSpanName(t *testing.T) {
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

func TestOperationOf_SpanNamesThatProduceNoOperation(t *testing.T) {
	for _, name := range []string{"server", "get /x", "FOO /x", "GET x", "GET", "", "/x", "GET  /x extra", "GET/x"} {
		t.Run(name, func(t *testing.T) {
			if op, ok := trace.OperationOf(trace.Span{Kind: 2, Name: name}); ok {
				t.Fatalf("name %q must not produce an operation, got %+v", name, op)
			}
		})
	}
}
