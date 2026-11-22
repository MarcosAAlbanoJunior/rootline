// Expected API (package correlation; stdlib only):
//
//	type Frame struct { Function, File string; Line int } // File is module-relative
//	type Ports interface {
//	    RevList(base, bad string) ([]string, error)                         // oldest first
//	    ChangedLines(commit string) (map[string][][2]int, error)            // repo path -> new-side [start,end]
//	    FuncRange(sha, file, function string) (start, end int, found bool, err error) // file is a repo path
//	    ResolvePath(sha, modulePath, relFile string) (string, error)
//	}
//	type Input struct {
//	    BaseSHA, BadSHA  string
//	    ModulePath       string
//	    Stack            []Frame // innermost first
//	    LocationFunction string  // function of the candidate location
//	}
//	type Touch struct { Commit, Function, Strength string } // Strength: "location" | "stack"
//	// One Touch per commit that touches the stack (strongest strength wins), in commit order.
//	func StackTouches(p Ports, in Input) ([]Touch, error)
package correlation_test

import (
	"reflect"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/correlation"
)

const (
	svcFn  = "example.com/demo-app/internal/orders.(*Service).List"
	repoFn = "example.com/demo-app/internal/orders.(*Repo).ItemsByOrder"
)

type fakePorts struct {
	commits   []string
	changes   map[string]map[string][][2]int
	ranges    map[string][2]int // file+"|"+function
	rangeSHAs []string
}

func (f *fakePorts) RevList(base, bad string) ([]string, error) { return f.commits, nil }
func (f *fakePorts) ChangedLines(c string) (map[string][][2]int, error) {
	return f.changes[c], nil
}
func (f *fakePorts) FuncRange(sha, file, fn string) (int, int, bool, error) {
	f.rangeSHAs = append(f.rangeSHAs, sha)
	r, ok := f.ranges[file+"|"+fn]
	return r[0], r[1], ok, nil
}
func (f *fakePorts) ResolvePath(_, _, rel string) (string, error) { return "demo-app/" + rel, nil }

func ports(changes map[string]map[string][][2]int) *fakePorts {
	var commits []string
	for c := range changes {
		commits = append(commits, c)
	}
	if len(commits) == 2 { // deterministic order for the two-commit test
		commits = []string{"c1", "c2"}
	}
	return &fakePorts{
		commits: commits,
		changes: changes,
		ranges: map[string][2]int{
			"demo-app/internal/orders/service.go|" + svcFn:     {35, 50},
			"demo-app/internal/orders/repository.go|" + repoFn: {25, 40},
		},
	}
}

var input = correlation.Input{
	BaseSHA:    "base123",
	BadSHA:     "bad1234",
	ModulePath: "example.com/demo-app",
	Stack: []correlation.Frame{
		{Function: repoFn, File: "internal/orders/repository.go", Line: 30},
		{Function: svcFn, File: "internal/orders/service.go", Line: 42},
	},
	LocationFunction: svcFn,
}

func TestAC33_TouchOnLocationFunction(t *testing.T) {
	p := ports(map[string]map[string][][2]int{"c1": {"demo-app/internal/orders/service.go": {{40, 44}}}})
	got, err := correlation.StackTouches(p, input)
	if err != nil {
		t.Fatal(err)
	}
	want := []correlation.Touch{{Commit: "c1", Function: svcFn, Strength: "location"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for _, s := range p.rangeSHAs {
		if s != "c1" {
			t.Errorf("function range must be read at the commit SHA, got %q", s)
		}
	}
}

func TestAC33_TouchOnOtherStackFrame(t *testing.T) {
	p := ports(map[string]map[string][][2]int{"c1": {"demo-app/internal/orders/repository.go": {{31, 31}}}})
	got, err := correlation.StackTouches(p, input)
	if err != nil {
		t.Fatal(err)
	}
	want := []correlation.Touch{{Commit: "c1", Function: repoFn, Strength: "stack"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAC33_NoTouchOutsideTheStack(t *testing.T) {
	tests := []struct {
		name    string
		changes map[string][][2]int
	}{
		{"file outside the stack", map[string][][2]int{"demo-app/README.md": {{1, 10}}}},
		{"same file, lines outside the function", map[string][][2]int{"demo-app/internal/orders/service.go": {{100, 120}, {1, 5}}}},
		{"adjacent line just after the function", map[string][][2]int{"demo-app/internal/orders/service.go": {{51, 52}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := ports(map[string]map[string][][2]int{"c1": tc.changes})
			got, err := correlation.StackTouches(p, input)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 0 {
				t.Fatalf("want no touch, got %+v", got)
			}
		})
	}
}

func TestAC33_RangeBoundariesAreInclusive(t *testing.T) {
	p := ports(map[string]map[string][][2]int{"c1": {"demo-app/internal/orders/service.go": {{50, 60}}}})
	got, _ := correlation.StackTouches(p, input)
	if len(got) != 1 {
		t.Fatalf("a change overlapping the last function line must touch, got %+v", got)
	}
}

func TestAC33_StrongestStrengthWinsPerCommitAndOrderIsKept(t *testing.T) {
	p := ports(map[string]map[string][][2]int{
		"c1": {"demo-app/internal/orders/repository.go": {{30, 30}}},
		"c2": {
			"demo-app/internal/orders/repository.go": {{30, 30}},
			"demo-app/internal/orders/service.go":    {{42, 42}},
		},
	})
	got, err := correlation.StackTouches(p, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Commit != "c1" || got[0].Strength != "stack" || got[1].Commit != "c2" || got[1].Strength != "location" {
		t.Fatalf("got %+v", got)
	}
}
