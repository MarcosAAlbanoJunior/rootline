package detector

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// Dependency kinds of the time breakdown (ADR-0031). KindOwnTime is the service's own time: the
// root span not covered by any direct child.
const (
	KindDatabase = "db"
	KindHTTP     = "http"
	KindRPC      = "rpc"
	KindSpanName = "span"
	KindOwnTime  = "own"
)

// Dependency is the destination of a client span: the database system, the host:port of an HTTP
// call, the RPC service or, last, the span name (ADR-0031).
type Dependency struct {
	Kind, Name string
}

// OwnTime is the row of the service's own time in the breakdown.
var OwnTime = Dependency{Kind: KindOwnTime, Name: "own_time"}

// String is the dependency as people read it: "http:moderation:8090", "db:postgresql", "own_time".
func (d Dependency) String() string {
	if d.Kind == KindOwnTime {
		return d.Name
	}
	return d.Kind + ":" + d.Name
}

// DependencyOf returns the dependency a span calls. Only client spans and database spans (pool and
// connect spans of a driver may be internal) are calls to a dependency.
func DependencyOf(s trace.Span) (Dependency, bool) {
	if sys := DBSystem(s); sys != "" {
		return Dependency{Kind: KindDatabase, Name: sys}, true
	}
	if s.Kind != trace.KindClient {
		return Dependency{}, false
	}
	if svc, ok := s.String("rpc.service"); ok && svc != "" {
		return Dependency{Kind: KindRPC, Name: svc}, true
	}
	if hp := hostPort(s); hp != "" {
		return Dependency{Kind: KindHTTP, Name: hp}, true
	}
	return Dependency{Kind: KindSpanName, Name: s.Name}, true
}

// hostPort is the peer of an HTTP client span, from the current semconv attributes, the legacy ones
// or, last, the URL.
func hostPort(s trace.Span) string {
	for _, k := range [][2]string{{"server.address", "server.port"}, {"net.peer.name", "net.peer.port"}} {
		host, ok := s.String(k[0])
		if !ok || host == "" {
			continue
		}
		if port, ok := s.Int(k[1]); ok {
			return host + ":" + strconv.FormatInt(port, 10)
		}
		return host
	}
	for _, k := range []string{"url.full", "http.url"} {
		if raw, ok := s.String(k); ok {
			if u, err := url.Parse(raw); err == nil && u.Host != "" {
				return u.Host
			}
		}
	}
	return ""
}

// VersionTrace is one trace of the analyzed operation: its service root and all its spans.
type VersionTrace struct {
	Root  trace.Span
	Spans []trace.Span
}

// SlowestShare is the share of the slowest traces of each version left out of the mean of the
// breakdown (ADR-0031): the tail would otherwise decide it.
const SlowestShare = 0.05

// Share is what one dependency took in one version. MeanMs, CallsPerTrace and MsPerCall come from
// the traces in the mean; Presence is the fraction of all the traces where the dependency appears.
type Share struct {
	MeanMs, Presence, CallsPerTrace, MsPerCall float64
}

// OperationShare is an operation inside a dependency (a normalized query, an HTTP method, a span
// name) and the fraction of the traces where it appears in each version.
type OperationShare struct {
	Name                          string
	BaselinePresence, BadPresence float64
}

// DependencyShare is one row of the breakdown. Explained is the growth of its mean over the growth
// of the root mean, capped at 1, and zero when the root mean did not grow.
type DependencyShare struct {
	Dependency    Dependency
	Baseline, Bad Share
	Explained     float64
	Operations    []OperationShare
}

// DeltaMs is how much the mean of the dependency grew.
func (d DependencyShare) DeltaMs() float64 { return d.Bad.MeanMs - d.Baseline.MeanMs }

// RootShare is the root of one version: its mean duration over MeanTraces of its Traces.
type RootShare struct {
	MeanMs             float64
	Traces, MeanTraces int
}

