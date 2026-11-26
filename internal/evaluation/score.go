// Package evaluation scores analysis Results against the answer key of a recording and keeps the
// scoreboard (ADR-0026). It depends only on the standard library.
package evaluation

import "fmt"

// CauseInconclusive is the cause type of a scenario with no cause the Rootline can see: the
// expected Result is "inconclusive" (ADR-0004).
const CauseInconclusive = "inconclusive"

// Key is the answer key of a scenario. CommitLabel "" means no commit is expected. Target is the
// cause target of the type ("" when the type has none, as in an N+1; ADR-0026).
type Key struct {
	CauseType, Function, Target, File string
	Line                              int
	CommitLabel                       string
	Distractors                       []Distractor
}

// Distractor is code of the scenario that looks like a cause and is not, with its line at the bad
// commit (ADR-0022).
type Distractor struct {
	Function, File string
	Line           int
}

// CandidateLocation is the location of one candidate of the Result. Level 0 means no location;
// level 2 has no line.
type CandidateLocation struct {
	Level          int
	Function, File string
	Line           int
}

// matches applies the rule by level: file and line at level 1, function at level 2.
func (c CandidateLocation) matches(function, file string, line int) bool {
	switch c.Level {
	case 1:
		return c.File == file && c.Line == line
	case 2:
		return c.Function == function
	}
	return false
}

// Found is what a Result says, reduced to the fields that are scored. Candidates has the location
// of every candidate, the cause included.
type Found struct {
	HasCause    bool
	CauseType   string
	Function    string
	Target      string // the cause target from a structured field of the Result
	HasLocation bool   // level-1 location present
	Level       int    // location level of the cause: 0 = absent, 1 or 2
	File        string
	Line        int
	CommitSHA   string // "" = the Result has no commit
	Candidates  []CandidateLocation
}

// Outcome is the result of scoring one Result. LineLevel is the location level of the cause
// (0 = absent, 1, 2); it feeds the distribution, not the Line hit (ADR-0014). In a scenario without
// cause, the Line hit is having no cause, so it comes with level 0. FalsePositive is nil
// in a scenario without distractors, and true when some candidate matches a distractor (ADR-0022).
type Outcome struct {
	Cause         bool  `json:"cause"`
	Line          bool  `json:"line"`
	Commit        bool  `json:"commit"`
	Strict        bool  `json:"strict"`
	LineLevel     int   `json:"line_level"`
	FalsePositive *bool `json:"false_positive,omitempty"`
	// Hallucination is set in a scenario without cause or without commit: whether the Result
	// invented one. UnwarrantedAbstention is set in a scenario with a cause: whether the Result was
	// inconclusive. They are reported side by side (ADR-0026).
	Hallucination         *bool `json:"hallucination,omitempty"`
	UnwarrantedAbstention *bool `json:"unwarranted_abstention,omitempty"`
}

// ScoreOne scores one Result. shaToLabel is the manifest mapping from commit SHA to label.
func ScoreOne(key Key, found Found, shaToLabel map[string]string) Outcome {
	o := scoreAnswer(key, found, shaToLabel)
	hasCause := key.CauseType != CauseInconclusive
	if !hasCause || key.CommitLabel == "" {
		invented := (!hasCause && found.HasCause) || (key.CommitLabel == "" && found.CommitSHA != "")
		o.Hallucination = &invented
	}
	if hasCause {
		abstained := !found.HasCause
		o.UnwarrantedAbstention = &abstained
	}
	return o
}

// scoreAnswer scores cause, line, commit, strict and the distractors.
func scoreAnswer(key Key, found Found, shaToLabel map[string]string) Outcome {
	var o Outcome
	if key.CauseType == CauseInconclusive {
		// Nothing to point at: the hit is a Result without a cause, and so without a location.
		o.Cause, o.Line = !found.HasCause, !found.HasCause
		o.Commit = found.CommitSHA == ""
		o.Strict = o.Cause && o.Line && o.Commit
		o.FalsePositive = falsePositive(key, found)
		return o
	}
	// ADR-0026: the Cause is the type, the function and the target of the type; an N+1 has no
	// target, so both sides are empty.
	o.Cause = found.HasCause && found.CauseType == key.CauseType && found.Function == key.Function &&
		found.Target == key.Target
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
	o.FalsePositive = falsePositive(key, found)
	return o
}

// falsePositive is nil in a scenario without distractors (ADR-0022).
func falsePositive(key Key, found Found) *bool {
	if len(key.Distractors) == 0 {
		return nil
	}
	fp := hitsDistractor(key, found.Candidates)
	return &fp
}

// hitsDistractor reports whether a candidate that does not match the answer key matches a
// distractor. Causes and distractors may share a function: at level 1 the line tells them apart;
// at level 2 a candidate with that function matches the answer key and never counts (ADR-0022).
func hitsDistractor(key Key, candidates []CandidateLocation) bool {
	for _, c := range candidates {
		if c.matches(key.Function, key.File, key.Line) {
			continue
		}
		for _, d := range key.Distractors {
			if c.matches(d.Function, d.File, d.Line) {
				return true
			}
		}
	}
	return false
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
// DetectorFalsePositive counts, among the scenarios with a distractor, those with a false positive;
// it is nil when no scenario has a distractor (ADR-0022).
type Score struct {
	Cause                 Metric     `json:"cause"`
	Line                  Metric     `json:"line"`
	Commit                Metric     `json:"commit"`
	Strict                Metric     `json:"strict"`
	LineLevels            LineLevels `json:"line_levels"`
	DetectorFalsePositive *Metric    `json:"detector_false_positive,omitempty"`
	// Hallucination counts, among the scenarios without cause or commit, those where the Result
	// invented one; UnwarrantedAbstention, among those with a cause, the inconclusive Results.
	// Less is better in both (ADR-0026).
	Hallucination         Metric             `json:"hallucination"`
	UnwarrantedAbstention Metric             `json:"unwarranted_abstention"`
	Scenarios             map[string]Outcome `json:"scenarios,omitempty"`
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
	if o.FalsePositive != nil {
		if s.DetectorFalsePositive == nil {
			s.DetectorFalsePositive = &Metric{}
		}
		add(s.DetectorFalsePositive, *o.FalsePositive)
	}
	if o.Hallucination != nil {
		add(&s.Hallucination, *o.Hallucination)
	}
	if o.UnwarrantedAbstention != nil {
		add(&s.UnwarrantedAbstention, *o.UnwarrantedAbstention)
	}
	switch o.LineLevel {
	case 1:
		s.LineLevels.Level1++
	case 2:
		s.LineLevels.Level2++
	default:
		s.LineLevels.None++
	}
}
