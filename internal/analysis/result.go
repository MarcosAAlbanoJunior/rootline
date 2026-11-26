package analysis

import "github.com/MarcosAAlbanoJunior/rootline/internal/detector"

// Result constants.
const (
	SchemaVersion = "1"
	ModeDetectors = "detectors_only"

	StatusCauseFound   = "cause_found"
	StatusInconclusive = "inconclusive"

	TypeNPlusOne     = detector.TypeNPlusOne
	TypeMissingIndex = detector.TypeMissingIndex
	TypeAddedWork    = detector.TypeAddedWork
	TypeSlowCall     = detector.TypeSlowCall
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
	// Checks says what the analysis verified and why the Result is what it is (ADR-0027). Warnings
	// are only about the Result itself, such as an approximate location.
	Checks   []Check  `json:"checks"`
	Warnings []string `json:"warnings"`
}

// Check statuses.
const (
	CheckRan           = detector.CheckRan
	CheckSkipped       = detector.CheckSkipped
	CheckNotApplicable = detector.CheckNotApplicable
)

// Check ids that are not a detector's.
const (
	CheckCommitCorrelation = "commit_correlation"
)

// Check is one detector or precondition evaluated by the analysis (Verificação): ID is stable,
// Status is ran, skipped or not_applicable, Reason is the observed fact and Hint what to do next.
type Check struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason"`
	Hint   string `json:"hint"`
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

// TimeBreakdownEvidence is the time breakdown of the operation (ADR-0031): the growth of the mean
// per trace split between the dependencies and the own time, the one that grew most first. The
// means leave out the traces at the ceiling and the slowest 5% of each version (MeanTraces of
// Traces); the presences count all the Traces.
type TimeBreakdownEvidence struct {
	ID                 string           `json:"id"`
	Kind               string           `json:"kind"`
	BaselineMeanMs     float64          `json:"baseline_mean_ms"`
	BadMeanMs          float64          `json:"bad_mean_ms"`
	BaselineTraces     int              `json:"baseline_traces"`
	BadTraces          int              `json:"bad_traces"`
	BaselineMeanTraces int              `json:"baseline_mean_traces"`
	BadMeanTraces      int              `json:"bad_mean_traces"`
	Dependencies       []DependencyTime `json:"dependencies"`
}

// DependencyTime is one row of the time breakdown. Explained is the share of the growth of the mean
// that the dependency accounts for, capped at 1.
type DependencyTime struct {
	Dependency string              `json:"dependency"`
	Kind       string              `json:"kind"`
	Baseline   VersionTime         `json:"baseline"`
	Bad        VersionTime         `json:"bad"`
	Explained  float64             `json:"explained"`
	Operations []OperationPresence `json:"operations"`
}

// VersionTime is what one dependency took in one version.
type VersionTime struct {
	MeanMs        float64 `json:"mean_ms"`
	Presence      float64 `json:"presence"`
	CallsPerTrace float64 `json:"calls_per_trace"`
	MsPerCall     float64 `json:"ms_per_call"`
}

// OperationPresence is an operation inside a dependency and the fraction of the traces where it
// appears in each version.
type OperationPresence struct {
	Operation        string  `json:"operation"`
	BaselinePresence float64 `json:"baseline_presence"`
	BadPresence      float64 `json:"bad_presence"`
}
