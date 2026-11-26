package analysis

import (
	"fmt"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
)

// shapeRun is the time breakdown of an operation with a latency regression (ADR-0031) and the bad
// traces it read. Without a latency regression there is no breakdown, and check says why.
type shapeRun struct {
	breakdown *detector.Breakdown
	bad       []detector.VersionTrace
	check     Check
}

func traceShape(s Symptom, good, bad []versionTrace) shapeRun {
	c := Check{ID: detector.CheckTraceShape, Status: CheckNotApplicable}
	switch {
	case len(bad) == 0:
		c.Reason = "no traces of the operation in the bad version"
		return shapeRun{check: c}
	case s.BaselineVersion == "":
		c.Reason = "no baseline version: there is no time to split"
		return shapeRun{check: c}
	case len(good) == 0:
		c.Reason = "no traces of the operation in the baseline version"
		return shapeRun{check: c}
	}
	r := Regress(sampleOf(good), sampleOf(bad))
	if !r.Enough {
		c.Reason = fmt.Sprintf("fewer than %d traces in a version: no latency regression can be told, see symptom_regression", MinRegressionSample)
		return shapeRun{check: c}
	}
	if !r.Latency {
		c.Reason = "no latency regression: the time breakdown only splits a slowdown"
		return shapeRun{check: c}
	}
	// The ceiling is a property of the bad version (ADR-0030); the baseline is taken as it is.
	_, badKept := splitAtCeiling(bad)
	run := shapeRun{bad: shapeTraces(badKept)}
	b := detector.TimeBreakdown(shapeTraces(good), run.bad)
	run.breakdown = &b
	return run
}

// isShape says whether a cause type comes from the trace shape. Those candidates always come after
// the specific ones (N+1, missing index), whatever the score (ADR-0031).
func isShape(typ string) bool { return typ == detector.TypeAddedWork || typ == detector.TypeSlowCall }

func shapeTraces(vts []versionTrace) []detector.VersionTrace {
	out := make([]detector.VersionTrace, len(vts))
	for i, vt := range vts {
		out[i] = detector.VersionTrace{Root: vt.root, Spans: vt.spans}
	}
	return out
}

func breakdownEvidence(id string, b detector.Breakdown) TimeBreakdownEvidence {
	ev := TimeBreakdownEvidence{
		ID: id, Kind: "time_breakdown",
		BaselineMeanMs: b.Baseline.MeanMs, BadMeanMs: b.Bad.MeanMs,
		BaselineTraces: b.Baseline.Traces, BadTraces: b.Bad.Traces,
		BaselineMeanTraces: b.Baseline.MeanTraces, BadMeanTraces: b.Bad.MeanTraces,
		Dependencies: make([]DependencyTime, 0, len(b.Rows)),
	}
	for _, r := range b.Rows {
		row := DependencyTime{
			Dependency: r.Dependency.String(), Kind: r.Dependency.Kind,
			Baseline: versionTime(r.Baseline), Bad: versionTime(r.Bad), Explained: r.Explained,
			Operations: make([]OperationPresence, 0, len(r.Operations)),
		}
		for _, op := range r.Operations {
			row.Operations = append(row.Operations, OperationPresence{
				Operation: op.Name, BaselinePresence: op.BaselinePresence, BadPresence: op.BadPresence,
			})
		}
		ev.Dependencies = append(ev.Dependencies, row)
	}
	return ev
}

func versionTime(s detector.Share) VersionTime {
	return VersionTime{MeanMs: s.MeanMs, Presence: s.Presence, CallsPerTrace: s.CallsPerTrace, MsPerCall: s.MsPerCall}
}
