package analysis

import (
	"fmt"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// Check ids of the symptom itself.
const (
	CheckSymptomRegression = "symptom_regression"
	CheckErrorRate         = "error_rate"
)

// sampleOf is what the traces of one version recorded for the regression rule.
func sampleOf(vts []versionTrace) VersionSample {
	var s VersionSample
	for _, vt := range vts {
		s.DurationsMs = append(s.DurationsMs, durationMs(vt.root))
		if vt.root.Failed() {
			s.Errors++
		}
	}
	return s
}

// regressionChecks are the symptom_regression and error_rate checks (ADR-0027): whether the
// operation really got slower, and whether its errors rose, by the rule of ADR-0032. noLatency
// says the rule ran and found no latency regression.
func regressionChecks(s Symptom, bad, good []versionTrace) (reg, errs Check, noLatency bool) {
	reg = Check{ID: CheckSymptomRegression}
	errs = Check{ID: CheckErrorRate}
	badSample := sampleOf(bad)
	badStats := statsOf(badSample)
	switch {
	case len(bad) == 0:
		reg.Status, reg.Reason = CheckNotApplicable, "no traces of the operation in the bad version"
		errs.Status, errs.Reason = CheckNotApplicable, reg.Reason
		return reg, errs, false
	case s.BaselineVersion == "":
		reg.Status, reg.Reason = CheckNotApplicable, "no baseline version to compare with"
		reg.Hint = "Pass --baseline-version to check that the operation regressed; rootline symptoms finds the versions and the operation."
		errs.Status = CheckNotApplicable
		errs.Reason = "no baseline version; bad version " + rate(badSample.Errors, badStats)
		return reg, errs, false
	case len(good) == 0:
		reg.Status = CheckNotApplicable
		reg.Reason = fmt.Sprintf("no traces of the operation in baseline version %s", s.BaselineVersion)
		errs.Status, errs.Reason = CheckNotApplicable, reg.Reason+"; bad version "+rate(badSample.Errors, badStats)
		return reg, errs, false
	}

	goodSample := sampleOf(good)
	r := Regress(goodSample, badSample)
	stats := fmt.Sprintf("baseline %s: n=%d, p50 %.1f ms, p95 %.1f ms; bad %s: n=%d, p50 %.1f ms, p95 %.1f ms; n ratio %.2f",
		s.BaselineVersion, r.Baseline.N, r.Baseline.P50, r.Baseline.P95,
		s.BadVersion, r.Bad.N, r.Bad.P50, r.Bad.P95, r.NRatio())
	rates := fmt.Sprintf("baseline %s, bad %s", rate(goodSample.Errors, r.Baseline), rate(badSample.Errors, r.Bad))
	if !r.Enough {
		reg.Status = CheckSkipped
		reg.Reason = fmt.Sprintf("fewer than %d traces in a version (%s)", MinRegressionSample, stats)
		reg.Hint = fmt.Sprintf("Record more traffic of this operation: at least %d traces in each version.", MinRegressionSample)
		errs.Status, errs.Reason, errs.Hint = CheckSkipped, fmt.Sprintf("fewer than %d traces in a version (%s)", MinRegressionSample, rates), reg.Hint
		return reg, errs, false
	}

	reg.Status = CheckRan
	if r.Latency {
		reg.Reason = fmt.Sprintf("%s; latency regression (p50 %.1fx, p95 %.1fx)", stats, r.P50Ratio(), r.P95Ratio())
	} else {
		reg.Reason = fmt.Sprintf("%s; no latency regression: %s", stats, r.LatencyShortfall())
		reg.Hint = "The detectors ran anyway. If this is not the operation that got slower, find it with rootline symptoms."
	}
	if r.NRatio() < disappearanceRatio {
		reg.Hint = joinHints(reg.Hint, "The bad version has far fewer traces: requests may be stuck without becoming spans, or the load differed.")
	}

	errs.Status, errs.Reason = CheckRan, rates
	if r.Error {
		errs.Reason += "; error regression"
		errs.Hint = "Rootline v1 analyzes latency only: the rise in errors is reported, but its cause is not analyzed."
	}
	return reg, errs, !r.Latency
}

// warningCauseWithoutRegression is added to a Result with a Cause in an operation the rule found
// without a latency regression (ADR-0032).
const warningCauseWithoutRegression = "the cause is in the code of the operation, but the operation has no latency regression (see symptom_regression): the detectors read the shape of the traces, not their latency, so the cause may not explain the slowdown you are after; rootline symptoms shows whether another operation regressed"

// hintClientTimeouts explains an error rate of zero next to a ceiling.
const hintClientTimeouts = "No trace ended in error. If the ceiling of the truncated_traces check is a timeout of the client, that is expected: a request the client gave up on is usually not marked as an error on the server span."

// disappearanceRatio is the n ratio under which the drop in traces is pointed out (ADR-0032).
const disappearanceRatio = 0.5

func rate(errors int, st VersionStats) string {
	return fmt.Sprintf("%.1f%% (%d of %d)", st.ErrorRate*100, errors, st.N)
}

func joinHints(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}

func durationMs(s trace.Span) float64 {
	return float64(s.End.Sub(s.Start)) / float64(time.Millisecond)
}
