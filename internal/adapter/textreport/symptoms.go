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

	b.WriteString("\n| Operation | Regression | n (base → bad) | n ratio | p50 ms (base → bad) | p50 ratio | p95 ms (base → bad) | errors (base → bad) |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, o := range r.Operations {
		fmt.Fprintf(&b, "| %s %s | %s | %d → %d | %s | %.1f → %.1f | %s | %.1f → %.1f | %.1f%% → %.1f%% |\n",
			o.Method, o.Route, regressionText(o), o.Baseline.N, o.Bad.N, ratioText(o.NRatio, ""),
			o.Baseline.P50, o.Bad.P50, ratioText(o.P50Ratio, "x"), o.Baseline.P95, o.Bad.P95,
			o.Baseline.ErrorRate*100, o.Bad.ErrorRate*100)
	}

	if r.Suggested == nil {
		b.WriteString("\nNo operation is suggested.\n")
	} else {
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
