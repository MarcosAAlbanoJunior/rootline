// Package evaluation scores analysis Results against the answer key of a recording and keeps the
// scoreboard (ADR-0009). It depends only on the standard library.
package evaluation

import "fmt"

// Key is the answer key of a scenario. CommitLabel "" means no commit is expected.
type Key struct {
	CauseType, Function, File string
	Line                      int
	CommitLabel               string
}

// Found is what a Result says, reduced to the fields that are scored.
type Found struct {
	HasCause    bool
	CauseType   string
	Function    string
	HasLocation bool // level-1 location present
	File        string
	Line        int
	CommitSHA   string // "" = the Result has no commit
}

// Outcome is the result of scoring one Result.
type Outcome struct{ Cause, Line, Commit, Strict bool }

// ScoreOne scores one Result. shaToLabel is the manifest mapping from commit SHA to label.
func ScoreOne(key Key, found Found, shaToLabel map[string]string) Outcome {
	var o Outcome
	o.Cause = found.HasCause && found.CauseType == key.CauseType && found.Function == key.Function
	o.Line = found.HasCause && found.HasLocation && found.File == key.File && found.Line == key.Line
	switch found.CommitSHA {
	case "":
		o.Commit = key.CommitLabel == ""
	default:
		label, known := shaToLabel[found.CommitSHA]
		o.Commit = known && label == key.CommitLabel
	}
	o.Strict = o.Cause && o.Line && o.Commit
	return o
}

// Metric is hits out of total.
type Metric struct {
	Hits  int `json:"hits"`
	Total int `json:"total"`
}

// String formats as "hits/total, pct%" with the integer (floor) percentage.
func (m Metric) String() string {
	pct := 0
	if m.Total > 0 {
		pct = m.Hits * 100 / m.Total
	}
	return fmt.Sprintf("%d/%d, %d%%", m.Hits, m.Total, pct)
}

// Score accumulates the four metrics.
type Score struct {
	Cause  Metric `json:"cause"`
	Line   Metric `json:"line"`
	Commit Metric `json:"commit"`
	Strict Metric `json:"strict"`
}

// Add accumulates one outcome.
func (s *Score) Add(o Outcome) {
	add := func(m *Metric, hit bool) {
		m.Total++
		if hit {
			m.Hits++
		}
	}
	add(&s.Cause, o.Cause)
	add(&s.Line, o.Line)
	add(&s.Commit, o.Commit)
	add(&s.Strict, o.Strict)
}