// Breakdown is the time breakdown of an operation between two versions (ADR-0031): one row per
// dependency and one for the own time, the one that grew most first.
type Breakdown struct {
	Baseline, Bad RootShare
	Rows          []DependencyShare
}

// DeltaMs is how much the mean of the root grew.
func (b Breakdown) DeltaMs() float64 { return b.Bad.MeanMs - b.Baseline.MeanMs }

// TimeBreakdown splits the growth of the mean duration of an operation between its dependencies and
// its own time. The caller leaves out the traces at the ceiling; the mean also leaves out the
// slowest 5% of each version, and the presence counts every trace given (ADR-0031).
func TimeBreakdown(baseline, bad []VersionTrace) Breakdown {
	g, w := summarize(baseline), summarize(bad)
	b := Breakdown{Baseline: g.root, Bad: w.root}
	deps := map[Dependency]bool{}
	for d := range g.deps {
		deps[d] = true
	}
	for d := range w.deps {
		deps[d] = true
	}
	for d := range deps {
		r := DependencyShare{Dependency: d, Baseline: g.share(d), Bad: w.share(d)}
		if delta := b.DeltaMs(); delta > 0 {
			r.Explained = math.Min(r.DeltaMs()/delta, 1)
		}
		ops := map[string]bool{}
		for op := range g.ops[d] {
			ops[op] = true
		}
		for op := range w.ops[d] {
			ops[op] = true
		}
		for op := range ops {
			r.Operations = append(r.Operations, OperationShare{
				Name: op, BaselinePresence: g.presence(g.ops[d][op]), BadPresence: w.presence(w.ops[d][op]),
			})
		}
		sort.Slice(r.Operations, func(i, j int) bool {
			oi, oj := r.Operations[i], r.Operations[j]
			if di, dj := oi.BadPresence-oi.BaselinePresence, oj.BadPresence-oj.BaselinePresence; di != dj {
				return di > dj
			}
			return oi.Name < oj.Name
		})
		b.Rows = append(b.Rows, r)
	}
	sort.Slice(b.Rows, func(i, j int) bool {
		if di, dj := b.Rows[i].DeltaMs(), b.Rows[j].DeltaMs(); di != dj {
			return di > dj
		}
		return b.Rows[i].Dependency.String() < b.Rows[j].Dependency.String()
	})
	return b
}

// traceShape is one trace seen by the breakdown.
type traceShape struct {
	rootMs float64
	ms     map[Dependency]float64
	calls  map[Dependency]int
	ops    map[Dependency]map[string]bool
}

func shapeOf(vt VersionTrace) traceShape {
	ts := traceShape{
		rootMs: msBetween(vt.Root.Start, vt.Root.End),
		ms:     map[Dependency]float64{}, calls: map[Dependency]int{}, ops: map[Dependency]map[string]bool{},
	}
	spans := map[Dependency][]trace.Span{}
	var children []trace.Span
	for _, s := range underRoot(vt) {
		if s.ParentSpanID == vt.Root.SpanID {
			children = append(children, s)
		}
		d, ok := DependencyOf(s)
		if !ok {
			continue
		}
		spans[d] = append(spans[d], s)
		if ts.ops[d] == nil {
			ts.ops[d] = map[string]bool{}
		}
		ts.ops[d][operationOf(d, s)] = true
		if isCall(d, s) {
			ts.calls[d]++
		}
	}
	for d, ss := range spans {
		ts.ms[d] = unionMs(vt.Root, ss)
	}
	if own := ts.rootMs - unionMs(vt.Root, children); own > 0 {
		ts.ms[OwnTime] = own
		ts.ops[OwnTime] = map[string]bool{}
	}
	return ts
}

