package analysis

import (
	"math"
	"sort"
)

// Regression thresholds (ADR-0032). They are constants until a real case asks for others.
const (
	MinRegressionSample    = 30   // traces in each version
	LatencyP50Ratio        = 1.5  // bad p50 / baseline p50
	LatencyP95Ratio        = 2.0  // bad p95 / baseline p95
	MinLatencyDeltaMs      = 5.0  // absolute increase that the ratio must also reach
	LatencyP50DoubledRatio = 2.0  // bad p50 / baseline p50 of the branch with the lower floor
	MinP50DoubledDeltaMs   = 2.0  // absolute increase of that branch: a fast operation's N+1
	MinP50DoubledBadMs     = 3.0  // bad p50 of that branch: an operation still this fast does not count
	MinErrorRateDelta      = 0.01 // one percentage point
	ErrorRateRatio         = 2.0  // the error rate at least doubles
)

// Regression types.
const (
	RegressionLatency         = "latency"
	RegressionError           = "error"
	RegressionLatencyAndError = "latency_and_error"
)

// VersionSample is what one version of an entry operation recorded: the duration of each trace and
// how many failed.
type VersionSample struct {
	DurationsMs []float64
	Errors      int
}

// VersionStats summarizes a VersionSample.
type VersionStats struct {
	N         int     `json:"n"`
	P50       float64 `json:"p50_ms"`
	P95       float64 `json:"p95_ms"`
	ErrorRate float64 `json:"error_rate"`
}

// Regression is the outcome of the rule for one entry operation. Enough says both versions reached
// the minimum sample; without it there is no regression of any type.
type Regression struct {
	Baseline, Bad  VersionStats
	Enough         bool
	Latency, Error bool
}

// Regressed reports whether the operation regressed in latency, error or both.
func (r Regression) Regressed() bool { return r.Latency || r.Error }

// Type is the regression type, or "" without one.
func (r Regression) Type() string {
	switch {
	case r.Latency && r.Error:
		return RegressionLatencyAndError
	case r.Latency:
		return RegressionLatency
	case r.Error:
		return RegressionError
	}
	return ""
}

// P50Ratio is the bad p50 over the baseline p50 (+Inf over a zero baseline).
func (r Regression) P50Ratio() float64 { return ratio(r.Bad.P50, r.Baseline.P50) }

// P95Ratio is the bad p95 over the baseline p95.
func (r Regression) P95Ratio() float64 { return ratio(r.Bad.P95, r.Baseline.P95) }

// NRatio is the bad trace count over the baseline one: well below 1, requests may be stuck or the
// load differs.
func (r Regression) NRatio() float64 { return ratio(float64(r.Bad.N), float64(r.Baseline.N)) }

// Regress applies the single regression rule of ADR-0032 to the two versions of one operation:
// both need the minimum sample; latency regresses when p50 grows 1.5x or p95 2x, with at least
// 5 ms more, or when p50 doubles with at least 2 ms more and reaches 3 ms; error regresses when
// its rate rises at least one point and at least doubles.
func Regress(baseline, bad VersionSample) Regression {
	r := Regression{Baseline: statsOf(baseline), Bad: statsOf(bad)}
	r.Enough = r.Baseline.N >= MinRegressionSample && r.Bad.N >= MinRegressionSample
	if !r.Enough {
		return r
	}
	b, w := r.Baseline, r.Bad
	p50 := w.P50 >= LatencyP50Ratio*b.P50 && w.P50-b.P50 >= MinLatencyDeltaMs
	p50Doubled := w.P50 >= LatencyP50DoubledRatio*b.P50 && w.P50-b.P50 >= MinP50DoubledDeltaMs &&
		w.P50 >= MinP50DoubledBadMs
	p95 := w.P95 >= LatencyP95Ratio*b.P95 && w.P95-b.P95 >= MinLatencyDeltaMs
	r.Latency = p50 || p50Doubled || p95
	// The epsilon keeps a rise of exactly one point from failing on float rounding.
	r.Error = w.ErrorRate-b.ErrorRate >= MinErrorRateDelta-1e-9 && w.ErrorRate >= ErrorRateRatio*b.ErrorRate
	return r
}

func statsOf(s VersionSample) VersionStats {
	st := VersionStats{N: len(s.DurationsMs)}
	if st.N == 0 {
		return st
	}
	d := append([]float64(nil), s.DurationsMs...)
	sort.Float64s(d)
	st.P50, st.P95 = percentile(d, 0.50), percentile(d, 0.95)
	st.ErrorRate = float64(s.Errors) / float64(st.N)
	return st
}

// percentile is the nearest-rank percentile of sorted values.
func percentile(sorted []float64, p float64) float64 {
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[max(i, 0)]
}

func ratio(a, b float64) float64 {
	if b == 0 {
		if a == 0 {
			return 1
		}
		return math.Inf(1)
	}
	return a / b
}
