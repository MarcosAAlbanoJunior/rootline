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

// Regressions lists what dropped from the baseline to the current run. A mode that is in the
// baseline but missing from the current run regresses all of its metrics ("<mode>.<metric>").
//
// When the baseline mode has scenarios, they are compared one by one: each of cause, line, commit
// and strict that was true and became false gives "<mode>.<scenario>.<metric>", even if the
// aggregate did not change, and a scenario missing from the current run gives
// "<mode>.<scenario>". A scenario that is new in the current run and a change of line level only
// are not regressions.
//
// A baseline mode without scenarios (older scoreboard) uses the aggregate rule: a metric whose
// hits dropped while the total stayed the same. Metrics with different denominators are not
// compared.
func Regressions(baseline, current Scoreboard) []string {
	var out []string
	for _, mode := range sortedKeys(baseline) {
		base := baseline[mode]
		cur, ok := current[mode]
		switch {
		case !ok:
			for _, metric := range metricNames {
				out = append(out, mode+"."+metric)
			}
		case base.Scenarios != nil:
			out = append(out, scenarioRegressions(mode, base.Scenarios, cur.Scenarios)...)
		default:
			out = append(out, aggregateRegressions(mode, base, cur)...)
		}
	}
	return out
}

var metricNames = []string{"cause", "line", "commit", "strict"}

func scenarioRegressions(mode string, base, cur map[string]Outcome) []string {
	var out []string
	for _, name := range sortedKeys(base) {
		b := base[name]
		c, ok := cur[name]
		if !ok {
			out = append(out, mode+"."+name)
			continue
		}
		for i, pair := range [][2]bool{{b.Cause, c.Cause}, {b.Line, c.Line}, {b.Commit, c.Commit}, {b.Strict, c.Strict}} {
			if pair[0] && !pair[1] {
				out = append(out, mode+"."+name+"."+metricNames[i])
			}
		}
	}
	return out
}

func aggregateRegressions(mode string, base, cur Score) []string {
	var out []string
	for i, pair := range [][2]Metric{{base.Cause, cur.Cause}, {base.Line, cur.Line}, {base.Commit, cur.Commit}, {base.Strict, cur.Strict}} {
		if pair[0].Total == pair[1].Total && pair[1].Hits < pair[0].Hits {
			out = append(out, mode+"."+metricNames[i])
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
