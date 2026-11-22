package trace

import "sort"

// Tree links the spans of one trace to their parents and children.
type Tree struct {
	byID     map[string]Span
	children map[string][]Span
}

// NewTree builds the tree of the spans of ONE trace.
func NewTree(spans []Span) *Tree {
	t := &Tree{byID: make(map[string]Span, len(spans)), children: make(map[string][]Span)}
	for _, s := range spans {
		t.byID[s.SpanID] = s
	}
	for _, s := range spans {
		if s.ParentSpanID != "" {
			t.children[s.ParentSpanID] = append(t.children[s.ParentSpanID], s)
		}
	}
	for _, kids := range t.children {
		sort.SliceStable(kids, func(i, j int) bool { return kids[i].Start.Before(kids[j].Start) })
	}
	return t
}

// Root returns the server span of service that has no parent in the same service.
func (t *Tree) Root(service string) (Span, bool) {
	var best Span
	found := false
	for _, s := range t.byID {
		if s.Kind != KindServer || s.Service != service {
			continue
		}
		if p, ok := t.byID[s.ParentSpanID]; ok && p.Service == service {
			continue
		}
		if !found || s.Start.Before(best.Start) || (s.Start.Equal(best.Start) && s.SpanID < best.SpanID) {
			best, found = s, true
		}
	}
	return best, found
}

// Children returns the direct children of spanID, ordered by start time.
func (t *Tree) Children(spanID string) []Span {
	return t.children[spanID]
}

// Parent returns the parent of spanID, if it is in the trace.
func (t *Tree) Parent(spanID string) (Span, bool) {
	s, ok := t.byID[spanID]
	if !ok {
		return Span{}, false
	}
	p, ok := t.byID[s.ParentSpanID]
	return p, ok
}
