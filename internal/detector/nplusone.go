package detector

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// Initial N+1 criteria (adjustable; see the slice-1 spec).
const (
	MinRepetitions    = 5
	MaxMedianRows     = 10
	MinAffectedTraces = 3
	MinAffectedRatio  = 0.5
)

// Span attribute names read by the detector.
const (
	attrQueryText    = "db.query.text"
	attrRowsAffected = "pgx.rows_affected"
	attrPrepareName  = "pgx.prepare_stmt.name"
	attrPhase        = "rootline.db.phase"
	attrAppStack     = "rootline.app_stack"
	attrModulePath   = "rootline.module.path"
)

// Occurrence is one N+1 occurrence inside a single trace.
type Occurrence struct {
	TraceID, ParentSpanID, Query string // Query is normalized
	Spans                        []trace.Span
	PlusOne                      bool
	PhaseInferred                bool
	RowsMedian                   int
}

// Env is the port consumed by location. It is implemented by adapters (and fakes in tests).
type Env interface {
	// ResolvePath maps a module-relative file to a repo-relative path at sha.
	ResolvePath(sha, modulePath, relFile string) (string, error)
	// InLoop reports whether the line is strictly inside a for/range body at sha.
	InLoop(sha, file string, line int) (bool, error)
}

// Location is a level-1 code location.
type Location struct {
	Level    int
	Function string
	File     string
	Line     int
	InLoop   bool
}

// StackFrame is one entry of rootline.app_stack. File is module-relative.
type StackFrame struct {
	Function, File string
	Line           int
}

// Candidate is a root-cause candidate of type N+1.
type Candidate struct {
	Query               string
	Location            *Location
	LocationReason      string
	ModulePath          string
	Stack               []StackFrame
	Confidence          string  // "high" | "medium"
	Score               float64 // affected/total + 0.5 if plus-one (the commit bonus is added later)
	TracesAffected      int
	TracesTotal         int
	GroupSizeMedian     int
	RowsMedian          int
	PlusOneMatchedRatio float64
	PhaseInferred       bool
	SampleTraceID       string
	SampleParentSpanID  string
	Warnings            []string // location problems (unresolved path, index failure), deduplicated
}

type phaseKind int

const (
	phaseNone phaseKind = iota // not a database span
	phaseQuery
	phasePrepare
)

// classify tells whether a span is a query span, a prepare span or neither, and whether the
// answer came from the heuristic (no rootline.db.phase attribute).
func classify(s trace.Span) (kind phaseKind, inferred bool) {
	if _, ok := s.String(attrQueryText); !ok {
		return phaseNone, false
	}
	if p, ok := s.String(attrPhase); ok {
		if p == "prepare" {
			return phasePrepare, false
		}
		return phaseQuery, false
	}
	_, hasName := s.Attrs[attrPrepareName]
	_, hasRows := s.Attrs[attrRowsAffected]
	if hasName && !hasRows {
		return phasePrepare, true
	}
	return phaseQuery, true
}

type groupKey struct{ parent, query string }

