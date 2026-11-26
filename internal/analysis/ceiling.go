package analysis

import "fmt"

// Ceiling thresholds (ADR-0032): a share of the traces of a version stopping within a band below
// the longest one.
const (
	CeilingMinShare = 0.20
	CeilingBand     = 0.02
)

// CheckTruncatedTraces is the id of the check on the ceiling.
const CheckTruncatedTraces = "truncated_traces"

// Ceiling says whether the traces of one version of an operation stop at a common duration,
// usually a timeout. AtMs is the longest duration; Traces counts the traces within the band below
// it, out of N.
type Ceiling struct {
	Found  bool
	AtMs   float64
	Traces int
	N      int
}

// at reports whether a duration is within the band of the ceiling.
func (c Ceiling) at(ms float64) bool { return c.Found && ms >= (1-CeilingBand)*c.AtMs }

// CeilingOf applies the ceiling rule of ADR-0032 to the durations of one version: at least 20% of
// the traces within 2% of the longest. It needs the minimum sample of the regression rule, since a
// handful of traces always cluster somewhere. All of them stopping together is a ceiling too: every
// request hit the timeout.
func CeilingOf(durationsMs []float64) Ceiling {
	c := Ceiling{N: len(durationsMs)}
	for _, d := range durationsMs {
		c.AtMs = max(c.AtMs, d)
	}
	for _, d := range durationsMs {
		if d >= (1-CeilingBand)*c.AtMs {
			c.Traces++
		}
	}
	c.Found = c.N >= MinRegressionSample && c.AtMs > 0 && float64(c.Traces) >= CeilingMinShare*float64(c.N)
	return c
}

// splitAtCeiling separates the traces of the bad version that stopped at the ceiling, which the
// N+1 count leaves out because they were cut and have fewer child spans (ADR-0027). When every
// trace is at the ceiling, all of them are kept: there is no complete trace to count instead, and a
// steady duration would otherwise blind the detector.
func splitAtCeiling(bad []versionTrace) (Ceiling, []versionTrace) {
	c := CeilingOf(sampleOf(bad).DurationsMs)
	if !c.Found {
		return c, bad
	}
	var kept []versionTrace
	for _, vt := range bad {
		if !c.at(durationMs(vt.root)) {
			kept = append(kept, vt)
		}
	}
	if len(kept) == 0 {
		return c, bad
	}
	return c, kept
}

// truncatedCheck reports the ceiling. nPlusOneRan says whether the N+1 detector had query spans to
// count; excluded is how many traces left its count.
func truncatedCheck(c Ceiling, excluded int, nPlusOneRan bool) Check {
	ch := Check{ID: CheckTruncatedTraces}
	switch {
	case c.N == 0:
		ch.Status, ch.Reason = CheckNotApplicable, "no traces of the operation in the bad version"
	case c.N < MinRegressionSample:
		ch.Status = CheckSkipped
		ch.Reason = fmt.Sprintf("%d traces in the bad version; at least %d are needed to look for a ceiling", c.N, MinRegressionSample)
	case c.Found:
		ch.Status = CheckRan
		ch.Reason = fmt.Sprintf("%d of %d traces (%.0f%%) stopped within %.0f%% of %.1f ms in the bad version",
			c.Traces, c.N, 100*float64(c.Traces)/float64(c.N), CeilingBand*100, c.AtMs)
		ch.Hint = "These traces were probably cut by a timeout: the real latency is higher, and they have fewer child spans than the work done."
		switch {
		case !nPlusOneRan:
		case excluded > 0:
			ch.Hint += " They were left out of the N+1 count."
		default:
			ch.Hint += " With no complete trace left, the N+1 counted them anyway."
		}
	default:
		ch.Status = CheckRan
		ch.Reason = fmt.Sprintf("no ceiling: %d of %d traces within %.0f%% of the longest (%.1f ms)",
			c.Traces, c.N, CeilingBand*100, c.AtMs)
	}
	return ch
}
