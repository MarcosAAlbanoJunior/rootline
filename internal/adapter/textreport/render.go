// Package textreport renders an analysis Result as plain Markdown for people reading a terminal
// (ADR-0027). The JSON form stays the contract for machines.
package textreport

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
)

// Render writes r in this order: the symptom in one line, the cause or "Inconclusive", the
// evidence, the checks (skipped first, with their hints) and the warnings.
func Render(w io.Writer, r analysis.Result) error {
	var b strings.Builder
	sy := r.Symptom
	fmt.Fprintf(&b, "# %s %s %s at %s", sy.Service, sy.Operation.Method, sy.Operation.Route, short(sy.BadVersion))
	if sy.BaselineVersion != nil {
		fmt.Fprintf(&b, " (baseline %s)", short(*sy.BaselineVersion))
	}
	b.WriteString("\n\n")

	if r.Cause == nil {
		b.WriteString("## Inconclusive\n\nNo cause is supported by the evidence. The checks below say what was verified and what was missing.\n")
	} else {
		writeCause(&b, r.Cause)
	}

	if len(r.Candidates) > 1 {
		b.WriteString("\n## Other candidates\n\n")
		for _, c := range r.Candidates[1:] {
			fmt.Fprintf(&b, "- %s %s, score %.2f, confidence %s%s\n", c.ID, c.Type, c.Score, c.Confidence, locationSuffix(c.Location))
		}
	}

	if len(r.Evidence) > 0 {
		b.WriteString("\n## Evidence\n\n")
		for _, e := range r.Evidence {
			fmt.Fprintf(&b, "- %s\n", evidenceLine(e))
		}
	}

	if len(r.Observations) > 0 {
		b.WriteString("\n## Observations\n\n")
		for _, o := range r.Observations {
			fmt.Fprintf(&b, "- %s\n", o)
		}
	}

	if len(r.Checks) > 0 {
		b.WriteString("\n## Checks\n\n")
		for _, status := range []string{analysis.CheckSkipped, analysis.CheckRan, analysis.CheckNotApplicable} {
			for _, c := range r.Checks {
				if c.Status != status {
					continue
				}
				fmt.Fprintf(&b, "- **%s** (%s): %s\n", c.ID, strings.ReplaceAll(c.Status, "_", " "), c.Reason)
				if c.Hint != "" {
					fmt.Fprintf(&b, "  - Hint: %s\n", c.Hint)
				}
			}
		}
	}

	if len(r.Warnings) > 0 {
		b.WriteString("\n## Warnings\n\n")
		for _, x := range r.Warnings {
			fmt.Fprintf(&b, "- %s\n", x)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeCause(b *strings.Builder, c *analysis.Cause) {
	fmt.Fprintf(b, "## Cause: %s", c.Type)
	if c.Target != "" {
		fmt.Fprintf(b, " on %s", c.Target)
	}
	fmt.Fprintf(b, " (%s, confidence %s)\n\n", c.CandidateID, c.Confidence)
	if c.Location == nil {
		b.WriteString("- Location: unavailable (see the warnings)\n")
	} else {
		l := c.Location
		fmt.Fprintf(b, "- Location: %s, %s", l.Function, l.File)
		if l.Line > 0 {
			fmt.Fprintf(b, ":%d", l.Line)
		}
		fmt.Fprintf(b, " (level %d", l.Level)
		if l.Approximate {
			b.WriteString(", approximate")
		}
		if l.InLoop != nil {
			if *l.InLoop {
				b.WriteString(", in a loop")
			} else {
				b.WriteString(", no loop found")
			}
		}
		b.WriteString(")\n")
	}
	if c.Commit == nil {
		b.WriteString("- Commit: none correlated (see the commit_correlation check)\n")
	} else {
		fmt.Fprintf(b, "- Commit: %s (touches the %s)\n", short(c.Commit.SHA), c.Commit.Touch)
	}
}

func locationSuffix(l *analysis.Location) string {
	if l == nil {
		return ""
	}
	if l.Line > 0 {
		return fmt.Sprintf(", at %s (%s:%d)", l.Function, l.File, l.Line)
	}
	return fmt.Sprintf(", at %s (%s)", l.Function, l.File)
}

func evidenceLine(e any) string {
	switch v := e.(type) {
	case analysis.RepeatedQueryEvidence:
		return fmt.Sprintf("%s repeated query `%s`: %d times per parent (median), %d rows each, +1 matched in %.0f%%, in %d of %d traces",
			v.ID, v.Query, v.GroupSizeMedian, v.RowsMedian, v.PlusOneMatchedRatio*100, v.TracesAffected, v.TracesTotal)
	case analysis.QueryPlanEvidence:
		return fmt.Sprintf("%s query plan of `%s`: %s on %s (~%.0f rows) returning ~%.0f rows (selectivity %.4f), filter %s, in %d of %d traces",
			v.ID, v.Query, v.NodeType, v.Table, v.RelTuples, v.PlanRows, v.Selectivity, v.Filter, v.TracesAffected, v.TracesTotal)
	case analysis.StackTouchEvidence:
		return fmt.Sprintf("%s commit %s changed %s (%s)", v.ID, short(v.Commit), v.Function, v.Strength)
	case analysis.TimeBreakdownEvidence:
		return breakdownLines(v)
	case analysis.SymptomStatsEvidence:
		return fmt.Sprintf("%s version %s: %d traces, median %.1f ms, median %d query spans",
			v.ID, short(v.Version), v.Traces, v.DurationMsMedian, v.DBQuerySpansMedian)
	}
	return fmt.Sprintf("%v", e)
}

// short abbreviates a SHA for reading; any other version is kept as it is.
func short(v string) string {
	if len(v) == 40 {
		return v[:12]
	}
	return v
}

// maxOperations is how many operations of a dependency the text lists; the JSON has all of them.
const maxOperations = 3

// breakdownLines writes the time breakdown as one line and a nested line per dependency.
func breakdownLines(v analysis.TimeBreakdownEvidence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s time breakdown: mean per trace %.1f → %.1f ms (%+.1f ms; %d and %d traces, without the slowest 5%%)",
		v.ID, v.BaselineMeanMs, v.BadMeanMs, v.BadMeanMs-v.BaselineMeanMs, v.BaselineMeanTraces, v.BadMeanTraces)
	for _, d := range v.Dependencies {
		fmt.Fprintf(&b, "\n  - %s: %.1f → %.1f ms per trace, explains %s, presence %.0f%% → %.0f%%",
			d.Dependency, d.Baseline.MeanMs, d.Bad.MeanMs, percent(d.Explained), d.Baseline.Presence*100, d.Bad.Presence*100)
		if d.Kind != "own" {
			fmt.Fprintf(&b, ", %.1f → %.1f calls per trace, %.1f → %.1f ms per call",
				d.Baseline.CallsPerTrace, d.Bad.CallsPerTrace, d.Baseline.MsPerCall, d.Bad.MsPerCall)
		}
		if len(d.Operations) > 0 {
			var ops []string
			for _, op := range d.Operations[:min(len(d.Operations), maxOperations)] {
				ops = append(ops, fmt.Sprintf("`%s` (%.0f%% → %.0f%%)", op.Operation, op.BaselinePresence*100, op.BadPresence*100))
			}
			fmt.Fprintf(&b, "; operations: %s", strings.Join(ops, ", "))
			if n := len(d.Operations) - maxOperations; n > 0 {
				fmt.Fprintf(&b, " and %d more", n)
			}
		}
	}
	return b.String()
}

// percent rounds a fraction to a whole percent without printing "-0%" for a tiny negative one.
func percent(f float64) string {
	p := math.Round(f * 100)
	if p == 0 {
		p = 0 // drops the sign of a negative zero
	}
	return fmt.Sprintf("%.0f%%", p)
}
