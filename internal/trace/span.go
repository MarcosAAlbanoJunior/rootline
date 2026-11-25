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

// Span status codes, as in OTLP.
const (
	StatusUnset = 0
	StatusOK    = 1
	StatusError = 2
)

// Span is one recorded span with the resource attributes the analysis needs.
type Span struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Name         string
	Kind         int    // OTLP span kind
	StatusCode   int    // OTLP status code
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

// Failed reports whether the span ended in error: an error status, or an HTTP 5xx response for
// instrumentations that do not set the status. A 4xx is the client's error, not the span's.
func (s Span) Failed() bool {
	if s.StatusCode == StatusError {
		return true
	}
	for _, k := range []string{"http.response.status_code", "http.status_code"} {
		if code, ok := s.Int(k); ok && code >= 500 {
			return true
		}
	}
	return false
}
