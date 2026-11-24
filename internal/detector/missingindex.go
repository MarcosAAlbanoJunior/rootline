package detector

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// Missing-index thresholds (ADR-0023): initial values, to be calibrated like the N+1 ones.
const (
	MinTableRows       = 10000
	MaxScanSelectivity = 0.01
)

// Plan node types read by the detector.
const (
	nodeSeqScan         = "Seq Scan"
	nodeParallelSeqScan = "Parallel Seq Scan"
	nodeGather          = "Gather"
	nodeGatherMerge     = "Gather Merge"
)

// QueryPlan is the recorded execution plan of one normalized query: the raw output of
// EXPLAIN (FORMAT JSON) and the estimated row count (pg_class.reltuples) of each table in it. When
// the query could not be explained, Plan is empty and Unexplainable says why. Generic says the plan
// was made for any parameter value (GENERIC_PLAN), so its estimates may differ from a real value's.
type QueryPlan struct {
	Plan          json.RawMessage
	RelTuples     map[string]float64
	Generic       bool
	Unexplainable string
}

// PlanSource gives the plan of a normalized query at a version. found is false when the query was
// not recorded.
type PlanSource interface {
	Plan(sha, query string) (plan QueryPlan, found bool, err error)
}

// PlanFinding is the plan node behind a missing-index candidate. PlanRows is the total estimate (the
// Gather one for a parallel scan). FilterColumns is a best effort over the raw Filter text and stays
// out of the cause target (ADR-0026).
type PlanFinding struct {
	NodeType      string
	Table         string
	RelTuples     float64
	PlanRows      float64
	Selectivity   float64
	Filter        string
	FilterColumns []string
	GenericPlan   bool
}

// indexAgg aggregates the spans of one (query, location) across traces.
type indexAgg struct {
	reason   string
	traces   map[int]bool
	sample   trace.Span
	warnings warnSet
}

// planNode is the part of a Postgres JSON plan node the detector reads.
type planNode struct {
	NodeType     string     `json:"Node Type"`
	RelationName string     `json:"Relation Name"`
	PlanRows     float64    `json:"Plan Rows"`
	Filter       string     `json:"Filter"`
	Plans        []planNode `json:"Plans"`
}

// DetectMissingIndex receives the traces of the entry operation in the bad version and returns the
// missing-index candidates (ADR-0023), highest score first: a query whose plan scans a large table
// sequentially through a selective filter. The candidates are located at the innermost app frame,
// where the query is issued (ADR-0024), and aggregated as in the N+1 detector.
func DetectMissingIndex(env Env, plans PlanSource, sha string, traces [][]trace.Span) ([]Candidate, error) {
	total := len(traces)
	findings := map[string]*PlanFinding{} // query -> finding, nil when the plan has none
	aggs := map[candKey]*indexAgg{}
	var order []candKey
	for ti, spans := range traces {
		for _, s := range spans {
			if PhaseOf(s) != PhaseQuery {
				continue
			}
			text, _ := s.String(attrQueryText)
			q := Normalize(text)
			f, seen := findings[q]
			if !seen {
				var err error
				if f, err = findingOf(plans, sha, q); err != nil {
					return nil, err
				}
				findings[q] = f
			}
			if f == nil {
				continue
			}
			loc, reason, warns := LocateWith(InnermostFrame, env, sha, []trace.Span{s})
			k := candKey{query: q}
			if loc != nil {
				k.loc, k.has = *loc, true
			}
			a, ok := aggs[k]
			if !ok {
				a = &indexAgg{reason: reason, traces: map[int]bool{}, sample: s}
				aggs[k] = a
				order = append(order, k)
			}
			a.traces[ti] = true
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
		sample := a.sample
		f := findings[k.query]
		c := Candidate{
			Type:           TypeMissingIndex,
			Target:         f.Table,
			Plan:           f,
			Query:          k.query,
			LocationReason: a.reason,
			Stack:          parseStack(sample),
			Confidence:     "medium",
			Score:          float64(affected) / float64(total),
			TracesAffected: affected,
			TracesTotal:    total,
			SampleTraceID:  sample.TraceID,
			Warnings:       a.warnings.list,
		}
		c.ModulePath, _ = sample.String(attrModulePath)
		if k.has {
			loc := k.loc
			c.Location = &loc
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

// findingOf evaluates the plan of a query; nil means no missing-index finding (or no plan).
func findingOf(plans PlanSource, sha, query string) (*PlanFinding, error) {
	p, found, err := plans.Plan(sha, query)
	if err != nil {
		return nil, err
	}
	if !found || len(p.Plan) == 0 {
		return nil, nil
	}
	var roots []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(p.Plan, &roots); err != nil {
		return nil, fmt.Errorf("plan of %q at %s: %w", query, sha, err)
	}
	var best *PlanFinding
	for _, r := range roots {
		walkPlan(r.Plan, -1, func(n planNode, totalRows float64) {
			f, ok := evaluateScan(n, totalRows, p.RelTuples)
			// Of several qualifying scans, the largest table is the biggest waste.
			if ok && (best == nil || f.RelTuples > best.RelTuples) {
				f.GenericPlan = p.Generic
				best = &f
			}
		})
	}
	return best, nil
}

// walkPlan visits every node with its total row estimate: its own Plan Rows, or, for a parallel
// node directly under a Gather, the Gather's (a parallel node's own estimate is per worker). With
// a node in between (a partial aggregate), the Gather estimate is no longer the scan's, so the
// parallel node gets -1: unknown.
func walkPlan(n planNode, gatherRows float64, visit func(n planNode, totalRows float64)) {
	total := n.PlanRows
	if n.NodeType == nodeParallelSeqScan {
		total = gatherRows
	}
	visit(n, total)
	childGather := -1.0
	if n.NodeType == nodeGather || n.NodeType == nodeGatherMerge {
		childGather = n.PlanRows
	}
	for _, c := range n.Plans {
		walkPlan(c, childGather, visit)
	}
}

// evaluateScan applies the three conditions of ADR-0023 to one node: a filtered sequential scan, a
// large table and a selective filter. No time enters the rule.
func evaluateScan(n planNode, totalRows float64, reltuples map[string]float64) (PlanFinding, bool) {
	if n.NodeType != nodeSeqScan && n.NodeType != nodeParallelSeqScan {
		return PlanFinding{}, false
	}
	if n.Filter == "" || n.RelationName == "" || totalRows < 0 {
		return PlanFinding{}, false
	}
	rel := reltuples[n.RelationName]
	if rel < MinTableRows {
		return PlanFinding{}, false
	}
	sel := totalRows / rel
	if sel >= MaxScanSelectivity {
		return PlanFinding{}, false
	}
	return PlanFinding{
		NodeType: n.NodeType, Table: n.RelationName, RelTuples: rel, PlanRows: totalRows, Selectivity: sel,
		Filter: n.Filter, FilterColumns: FilterColumns(n.Filter),
	}, true
}

// columnEqualsParam matches "(column = $n)" as Postgres prints it in a plan Filter.
var columnEqualsParam = regexp.MustCompile(`\(([A-Za-z_][A-Za-z0-9_]*) = \$[0-9]+\)`)

// FilterColumns extracts, in order and without repeats, the columns compared to a parameter in a
// plan Filter, only in the form "column = $n". It is a best effort over text, not a SQL parser
// (ADR-0006): casts, qualified names and other operators give no column.
func FilterColumns(filter string) []string {
	cols := []string{}
	seen := map[string]bool{}
	for _, m := range columnEqualsParam.FindAllStringSubmatch(filter, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			cols = append(cols, m[1])
		}
	}
	return cols
}
