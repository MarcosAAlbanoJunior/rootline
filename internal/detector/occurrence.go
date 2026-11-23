package detector

import (
	"sort"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// Phase says what role a span plays in the database protocol.
type Phase int

const (
	PhaseNone Phase = iota // not a database span
	PhaseQuery
	PhasePrepare
)

// PhaseOf tells whether a span is a query span, a prepare span or not a database span.
func PhaseOf(s trace.Span) Phase {
	p, _ := classify(s)
	return p
}

// classify tells whether a span is a query span, a prepare span or neither, and whether the
// answer came from the heuristic (no rootline.db.phase attribute).
func classify(s trace.Span) (kind Phase, inferred bool) {
	if _, ok := s.String(attrQueryText); !ok {
		return PhaseNone, false
	}
	if p, ok := s.String(attrPhase); ok {
		if p == "prepare" {
			return PhasePrepare, false
		}
		return PhaseQuery, false
	}
	_, hasName := s.Attrs[attrPrepareName]
	_, hasRows := s.Attrs[attrRowsAffected]
	if hasName && !hasRows {
		return PhasePrepare, true
	}
	return PhaseQuery, true
}

type groupKey struct{ parent, query string }

type effectiveParent struct {
	id, collapsedName string
	collapsedCount    int
}

// FindOccurrences finds the N+1 occurrences among the spans of ONE trace. Queries are grouped, and
// their "+1" looked for, under their effective parent (ADR-0020).
func FindOccurrences(spans []trace.Span) []Occurrence {
	tree := trace.NewTree(spans)
	byDirectParent := map[string]effectiveParent{} // the rule depends only on the direct parent
	parentOf := map[string]string{}                // query span id -> effective parent id
	collapsed := map[groupKey]effectiveParent{}
	groups := map[groupKey][]trace.Span{}
	var order []groupKey
	var queries []trace.Span
	for _, s := range spans {
		kind, _ := classify(s)
		if kind != PhaseQuery {
			continue
		}
		queries = append(queries, s)
		e, ok := byDirectParent[s.ParentSpanID]
		if !ok {
			e.id, e.collapsedName, e.collapsedCount = EffectiveParent(tree, s)
			byDirectParent[s.ParentSpanID] = e
		}
		parentOf[s.SpanID] = e.id
		text, _ := s.String(attrQueryText)
		k := groupKey{parent: e.id, query: Normalize(text)}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		if _, has := collapsed[k]; !has && e.collapsedCount > 0 {
			collapsed[k] = e
		}
		groups[k] = append(groups[k], s)
	}

	var out []Occurrence
	for _, k := range order {
		g := groups[k]
		if len(g) < MinRepetitions {
			continue
		}
		sort.SliceStable(g, func(i, j int) bool { return g[i].Start.Before(g[j].Start) })
		rows := rowsOf(g)
		median := medianInt(rows)
		if median > MaxMedianRows {
			continue
		}
		occ := Occurrence{
			TraceID: g[0].TraceID, ParentSpanID: k.parent, Query: k.query,
			Spans: g, RowsMedian: median,
			PlusOne:              plusOne(queries, g, k.parent, parentOf),
			CollapsedParentName:  collapsed[k].collapsedName,
			CollapsedParentCount: collapsed[k].collapsedCount,
		}
		for _, s := range g {
			if _, inferred := classify(s); inferred {
				occ.PhaseInferred = true
				break
			}
		}
		out = append(out, occ)
	}
	return out
}

func rowsOf(g []trace.Span) []int {
	var rows []int
	for _, s := range g {
		if r, ok := s.Int(attrRowsAffected); ok {
			rows = append(rows, int(r))
		}
	}
	return rows
}

func medianInt(v []int) int {
	if len(v) == 0 {
		return 0
	}
	c := append([]int(nil), v...)
	sort.Ints(c)
	return c[len(c)/2]
}

// plusOne reports whether another query span under the same effective parent, started before the
// group, returned a number of rows that matches the group size.
func plusOne(queries, group []trace.Span, parent string, parentOf map[string]string) bool {
	first := group[0].Start
	inGroup := make(map[string]bool, len(group))
	for _, s := range group {
		inGroup[s.SpanID] = true
	}
	size := len(group)
	for _, s := range queries {
		if inGroup[s.SpanID] || parentOf[s.SpanID] != parent || !s.Start.Before(first) {
			continue
		}
		r, ok := s.Int(attrRowsAffected)
		if !ok {
			continue
		}
		tol := max(1, int(r)/10)
		if abs(size-int(r)) <= tol {
			return true
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
