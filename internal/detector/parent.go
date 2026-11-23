package detector

import "github.com/MarcosAAlbanoJunior/rootline/internal/trace"

// EffectiveParent returns the span under which the query span s is grouped and its "+1" is looked
// for. It is the grandparent when the direct parent is a repeated wrapper: an INTERNAL span that is
// not a database span, with at least one homonym under the same grandparent, where it and all its
// homonyms only have database client children (query, prepare, or any span with db.system.name or
// db.system, such as a connection pool acquire). Then collapsedName is the wrapper's name and collapsedCount
// the number of homonyms. Otherwise it is s.ParentSpanID, "" and 0. It climbs one level only
// (ADR-0020).
func EffectiveParent(tree *trace.Tree, s trace.Span) (parentID, collapsedName string, collapsedCount int) {
	p, ok := tree.Parent(s.SpanID)
	if !ok || p.Kind != trace.KindInternal || PhaseOf(p) != PhaseNone {
		return s.ParentSpanID, "", 0
	}
	g, ok := tree.Parent(p.SpanID)
	if !ok {
		return s.ParentSpanID, "", 0
	}
	n := 0
	for _, sibling := range tree.Children(g.SpanID) {
		if sibling.Name != p.Name {
			continue
		}
		if !onlyDatabaseChildren(tree, sibling.SpanID) {
			return s.ParentSpanID, "", 0
		}
		n++
	}
	if n < 2 {
		return s.ParentSpanID, "", 0
	}
	return g.SpanID, p.Name, n
}

// onlyDatabaseChildren reports whether every child of spanID is a database client span; a span
// without children qualifies.
func onlyDatabaseChildren(tree *trace.Tree, spanID string) bool {
	for _, c := range tree.Children(spanID) {
		if !isDatabaseClient(c) {
			return false
		}
	}
	return true
}

// isDatabaseClient reports whether s belongs to the database client: a query or prepare span, or a
// span carrying the semconv database system attribute (db.system is its legacy name), like the
// pool.acquire and connect spans of otelpgx.
func isDatabaseClient(s trace.Span) bool {
	if PhaseOf(s) != PhaseNone {
		return true
	}
	_, current := s.Attrs[attrDBSystemName]
	_, legacy := s.Attrs[attrDBSystem]
	return current || legacy
}