// unionMs is the time covered by the spans inside the root, counting overlaps once.
func unionMs(root trace.Span, spans []trace.Span) float64 {
	type iv struct{ from, to time.Time }
	var ivs []iv
	for _, s := range spans {
		from, to := s.Start, s.End
		if from.Before(root.Start) {
			from = root.Start
		}
		if to.After(root.End) {
			to = root.End
		}
		if to.After(from) {
			ivs = append(ivs, iv{from, to})
		}
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].from.Before(ivs[j].from) })
	var total time.Duration
	var cur iv
	for i, v := range ivs {
		switch {
		case i == 0:
			cur = v
		case !v.from.After(cur.to):
			if v.to.After(cur.to) {
				cur.to = v.to
			}
		default:
			total += cur.to.Sub(cur.from)
			cur = v
		}
	}
	if len(ivs) > 0 {
		total += cur.to.Sub(cur.from)
	}
	return float64(total) / float64(time.Millisecond)
}

// operationOf is the detail of a span inside its dependency.
func operationOf(d Dependency, s trace.Span) string {
	switch d.Kind {
	case KindDatabase:
		// db.query.text is parameterized; the legacy db.statement of some clients (Redis) carries the
		// literal values and would make an operation per key.
		if q, ok := s.String(attrQueryText); ok && q != "" {
			return Normalize(q)
		}
		if op, ok := s.String("db.operation.name"); ok && op != "" {
			return op
		}
	case KindHTTP:
		for _, k := range []string{"http.request.method", "http.method"} {
			if m, ok := s.String(k); ok && m != "" {
				return m
			}
		}
	}
	return s.Name
}

// isCall says whether a span is a call to its dependency. A database span is one only when it runs
// a statement: pool, connect and prepare spans are time of the dependency, not calls.
func isCall(d Dependency, s trace.Span) bool {
	if d.Kind != KindDatabase {
		return true
	}
	return queryText(s) != "" && PhaseOf(s) != PhasePrepare
}

func queryText(s trace.Span) string {
	if q, ok := s.String(attrQueryText); ok {
		return q
	}
	q, _ := s.String("db.statement")
	return q
}

// versionShape sums the traces of one version.
type versionShape struct {
	root  RootShare
	ms    map[Dependency]float64 // over the traces in the mean
	calls map[Dependency]int     // over the traces in the mean
	deps  map[Dependency]int     // traces where the dependency appears, over all
	ops   map[Dependency]map[string]int
}

func summarize(vts []VersionTrace) versionShape {
	v := versionShape{
		ms: map[Dependency]float64{}, calls: map[Dependency]int{}, deps: map[Dependency]int{},
		ops: map[Dependency]map[string]int{},
	}
	shapes := make([]traceShape, len(vts))
	for i, vt := range vts {
		shapes[i] = shapeOf(vt)
		for d, ops := range shapes[i].ops {
			v.deps[d]++
			if v.ops[d] == nil {
				v.ops[d] = map[string]int{}
			}
			for op := range ops {
				v.ops[d][op]++
			}
		}
	}
	sort.SliceStable(shapes, func(i, j int) bool { return shapes[i].rootMs < shapes[j].rootMs })
	kept := shapes[:len(shapes)-int(math.Floor(SlowestShare*float64(len(shapes))))]
	v.root = RootShare{Traces: len(vts), MeanTraces: len(kept)}
	for _, ts := range kept {
		v.root.MeanMs += ts.rootMs
		for d, ms := range ts.ms {
			v.ms[d] += ms
		}
		for d, n := range ts.calls {
			v.calls[d] += n
		}
	}
	if len(kept) > 0 {
		v.root.MeanMs /= float64(len(kept))
	}
	return v
}

func (v versionShape) presence(traces int) float64 {
	if v.root.Traces == 0 {
		return 0
	}
	return float64(traces) / float64(v.root.Traces)
}

