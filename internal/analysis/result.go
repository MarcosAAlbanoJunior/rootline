package analysis

import "github.com/MarcosAAlbanoJunior/rootline/internal/detector"

// Result constants.
const (
	SchemaVersion = "0"
	ModeDetectors = "detectors_only"

	StatusCauseFound   = "cause_found"
	StatusInconclusive = "inconclusive"

	TypeNPlusOne     = detector.TypeNPlusOne
	TypeMissingIndex = detector.TypeMissingIndex
)

// Result is the analysis output. Its JSON form is the output contract of the CLI (keys in English,
// snake_case).
type Result struct {
	SchemaVersion string      `json:"schema_version"`
	Mode          string      `json:"mode"`
	Symptom       SymptomInfo `json:"symptom"`
	Status        string      `json:"status"`
	Cause         *Cause      `json:"cause"`
	Candidates    []Candidate `json:"candidates"`
	Evidence      []any       `json:"evidence"`
	Observations  []string    `json:"observations"`
	Warnings      []string    `json:"warnings"`
}

// SymptomInfo echoes the analyzed symptom.
type SymptomInfo struct {
	Service         string        `json:"service"`
	Operation       OperationInfo `json:"operation"`
	BadVersion      string        `json:"bad_version"`
	BaselineVersion *string       `json:"baseline_version"`
}

// OperationInfo is the entry operation.
type OperationInfo struct {
	Method      string `json:"method"`
	Route       string `json:"route"`
	RouteSource string `json:"route_source"`
}

// Location is a code location. Level 1 comes from the app stack and has a line. Level 2 (route to
// handler) is approximate: a function with no line, marked Approximate.
type Location struct {
	Level       int    `json:"level"`
	Function    string `json:"function"`
	File        string `json:"file"`
	Line        int    `json:"line,omitempty"`
	InLoop      *bool  `json:"in_loop,omitempty"` // nil when nothing was checked: level 2, or a strategy without the loop rule
	Approximate bool   `json:"approximate,omitempty"`
}

// CommitRef is the commit correlated with a cause.
type CommitRef struct {
	SHA   string `json:"sha"`
	Touch string `json:"touch"`
}

// Cause is copied from the highest-scored candidate.
type Cause struct {
	CandidateID string     `json:"candidate_id"`
	Type        string     `json:"type"`
	Target      string     `json:"target,omitempty"` // the cause target, when the type has one (ADR-0026)
	Confidence  string     `json:"confidence"`
	Location    *Location  `json:"location"`
	Commit      *CommitRef `json:"commit"`
}

// Candidate is a root-cause candidate.
type Candidate struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Target     string    `json:"target,omitempty"` // the cause target, when the type has one (ADR-0026)
	Score      float64   `json:"score"`
	Confidence string    `json:"confidence"`
	Location   *Location `json:"location"`
	Evidence   []string  `json:"evidence"`
}

// RepeatedQueryEvidence describes the repeated query behind an N+1 candidate.
type RepeatedQueryEvidence struct {
	ID                  string  `json:"id"`
	Kind                string  `json:"kind"`
	Query               string  `json:"query"`
	GroupSizeMedian     int     `json:"group_size_median"`
	RowsMedian          int     `json:"rows_median"`
	PlusOneMatchedRatio float64 `json:"plus_one_matched_ratio"`
	TracesAffected      int     `json:"traces_affected"`
	TracesTotal         int     `json:"traces_total"`
	PhaseInferred       bool    `json:"phase_inferred"`
	SampleTraceID       string  `json:"sample_trace_id"`
	SampleParentSpanID  string  `json:"sample_parent_span_id"`
	// CollapsedParentName and CollapsedParentCount appear only when the group came from collapsing
	// repeated wrapper spans into the effective parent (ADR-0020).
	CollapsedParentName  string `json:"collapsed_parent_name,omitempty"`
	CollapsedParentCount int    `json:"collapsed_parent_count,omitempty"`
}

// QueryPlanEvidence is the plan node behind a missing-index candidate: a sequential scan through a
// selective filter on a large table (ADR-0023). All numbers are planner estimates.
type QueryPlanEvidence struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Query       string  `json:"query"`
	NodeType    string  `json:"node_type"`
	Table       string  `json:"table"`
	RelTuples   float64 `json:"reltuples"`
	PlanRows    float64 `json:"plan_rows"`
	Selectivity float64 `json:"selectivity"`
	Filter      string  `json:"filter"`
	// FilterColumns is a best effort over Filter, only "column = $n"; it is not part of the target.
	FilterColumns  []string `json:"filter_columns"`
	GenericPlan    bool     `json:"generic_plan"` // the plan was made for any parameter value
	TracesAffected int      `json:"traces_affected"`
	TracesTotal    int      `json:"traces_total"`
	SampleTraceID  string   `json:"sample_trace_id"`
}

// StackTouchEvidence is a commit that changed a function on the candidate's stack.
type StackTouchEvidence struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Commit   string `json:"commit"`
	Function string `json:"function"`
	Strength string `json:"strength"`
}

// SymptomStatsEvidence summarizes the analyzed traces of one version.
type SymptomStatsEvidence struct {
	ID                 string  `json:"id"`
	Kind               string  `json:"kind"`
	Version            string  `json:"version"`
	Traces             int     `json:"traces"`
	DurationMsMedian   float64 `json:"duration_ms_median"`
	DBQuerySpansMedian int     `json:"db_query_spans_median"`
}
