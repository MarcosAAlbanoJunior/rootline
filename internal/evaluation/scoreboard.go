package evaluation

import (
	"encoding/json"
	"io"
	"sort"
)

// Scoreboard maps an analysis mode (e.g. "detectors_only") to its score.
type Scoreboard map[string]Score

// ReadScoreboard decodes a scoreboard from JSON.
func ReadScoreboard(r io.Reader) (Scoreboard, error) {
	var sb Scoreboard
	if err := json.NewDecoder(r).Decode(&sb); err != nil {
		return nil, err
	}
	return sb, nil
}

// WriteScoreboard encodes a scoreboard as indented JSON.
func WriteScoreboard(w io.Writer, sb Scoreboard) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(sb)
}

// Regressions lists "<mode>.<metric>" for each metric whose hits dropped while the total stayed
// the same as in the baseline. Metrics with different denominators are not compared. A mode that
// is in the baseline but missing from the current run regresses all of its metrics.
func Regressions(baseline, current Scoreboard) []string {
	modes := make([]string, 0, len(baseline))
	for m := range baseline {
		modes = append(modes, m)
	}
	sort.Strings(modes)
	var out []string
	for _, mode := range modes {
		base := baseline[mode]
		cur, ok := current[mode]
		if !ok {
			for _, metric := range []string{"cause", "line", "commit", "strict"} {
				out = append(out, mode+"."+metric)
			}
			continue
		}
		for _, m := range []struct {
			name      string
			base, cur Metric
		}{
			{"cause", base.Cause, cur.Cause},
			{"line", base.Line, cur.Line},
			{"commit", base.Commit, cur.Commit},
			{"strict", base.Strict, cur.Strict},
		} {
			if m.base.Total == m.cur.Total && m.cur.Hits < m.base.Hits {
				out = append(out, mode+"."+m.name)
			}
		}
	}
	return out
}
