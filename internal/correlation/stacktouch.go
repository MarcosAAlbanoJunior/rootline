// Package correlation links a candidate's code location to the commits that changed it. It
// depends only on the standard library.
package correlation

// Touch strengths.
const (
	StrengthLocation = "location"
	StrengthStack    = "stack"
)

// Frame is one frame of the candidate's stack. File is module-relative.
type Frame struct {
	Function, File string
	Line           int
}

// Ports is what the correlation needs from git and the code index.
type Ports interface {
	RevList(base, bad string) ([]string, error)                                   // oldest first
	ChangedLines(commit string) (map[string][][2]int, error)                      // repo path -> new-side [start,end]
	FuncRange(sha, file, function string) (start, end int, found bool, err error) // file is a repo path
	ResolvePath(sha, modulePath, relFile string) (string, error)
}

// Input describes the candidate and the version range to inspect.
type Input struct {
	BaseSHA, BadSHA  string
	ModulePath       string
	Stack            []Frame // innermost first
	LocationFunction string  // function of the candidate location
}

// Touch is a commit that changed lines inside the function of a stack frame.
type Touch struct {
	Commit, Function, Strength string // Strength: "location" | "stack"
}

// StackTouches returns one Touch per commit in base..bad that changed lines inside the line range,
// at that commit's SHA, of the function of some stack frame. The strongest strength wins per commit.
func StackTouches(p Ports, in Input) ([]Touch, error) {
	commits, err := p.RevList(in.BaseSHA, in.BadSHA)
	if err != nil {
		return nil, err
	}
	var out []Touch
	for _, c := range commits {
		changes, err := p.ChangedLines(c)
		if err != nil {
			return nil, err
		}
		if len(changes) == 0 {
			continue
		}
		touch, ok, err := touchOf(p, in, c, changes)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, touch)
		}
	}
	return out, nil
}

func touchOf(p Ports, in Input, commit string, changes map[string][][2]int) (Touch, bool, error) {
	var best Touch
	found := false
	for _, f := range in.Stack {
		file, err := p.ResolvePath(commit, in.ModulePath, f.File)
		if err != nil {
			continue // the file or module may not exist at this commit
		}
		lines, changed := changes[file]
		if !changed {
			continue
		}
		start, end, ok, err := p.FuncRange(commit, file, f.Function)
		if err != nil || !ok {
			continue
		}
		if !overlaps(lines, start, end) {
			continue
		}
		strength := StrengthStack
		if f.Function == in.LocationFunction {
			strength = StrengthLocation
		}
		if !found || (strength == StrengthLocation && best.Strength != StrengthLocation) {
			best, found = Touch{Commit: commit, Function: f.Function, Strength: strength}, true
		}
	}
	return best, found, nil
}

func overlaps(ranges [][2]int, start, end int) bool {
	for _, r := range ranges {
		if r[0] <= end && r[1] >= start {
			return true
		}
	}
	return false
}
