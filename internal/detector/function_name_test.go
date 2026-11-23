package detector_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
)

// ADR-0019: the location names the declared function, not the synthetic names the runtime gives a
// range-over-func body (-rangeN) or a closure (.funcN, .funcN.M); file and line stay those of the
// frame. Package-level closures, -fm, gowrapN, deferwrapN and generics keep the frame name.
func TestLocate_FunctionIsTheDeclaredOne(t *testing.T) {
	tests := []struct{ frame, want string }{
		{"example.com/m/pkg.(*Service).Lines-range1", "example.com/m/pkg.(*Service).Lines"},
		{"example.com/m/pkg.(*Service).Lines-range1-range2", "example.com/m/pkg.(*Service).Lines"},
		{"example.com/m/pkg.Walk-range12.func1", "example.com/m/pkg.Walk"},
		{"example.com/m/pkg.(*Service).List.func2", "example.com/m/pkg.(*Service).List"},
		{"example.com/m/pkg.(*Service).Fetch.func1", "example.com/m/pkg.(*Service).Fetch"},
		{"example.com/m/pkg.(*Service).Fetch.func1.2", "example.com/m/pkg.(*Service).Fetch"},
		{"example.com/m/pkg.(*Service).Fetch.func3.1.1", "example.com/m/pkg.(*Service).Fetch"},
		{"example.com/m/pkg.Run-range1.func2", "example.com/m/pkg.Run"},
		{"example.com/m/pkg.glob..func1", "example.com/m/pkg.glob..func1"},
		{"example.com/m/pkg.glob..func1.2", "example.com/m/pkg.glob..func1.2"},
		{"example.com/m/pkg.init.func1", "example.com/m/pkg.init.func1"},
		{"example.com/m/pkg.init.0.func1", "example.com/m/pkg.init.0.func1"},
		{"gopkg.in/yaml.v3.glob..func1", "gopkg.in/yaml.v3.glob..func1"},
		{"gopkg.in/yaml.v3.init.func2", "gopkg.in/yaml.v3.init.func2"},
		{"gopkg.in/yaml.v3.(*Decoder).Decode.func1", "gopkg.in/yaml.v3.(*Decoder).Decode"},
		{"example.com/m/pkg.(*T).M-fm", "example.com/m/pkg.(*T).M-fm"},
		{"example.com/m/pkg.Run.gowrap1", "example.com/m/pkg.Run.gowrap1"},
		{"example.com/m/pkg.Run.deferwrap1", "example.com/m/pkg.Run.deferwrap1"},
		{"example.com/m/pkg.Map[...]", "example.com/m/pkg.Map[...]"},
		{"example.com/m/pkg.(*Service).Arrange", "example.com/m/pkg.(*Service).Arrange"},
		{"example.com/m/pkg.(*Service).Refunc1", "example.com/m/pkg.(*Service).Refunc1"},
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

// A goroutine launched in a loop (errgroup) has no frame of the enclosing function: the stack starts
// at the closure, whose line is inside the loop. The location keeps that frame's file and line.
func TestLocate_ClosureLaunchedInALoopKeepsTheFrameFileAndLine(t *testing.T) {
	const declared = modPath + "/internal/summary.(*Service).Summary"
	env := fakeEnv{loops: map[string]bool{"demo-app/internal/summary/service.go:51": true}}
	spans := spansWithStack(
		modPath+"/internal/summary.(*Repo).ItemsByOrder internal/summary/repository.go:20",
		declared+".func1 internal/summary/service.go:51",
	)
	loc, reason := detector.Locate(env, "abc1234", spans)
	if loc == nil {
		t.Fatalf("location is nil (reason %q)", reason)
	}
	if loc.Function != declared || loc.File != "demo-app/internal/summary/service.go" || loc.Line != 51 || !loc.InLoop {
		t.Fatalf("got %+v, want %s at demo-app/internal/summary/service.go:51 in a loop", *loc, declared)
	}
}
