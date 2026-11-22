// Package trace is the domain model of recorded traces: spans, entry operations and span trees.
// It depends only on the standard library.
package trace

import "time"

// Span kinds, as in OTLP.
const (
	KindInternal = 1
	KindServer   = 2
	KindClient   = 3
)

// Span is one recorded span with the resource attributes the analysis needs.
type Span struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Name         string
	Kind         int    // OTLP span kind
	Scope        string // instrumentation scope name
	Start, End   time.Time
	Attrs        map[string]any // values: string, int64, bool, []string
	Service      string         // resource attribute service.name
	Version      string         // resource attribute service.version
}

// String returns the string attribute key, if present.
func (s Span) String(key string) (string, bool) {
	v, ok := s.Attrs[key].(string)
	return v, ok
}

// Int returns the integer attribute key, if present.
func (s Span) Int(key string) (int64, bool) {
	v, ok := s.Attrs[key].(int64)
	return v, ok
}

// Strings returns the string-list attribute key, if present.
func (s Span) Strings(key string) ([]string, bool) {
	v, ok := s.Attrs[key].([]string)
	return v, ok
}