// FindOccurrences finds the N+1 occurrences among the spans of ONE trace.
func FindOccurrences(spans []trace.Span) []Occurrence {
	groups := map[groupKey][]trace.Span{}
	var order []groupKey
	var queries []trace.Span
	for _, s := range spans {
		kind, _ := classify(s)
		if kind != phaseQuery {
			continue
		}
		queries = append(queries, s)
		text, _ := s.String(attrQueryText)
		k := groupKey{parent: s.ParentSpanID, query: Normalize(text)}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
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
			PlusOne: plusOne(queries, g),
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

// plusOne reports whether another query span under the same parent, started before the group,
// returned a number of rows that matches the group size.
func plusOne(queries, group []trace.Span) bool {
	first := group[0].Start
	inGroup := make(map[string]bool, len(group))
	for _, s := range group {
		inGroup[s.SpanID] = true
	}
	size := len(group)
	for _, s := range queries {
		if inGroup[s.SpanID] || s.ParentSpanID != group[0].ParentSpanID || !s.Start.Before(first) {
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

// Reasons a location can be missing.
const (
	ReasonNoAppStack     = "no_app_stack"
	ReasonPathUnresolved = "path_unresolved"
)

// Locate applies the level-1 location rule over rootline.app_stack. When there is no stack it
// returns a nil location and reason "no_app_stack"; when the stack exists but no frame could be
// resolved to a repository path, nil and "path_unresolved".
func Locate(env Env, sha string, spans []trace.Span) (loc *Location, reason string) {
	loc, reason, _ = LocateWithWarnings(env, sha, spans)
	return loc, reason
}

// LocateWithWarnings is Locate plus the problems met on the way: a frame whose path could not be
// resolved is skipped, and a frame whose loop check failed counts as not in a loop. Each problem
// is reported once.
func LocateWithWarnings(env Env, sha string, spans []trace.Span) (loc *Location, reason string, warnings []string) {
	w := &warnSet{}
	counts := map[Location]int{}
	var order []Location
	hadStack := false
	for _, s := range spans {
		if len(parseStack(s)) > 0 {
			hadStack = true
		}
		l, ok := locateSpan(env, sha, s, w)
		if !ok {
			continue
		}
		if _, seen := counts[l]; !seen {
			order = append(order, l)
		}
		counts[l]++
	}
	if len(order) == 0 {
		if hadStack {
			return nil, ReasonPathUnresolved, w.list
		}
		return nil, ReasonNoAppStack, w.list
	}
	best := order[0]
	for _, l := range order[1:] {
		if counts[l] > counts[best] {
			best = l
		}
	}
	return &best, "", w.list
}

type warnSet struct {
	seen map[string]bool
	list []string
}

func (w *warnSet) add(msg string) {
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	if !w.seen[msg] {
		w.seen[msg] = true
		w.list = append(w.list, msg)
	}
}

// locateSpan applies the loop rule to one span. Frames whose path cannot be resolved are skipped.
func locateSpan(env Env, sha string, s trace.Span, w *warnSet) (Location, bool) {
	frames := parseStack(s)
	module, _ := s.String(attrModulePath)
	var first *Location // innermost resolvable frame, used when no frame is in a loop
	for _, f := range frames {
		file, err := env.ResolvePath(sha, module, f.File)
		if err != nil {
			w.add(fmt.Sprintf("path_unresolved: %s (module %s) at %s: %v", f.File, module, sha, err))
			continue
		}
		in, err := env.InLoop(sha, file, f.Line)
		if err != nil {
			w.add(fmt.Sprintf("in_loop_unavailable: %s:%d at %s: %v", file, f.Line, sha, err))
			in = false
		}
		if in {
			return Location{Level: 1, Function: f.Function, File: file, Line: f.Line, InLoop: true}, true
		}
		if first == nil {
			first = &Location{Level: 1, Function: f.Function, File: file, Line: f.Line}
		}
	}
	if first != nil {
		return *first, true
	}
	return Location{}, false
}

// parseStack parses "<function> <file>:<line>" entries, innermost first, skipping malformed ones.
func parseStack(s trace.Span) []StackFrame {
	entries, _ := s.Strings(attrAppStack)
	var out []StackFrame
	for _, e := range entries {
		sp := strings.LastIndexByte(e, ' ')
		if sp <= 0 {
			continue
		}
		loc := e[sp+1:]
		colon := strings.LastIndexByte(loc, ':')
		if colon <= 0 {
			continue
		}
		line, err := strconv.Atoi(loc[colon+1:])
		if err != nil {
			continue
		}
		out = append(out, StackFrame{Function: e[:sp], File: loc[:colon], Line: line})
	}
	return out
}

type candKey struct {
	query string
	loc   Location
	has   bool
}

type candAgg struct {
	key      candKey
	reason   string
	traces   map[string]bool
	occs     []Occurrence
	warnings warnSet
}

// Detect receives the traces (one []Span each) of the entry operation in the bad version and
// returns the N+1 candidates, highest score first.
func Detect(env Env, sha string, traces [][]trace.Span) ([]Candidate, error) {
	total := len(traces)
	aggs := map[candKey]*candAgg{}
	var order []candKey
	for ti, spans := range traces {
		for _, occ := range FindOccurrences(spans) {
			loc, reason, warns := LocateWithWarnings(env, sha, occ.Spans)
			k := candKey{query: occ.Query}
			if loc != nil {
				k.loc, k.has = *loc, true
			}
			a, ok := aggs[k]
			if !ok {
				a = &candAgg{key: k, reason: reason, traces: map[string]bool{}}
				aggs[k] = a
				order = append(order, k)
			}
			a.traces[strconv.Itoa(ti)] = true
			a.occs = append(a.occs, occ)
			for _, msg := range warns {
				a.warnings.add(msg)
			}
		}
	}

	var out []Candidate
	for _, k := range order {
		a := aggs[k]
		affected := len(a.traces)
		if affected < MinAffectedTraces || float64(affected) < MinAffectedRatio*float64(total) {
			continue
		}
		out = append(out, buildCandidate(a, affected, total))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

func buildCandidate(a *candAgg, affected, total int) Candidate {
	sample := a.occs[0]
	c := Candidate{
		Query:              a.key.query,
		LocationReason:     a.reason,
		TracesAffected:     affected,
		TracesTotal:        total,
		SampleTraceID:      sample.TraceID,
		SampleParentSpanID: sample.ParentSpanID,
	}
	if a.key.has {
		loc := a.key.loc
		c.Location = &loc
	}
	c.Warnings = a.warnings.list
	c.Stack = parseStack(sample.Spans[0])
	c.ModulePath, _ = sample.Spans[0].String(attrModulePath)

	var sizes, rows []int
	matched := 0
	for _, o := range a.occs {
		sizes = append(sizes, len(o.Spans))
		rows = append(rows, o.RowsMedian)
		if o.PlusOne {
			matched++
		}
		if o.PhaseInferred {
			c.PhaseInferred = true
		}
	}
	c.GroupSizeMedian = medianInt(sizes)
	c.RowsMedian = medianInt(rows)
	c.PlusOneMatchedRatio = float64(matched) / float64(len(a.occs))
	strong := c.PlusOneMatchedRatio >= 0.5
	c.Score = float64(affected) / float64(total)
	c.Confidence = "medium"
	if strong {
		c.Score += 0.5
		if !c.PhaseInferred {
			c.Confidence = "high"
		}
	}
	return c
}
