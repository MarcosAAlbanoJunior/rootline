package detector

import (
	"math"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// Cause types of the trace shape (ADR-0031). The target of both is the dependency.
const (
	TypeAddedWork = "added_work"
	TypeSlowCall  = "slow_call"
)

// Thresholds of the shape mechanisms (ADR-0031). They came from principle, not from tuning.
const (
	MinExplained      = 0.80 // share of the growth of the mean the dependency accounts for
	MinPresenceRise   = 0.25 // rise of the presence of the dependency, added_work
	MinCallSlowdown   = 2.0  // time per call, bad over baseline, slow_call
	MaxPresenceChange = 0.25 // presence change a slow call tolerates, slow_call
)

// epsilon keeps a value exactly at a threshold from failing on float rounding.
const epsilon = 1e-9

// ShapeMechanism returns the mechanism the breakdown supports and its dependency, if any: the
// first row, in breakdown order, that passes the rule of a promoted type. Nothing is promoted
// while the guard holds (the mean grew less than MinShapeDeltaMs). A slower database and a larger
// own time are never promoted: their origin is outside the trace.
func ShapeMechanism(b Breakdown) (typ string, row DependencyShare, ok bool) {
	if b.DeltaMs() < MinShapeDeltaMs {
		return "", DependencyShare{}, false
	}
	for _, r := range b.Rows {
		if r.Dependency.Kind == KindOwnTime || r.Explained < MinExplained-epsilon {
			continue
		}
		change := r.Bad.Presence - r.Baseline.Presence
		if change >= MinPresenceRise-epsilon {
			return TypeAddedWork, r, true
		}
		external := r.Dependency.Kind == KindHTTP || r.Dependency.Kind == KindRPC
		slower := r.Baseline.MsPerCall > 0 && r.Bad.MsPerCall >= MinCallSlowdown*r.Baseline.MsPerCall-epsilon
		if external && slower && math.Abs(change) < MaxPresenceChange-epsilon {
			return TypeSlowCall, r, true
		}
	}
	return "", DependencyShare{}, false
}

// ReasonNoCommonFrame is why an added_work has no level-1 location when the stacks of its
// operations share no frame.
const ReasonNoCommonFrame = "no_common_frame"

// PromoteShape returns the candidate of the mechanism the breakdown supports (ADR-0031): its target
// is the dependency, its confidence medium and its score the explained fraction. An added_work is
// located at the innermost app frame common to the stacks of the operations whose presence rose;
// bad holds the traces of the bad version the breakdown read. A slow_call has no level-1 location:
// the span of the call carries no app stack.
func PromoteShape(env Env, sha string, b Breakdown, bad []VersionTrace) (Candidate, bool) {
	typ, r, ok := ShapeMechanism(b)
	if !ok {
		return Candidate{}, false
	}
	c := Candidate{Type: typ, Target: r.Dependency.Name, Dependency: r.Dependency, Confidence: "medium", Score: r.Explained, LocationReason: ReasonNoAppStack}
	if typ == TypeAddedWork {
		w := &warnSet{}
		c.Location, c.Stack, c.ModulePath, c.LocationReason = commonFrame(env, sha, addedSpans(r, bad), w)
		c.Warnings = w.list
	}
	return c, true
}

// addedSpans are the spans, in the bad traces, of the operations of the dependency whose presence
// rose at least MinPresenceRise or, when none did on its own, of the one that rose most.
func addedSpans(r DependencyShare, bad []VersionTrace) []trace.Span {
	ops := map[string]bool{}
	var top OperationShare
	for i, op := range r.Operations {
		rise := op.BadPresence - op.BaselinePresence
		if rise >= MinPresenceRise-epsilon {
			ops[op.Name] = true
		}
		if i == 0 || rise > top.BadPresence-top.BaselinePresence {
			top = op
		}
	}
	if len(ops) == 0 {
		ops[top.Name] = true
	}
	var out []trace.Span
	for _, vt := range bad {
		for _, s := range underRoot(vt) {
			if d, ok := DependencyOf(s); ok && d == r.Dependency && ops[operationOf(d, s)] {
				out = append(out, s)
			}
		}
	}
	return out
}

// commonFrame is the innermost app frame present in every stack of the spans. Its line is the most
// frequent one of that function among the stacks (the smallest on a tie): the function may make
// each call from a different line. The stack returned starts at that frame, for the correlation.
func commonFrame(env Env, sha string, spans []trace.Span, w *warnSet) (*Location, []StackFrame, string, string) {
	var stacks [][]StackFrame
	module := ""
	for _, s := range spans {
		if st := parseStack(s); len(st) > 0 {
			if len(stacks) == 0 {
				module, _ = s.String(attrModulePath)
			}
			stacks = append(stacks, st)
		}
	}
	if len(stacks) == 0 {
		return nil, nil, "", ReasonNoAppStack
	}
	common := map[string]int{}
	for _, st := range stacks {
		seen := map[string]bool{}
		for _, f := range st {
			if !seen[f.Function] {
				seen[f.Function] = true
				common[f.Function]++
			}
		}
	}
	at := -1
	for i, f := range stacks[0] {
		if common[f.Function] == len(stacks) {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, nil, module, ReasonNoCommonFrame
	}
	fn := stacks[0][at].Function
	counts := map[StackFrame]int{}
	for _, st := range stacks {
		for _, f := range st {
			if f.Function == fn {
				counts[f]++
				break
			}
		}
	}
	best := stacks[0][at]
	for f, n := range counts {
		if n > counts[best] || (n == counts[best] && (f.File < best.File || (f.File == best.File && f.Line < best.Line))) {
			best = f
		}
	}
	file, ok := resolveFrame(env, sha, module, best, w)
	if !ok {
		return nil, nil, module, ReasonPathUnresolved
	}
	stack := append([]StackFrame{best}, stacks[0][at+1:]...)
	return &Location{Level: 1, Function: declaredFunction(fn), File: file, Line: best.Line}, stack, module, ""
}
