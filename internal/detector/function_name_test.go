package detector_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
)

// ADR-0016: the location names the declared function, not the range-over-func body; file and line
// stay those of the frame.
func TestLocate_FunctionDropsTheRangeOverFuncSuffix(t *testing.T) {
	tests := []struct{ frame, want string }{
		{"example.com/m/pkg.(*Service).Lines-range1", "example.com/m/pkg.(*Service).Lines"},
		{"example.com/m/pkg.(*Service).Lines-range1-range2", "example.com/m/pkg.(*Service).Lines"},
		{"example.com/m/pkg.Walk-range12.func1", "example.com/m/pkg.Walk.func1"},
		{"example.com/m/pkg.(*Service).List.func2", "example.com/m/pkg.(*Service).List.func2"},
		{"example.com/m/pkg.Map[...]", "example.com/m/pkg.Map[...]"},
		{"example.com/m/pkg.(*Service).Arrange", "example.com/m/pkg.(*Service).Arrange"},
	}
	env := fakeEnv{loops: map[string]bool{"demo-app/internal/pkg/service.go:17": true}}
	for _, tc := range tests {
		t.Run(tc.frame, func(t *testing.T) {
			loc, reason := detector.Locate(env, "abc1234", spansWithStack(tc.frame+" internal/pkg/service.go:17"))
			if loc == nil {
				t.Fatalf("location is nil (reason %q)", reason)
			}
			if loc.Function != tc.want || loc.File != "demo-app/internal/pkg/service.go" || loc.Line != 17 {
				t.Fatalf("got %+v, want Function %q at demo-app/internal/pkg/service.go:17", *loc, tc.want)
			}
		})
	}
}