func (v versionShape) share(d Dependency) Share {
	s := Share{Presence: v.presence(v.deps[d])}
	if n := v.root.MeanTraces; n > 0 {
		s.MeanMs = v.ms[d] / float64(n)
		s.CallsPerTrace = float64(v.calls[d]) / float64(n)
	}
	if c := v.calls[d]; c > 0 {
		s.MsPerCall = v.ms[d] / float64(c)
	}
	return s
}

func msBetween(from, to time.Time) float64 { return float64(to.Sub(from)) / float64(time.Millisecond) }

// CheckTraceShape is the id of the trace shape check.
const CheckTraceShape = "trace_shape"

// MinShapeDeltaMs is the guard of the trace shape (ADR-0031): no mechanism is promoted unless the
// mean per trace grew at least this much.
const MinShapeDeltaMs = 5.0

// TraceShapeCheck reports the breakdown of an operation with a latency regression: what was
// promoted, or the dependency that grew most and why the guard held. promoted is the candidate
// PromoteShape returned, if any.
func TraceShapeCheck(b Breakdown, promoted *Candidate) Check {
	c := Check{ID: CheckTraceShape, Status: CheckRan}
	means := fmt.Sprintf("mean per trace %.1f → %.1f ms (%+.1f ms, %d and %d traces)",
		b.Baseline.MeanMs, b.Bad.MeanMs, b.DeltaMs(), b.Baseline.MeanTraces, b.Bad.MeanTraces)
	if b.DeltaMs() < MinShapeDeltaMs {
		c.Reason = fmt.Sprintf("%s: the mean did not follow the p50, so no mechanism is promoted", means)
		c.Hint = "A bimodal latency can move the mean against the p50; the time breakdown is still in the evidence."
		return c
	}
	if promoted != nil {
		for _, r := range b.Rows {
			if r.Dependency == promoted.Dependency {
				c.Reason = fmt.Sprintf("%s; promoted %s on %s: %s, %.0f%% of the difference",
					means, promoted.Type, r.Dependency, mechanismFacts(promoted.Type, r), r.Explained*100)
				return c
			}
		}
	}
	if len(b.Rows) == 0 {
		c.Reason = means + "; nothing promoted"
		return c
	}
	top := b.Rows[0]
	c.Reason = fmt.Sprintf("%s; dominant: %s, %.0f%% of the difference; nothing promoted",
		means, top.Dependency, top.Explained*100)
	if top.Explained >= MinExplained-epsilon {
		switch top.Dependency.Kind {
		case KindDatabase:
			c.Reason += ": a slower database is not promoted: its origin is outside the trace (index, lock, pool)"
		case KindOwnTime:
			c.Reason += ": a larger own time is not promoted: its origin is outside the trace (CPU, lock, uninstrumented wait)"
		}
	}
	return c
}

// mechanismFacts are the numbers behind a promoted mechanism.
func mechanismFacts(typ string, r DependencyShare) string {
	if typ == TypeSlowCall {
		return fmt.Sprintf("%.1f → %.1f ms per call", r.Baseline.MsPerCall, r.Bad.MsPerCall)
	}
	return fmt.Sprintf("presence %.0f%% → %.0f%%", r.Baseline.Presence*100, r.Bad.Presence*100)
}

// underRoot are the spans of the root's service below the root. Another request of the same
// service in the trace (a retry, a self call) is not part of this one.
func underRoot(vt VersionTrace) []trace.Span {
	byID := make(map[string]trace.Span, len(vt.Spans))
	for _, s := range vt.Spans {
		byID[s.SpanID] = s
	}
	var out []trace.Span
	for _, s := range vt.Spans {
		if s.SpanID == vt.Root.SpanID || s.Service != vt.Root.Service {
			continue
		}
		p, seen := s.ParentSpanID, map[string]bool{}
		for p != "" && p != vt.Root.SpanID && !seen[p] {
			seen[p] = true
			p = byID[p].ParentSpanID
		}
		if p == vt.Root.SpanID {
			out = append(out, s)
		}
	}
	return out
}
