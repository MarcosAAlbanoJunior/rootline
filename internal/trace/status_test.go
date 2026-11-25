package trace_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func TestFailed_ErrorStatusOrServerErrorResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		span trace.Span
		want bool
	}{
		{"unset", trace.Span{}, false},
		{"error status", trace.Span{StatusCode: trace.StatusError}, true},
		{"5xx with unset status", trace.Span{Attrs: map[string]any{"http.response.status_code": int64(503)}}, true},
		{"5xx under the old attribute", trace.Span{Attrs: map[string]any{"http.status_code": int64(500)}}, true},
		{"4xx is the client's error", trace.Span{Attrs: map[string]any{"http.response.status_code": int64(404)}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.span.Failed(); got != tc.want {
				t.Errorf("Failed() = %v, want %v", got, tc.want)
			}
		})
	}
}
