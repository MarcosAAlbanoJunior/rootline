// Expected API (package trace):
//
//	func NewTree(spans []Span) *Tree          // spans of ONE trace
//	func (t *Tree) Root(service string) (Span, bool)   // server span (kind 2) with no parent in the same service
//	func (t *Tree) Children(spanID string) []Span      // ordered by Start
//	func (t *Tree) Parent(spanID string) (Span, bool)
package trace_test

import (
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestAC21_TreeLinksChildrenAndFindsServerRoot(t *testing.T) {
	spans := []trace.Span{
		{TraceID: "t", SpanID: "g", Service: "gateway", Kind: 3, Name: "call", Start: t0},
		{TraceID: "t", SpanID: "s", ParentSpanID: "g", Service: "demo-app", Kind: 2, Name: "GET /x", Start: t0.Add(1 * time.Millisecond)},
		{TraceID: "t", SpanID: "q2", ParentSpanID: "s", Service: "demo-app", Kind: 3, Name: "q", Start: t0.Add(5 * time.Millisecond)},
		{TraceID: "t", SpanID: "q1", ParentSpanID: "s", Service: "demo-app", Kind: 3, Name: "q", Start: t0.Add(3 * time.Millisecond)},
		{TraceID: "t", SpanID: "s2", ParentSpanID: "s", Service: "demo-app", Kind: 2, Name: "GET /inner", Start: t0.Add(2 * time.Millisecond)},
	}
	tree := trace.NewTree(spans)

	root, ok := tree.Root("demo-app")
	if !ok || root.SpanID != "s" {
		t.Fatalf("Root(demo-app) = %+v,%v; want span s (remote parent in another service)", root, ok)
	}
	if _, ok := tree.Root("gateway"); ok {
		t.Fatal("gateway has no server span, Root must report false")
	}

	kids := tree.Children("s")
	var ids []string
	for _, k := range kids {
		ids = append(ids, k.SpanID)
	}
	want := []string{"s2", "q1", "q2"}
	if len(ids) != len(want) {
		t.Fatalf("children = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("children = %v, want %v (ordered by start)", ids, want)
		}
	}

	p, ok := tree.Parent("q1")
	if !ok || p.SpanID != "s" {
		t.Fatalf("Parent(q1) = %+v,%v; want s", p, ok)
	}
	if _, ok := tree.Parent("g"); ok {
		t.Fatal("g has no parent")
	}
}
