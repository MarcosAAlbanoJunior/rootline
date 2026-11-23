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
	Level       int  // location level of the cause: 0 = absent, 1 or 2
	File        string
	Line        int
	CommitSHA   string // "" = the Result has no commit
}

// Outcome is the result of scoring one Result. LineLevel is the location level of the cause
// (0 = absent, 1, 2); it feeds the distribution, not the Line hit (ADR-0014).
type Outcome struct {
	Cause     bool `json:"cause"`
	Line      bool `json:"line"`
	Commit    bool `json:"commit"`
	Strict    bool `json:"strict"`
	LineLevel int  `json:"line_level"`
}

// ScoreOne scores one Result. shaToLabel is the manifest mapping from commit SHA to label.
func ScoreOne(key Key, found Found, shaToLabel map[string]string) Outcome {
	var o Outcome
	o.Cause = found.HasCause && found.CauseType == key.CauseType && found.Function == key.Function
	o.Line = found.HasCause && found.HasLocation && found.File == key.File && found.Line == key.Line
	if found.HasCause {
		o.LineLevel = found.Level
	}
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

// LineLevels is the distribution of location levels behind the Line metric (ADR-0009, ADR-0014).
// Its counts add up to line.total.
type LineLevels struct {
	Level1 int `json:"level_1"`
	Level2 int `json:"level_2"`
	None   int `json:"none"`
}

// Score accumulates the four metrics and the distribution of location levels. Scenarios keeps the
// outcome of each scenario by name; with more than one scenario the aggregate alone masks a swap
// (one improves, another drops, same total), so regressions are found per scenario.
type Score struct {
	Cause      Metric             `json:"cause"`
	Line       Metric             `json:"line"`
	Commit     Metric             `json:"commit"`
	Strict     Metric             `json:"strict"`
	LineLevels LineLevels         `json:"line_levels"`
	Scenarios  map[string]Outcome `json:"scenarios,omitempty"`
}

// ScoreScenarios builds a Score whose aggregate is derived from the outcome of each scenario.
func ScoreScenarios(outcomes map[string]Outcome) Score {
	s := Score{Scenarios: make(map[string]Outcome, len(outcomes))}
	for name, o := range outcomes {
		s.Scenarios[name] = o
		s.Add(o)
	}
	return s
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
	switch o.LineLevel {
	case 1:
		s.LineLevels.Level1++
	case 2:
		s.LineLevels.Level2++
	default:
		s.LineLevels.None++
	}
}
