package textreport

import (
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
)

// RenderSymptoms writes the comparison of the operations as a Markdown table, the suggestion and,
// when there is one, the ready analyze command.
func RenderSymptoms(w io.Writer, r analysis.SymptomsReport, analyzeCommand string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Symptoms of %s: %s → %s\n\n", r.Service, short(r.BaselineVersion), short(r.BadVersion))
	fmt.Fprintf(&b, "Baseline %s, bad version %s (%s).\n", r.BaselineVersion, r.BadVersion, orderText(r.VersionOrder))
	for _, n := range r.Notices {
		fmt.Fprintf(&b, "\n> %s\n", n)
	}
	if r.Generalized {
		fmt.Fprintf(&b, "\n**Generalized regression:** at least %.0f%% of the operations with the minimum sample regressed together. This usually points to a saturated shared resource (pool, lock, CPU), not to each operation.\n",
			analysis.GeneralizedShare*100)
	}

	b.WriteString("\n| Operation | Regression | n (base → bad) | n ratio | p50 ms (base → bad) | p50 ratio | p95 ms (base → bad) | errors (base → bad) | onset | ceiling |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, o := range r.Operations {
		nRatio := ratioText(o.NRatio, "")
		if o.NDrop {
			nRatio = "**" + nRatio + " drop**"
		}
		fmt.Fprintf(&b, "| %s %s | %s | %d → %d | %s | %.1f → %.1f | %s | %.1f → %.1f | %.1f%% → %.1f%% | %s | %s |\n",
			o.Method, o.Route, regressionText(o), o.Baseline.N, o.Bad.N, nRatio,
			o.Baseline.P50, o.Bad.P50, ratioText(o.P50Ratio, "x"), o.Baseline.P95, o.Bad.P95,
			o.Baseline.ErrorRate*100, o.Bad.ErrorRate*100, onsetText(o.OnsetS), ceilingText(o.CeilingMs))
	}
	b.WriteString("\nonset: the 3rd trace above the baseline p95, counted from the first trace of the bad version. drop: the bad version has under half the traces (stuck requests that never became spans, or a different load). ceiling: at least 20% of the traces stop together, usually cut by a timeout.\n")

	switch {
	case r.Suggested == nil && r.Generalized:
		fmt.Fprintf(&b, "\nNo operation is suggested: none stands out (the largest p50 ratio is not %.0fx the second). Compare the onset and the drop columns to choose one.\n", analysis.StandOutRatio)
	case r.Suggested == nil:
		b.WriteString("\nNo operation is suggested: none regressed.\n")
	default:
		fmt.Fprintf(&b, "\nSuggested operation (a suggestion, not the origin): %s %s.\n", r.Suggested.Method, r.Suggested.Route)
		if analyzeCommand != "" {
			fmt.Fprintf(&b, "\n    %s\n", analyzeCommand)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func orderText(order string) string {
	switch order {
	case analysis.OrderGit:
		return "ordered by the git history"
	case analysis.OrderTime:
		return "ordered by time"
	}
	return "given in the flags"
}

func regressionText(o analysis.OperationSymptom) string {
	switch {
	case !o.EnoughSample:
		return fmt.Sprintf("sample < %d", analysis.MinRegressionSample)
	case o.Regression == "":
		return "no"
	}
	return strings.ReplaceAll(o.Regression, "_", " ")
}

func ratioText(v *float64, suffix string) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f%s", *v, suffix)
}

func onsetText(s *float64) string {
	if s == nil {
		return "-"
	}
	return fmt.Sprintf("+%.1fs", *s)
}

func ceilingText(ms *float64) string {
	if ms == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f ms", *ms)
}
