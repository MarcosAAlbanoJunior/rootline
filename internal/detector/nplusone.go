package detector

import (
	"sort"
	"strconv"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// N+1 thresholds: initial values, to be calibrated with real data.
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
	attrDBSystemName = "db.system.name"
	attrDBSystem     = "db.system"
)

// Occurrence is one N+1 occurrence inside a single trace. ParentSpanID is the effective parent;
// when it came from collapsing repeated wrappers, CollapsedParentName and CollapsedParentCount say
// which and how many (ADR-0020).
type Occurrence struct {
	TraceID, ParentSpanID, Query string // Query is normalized
	Spans                        []trace.Span
	PlusOne                      bool
	PhaseInferred                bool
	RowsMedian                   int
	CollapsedParentName          string
	CollapsedParentCount         int
}

// Env is the port consumed by location. It is implemented by adapters (and fakes in tests).
type Env interface {
	// ResolvePath maps a module-relative file to a repo-relative path at sha.
	ResolvePath(sha, modulePath, relFile string) (string, error)
	// InLoop reports whether the line is strictly inside a for/range body at sha.
	InLoop(sha, file string, line int) (bool, error)
}

// Location is a level-1 code location. LoopChecked says whether the strategy looked for a loop;
// InLoop is meaningful only when it did (ADR-0024).
type Location struct {
	Level       int
	Function    string
	File        string
	Line        int
	InLoop      bool
	LoopChecked bool
}

// StackFrame is one entry of rootline.app_stack. File is module-relative.
type StackFrame struct {
	Function, File string
	Line           int
}

// Cause types.
const (
	TypeNPlusOne     = "n_plus_one"
	TypeMissingIndex = "missing_index"
)

// Candidate is a root-cause candidate. The fields after TracesTotal describe the N+1 repetition and
// are zero in other types; Plan is set only in missing-index candidates.
type Candidate struct {
	Type string
	// Target is what the cause points at besides the function, when the type has one and it comes
	// from a structured field (ADR-0026): the table in a missing index; empty in an N+1.
	Target              string
	Plan                *PlanFinding
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
	// CollapsedParentName and CollapsedParentCount come from the sample occurrence: the repeated
	// wrapper skipped to reach the effective parent, if any. They do not change Score or Confidence.
	CollapsedParentName  string
	CollapsedParentCount int
	Warnings             []string // location problems (unresolved path, index failure), deduplicated
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

// Discarded is a query whose occurrences had the pagination shape in Traces traces; they left
// before the aggregation (ADR-0021).
type Discarded struct {
	Query  string
	Traces int
}

// Detect receives the traces (one []Span each) of the entry operation in the bad version and
// returns the N+1 candidates, highest score first, and the queries discarded as pagination, in the
// order in which they appeared.
func Detect(env Env, sha string, traces [][]trace.Span) ([]Candidate, []Discarded, error) {
	total := len(traces)
	aggs := map[candKey]*candAgg{}
	var order []candKey
	var discarded []Discarded
	discardedAt := map[string]int{} // query -> index in discarded
	for ti, spans := range traces {
		counted := map[string]bool{} // a trace counts once per query, even with several occurrences
		for _, occ := range FindOccurrences(spans) {
			if paginationShape(occ) {
				i, seen := discardedAt[occ.Query]
				if !seen {
					i = len(discarded)
					discardedAt[occ.Query] = i
					discarded = append(discarded, Discarded{Query: occ.Query})
				}
				if !counted[occ.Query] {
					counted[occ.Query] = true
					discarded[i].Traces++
				}
				continue
			}
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
	return out, discarded, nil
}

func buildCandidate(a *candAgg, affected, total int) Candidate {
	sample := a.occs[0]
	c := Candidate{
		Type:                 TypeNPlusOne,
		Query:                a.key.query,
		LocationReason:       a.reason,
		TracesAffected:       affected,
		TracesTotal:          total,
		SampleTraceID:        sample.TraceID,
		SampleParentSpanID:   sample.ParentSpanID,
		CollapsedParentName:  sample.CollapsedParentName,
		CollapsedParentCount: sample.CollapsedParentCount,
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
