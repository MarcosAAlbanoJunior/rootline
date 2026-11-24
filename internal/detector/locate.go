package detector

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// Reasons a location can be missing.
const (
	ReasonNoAppStack     = "no_app_stack"
	ReasonPathUnresolved = "path_unresolved"
)

// Strategy is the level-1 location rule of a cause type. Each detector declares its own (ADR-0024).
type Strategy interface {
	// locateSpan picks the location of one span from its app stack.
	locateSpan(env Env, sha string, s trace.Span, w *warnSet) (Location, bool)
	// describe adds the warnings about the chosen location; depths are the stack sizes of the
	// spans located there.
	describe(best Location, depths []int, w *warnSet)
}

// The strategies of the cause types.
var (
	// FirstFrameInLoop is the N+1 rule (ADR-0002, ADR-0017): the first frame inside a loop, else
	// the innermost resolvable frame with InLoop false and the warning loop_not_found.
	FirstFrameInLoop Strategy = loopStrategy{}
	// InnermostFrame is the missing-index rule: the innermost resolvable frame, where the query is
	// issued. It does not look for loops, so the location has LoopChecked false.
	InnermostFrame Strategy = innermostStrategy{}
)

// Locate applies the N+1 level-1 rule over rootline.app_stack. When there is no stack it returns a
// nil location and reason "no_app_stack"; when the stack exists but no frame could be resolved to a
// repository path, nil and "path_unresolved".
func Locate(env Env, sha string, spans []trace.Span) (loc *Location, reason string) {
	loc, reason, _ = LocateWith(FirstFrameInLoop, env, sha, spans)
	return loc, reason
}

// LocateWithWarnings is LocateWith using the N+1 rule.
func LocateWithWarnings(env Env, sha string, spans []trace.Span) (loc *Location, reason string, warnings []string) {
	return LocateWith(FirstFrameInLoop, env, sha, spans)
}

// LocateWith locates the spans with the strategy of a cause type: the most frequent location among
// the spans wins. It also returns the problems met on the way: a frame whose path could not be
// resolved is skipped, a frame whose loop check failed counts as not in a loop, and the strategy may
// describe the chosen location (loop_not_found). Each problem is reported once.
func LocateWith(st Strategy, env Env, sha string, spans []trace.Span) (loc *Location, reason string, warnings []string) {
	w := &warnSet{}
	counts := map[Location]int{}
	depths := map[Location][]int{} // stack sizes of the spans located there, in span order
	var order []Location
	hadStack := false
	for _, s := range spans {
		frames := len(parseStack(s))
		if frames > 0 {
			hadStack = true
		}
		l, ok := st.locateSpan(env, sha, s, w)
		if !ok {
			continue
		}
		if _, seen := counts[l]; !seen {
			order = append(order, l)
		}
		counts[l]++
		depths[l] = append(depths[l], frames)
	}
	if len(order) == 0 {
		if hadStack {
			return nil, ReasonPathUnresolved, w.list
		}
		return nil, ReasonNoAppStack, w.list
	}
	best := order[0]
	for _, l := range order[1:] {
		if counts[l] > counts[best] {
			best = l
		}
	}
	st.describe(best, depths[best], w)
	return &best, "", w.list
}

type warnSet struct {
	seen map[string]bool
	list []string
}

func (w *warnSet) add(msg string) {
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	if !w.seen[msg] {
		w.seen[msg] = true
		w.list = append(w.list, msg)
	}
}

type loopStrategy struct{}

func (loopStrategy) locateSpan(env Env, sha string, s trace.Span, w *warnSet) (Location, bool) {
	frames := parseStack(s)
	module, _ := s.String(attrModulePath)
	var first *Location // innermost resolvable frame, used when no frame is in a loop
	for _, f := range frames {
		file, ok := resolveFrame(env, sha, module, f, w)
		if !ok {
			continue
		}
		in, err := env.InLoop(sha, file, f.Line)
		if err != nil {
			w.add(fmt.Sprintf("in_loop_unavailable: %s:%d at %s: %v", file, f.Line, sha, err))
			in = false
		}
		if in {
			return Location{Level: 1, Function: declaredFunction(f.Function), File: file, Line: f.Line, InLoop: true, LoopChecked: true}, true
		}
		if first == nil {
			first = &Location{Level: 1, Function: declaredFunction(f.Function), File: file, Line: f.Line, LoopChecked: true}
		}
	}
	if first != nil {
		return *first, true
	}
	return Location{}, false
}

func (loopStrategy) describe(best Location, depths []int, w *warnSet) {
	if best.InLoop {
		return
	}
	// ADR-0017: the level says where the location came from, not whether the loop was found;
	// that is in InLoop and in this warning (its "loop_not_found:" prefix is the stable id).
	// It describes the chosen location, so spans located elsewhere do not raise it.
	for _, n := range depths {
		w.add(fmt.Sprintf("loop_not_found: no app_stack frame inside a loop (%d frames)", n))
	}
}

type innermostStrategy struct{}

func (innermostStrategy) locateSpan(env Env, sha string, s trace.Span, w *warnSet) (Location, bool) {
	module, _ := s.String(attrModulePath)
	for _, f := range parseStack(s) {
		if file, ok := resolveFrame(env, sha, module, f, w); ok {
			return Location{Level: 1, Function: declaredFunction(f.Function), File: file, Line: f.Line}, true
		}
	}
	return Location{}, false
}

func (innermostStrategy) describe(Location, []int, *warnSet) {}

// resolveFrame maps the frame file to a repository path; a failure is reported and skips the frame.
func resolveFrame(env Env, sha, module string, f StackFrame, w *warnSet) (string, bool) {
	file, err := env.ResolvePath(sha, module, f.File)
	if err != nil {
		w.add(fmt.Sprintf("path_unresolved: %s (module %s) at %s: %v", f.File, module, sha, err))
		return "", false
	}
	return file, true
}

// parseStack parses "<function> <file>:<line>" entries, innermost first, skipping malformed ones.
func parseStack(s trace.Span) []StackFrame {
	entries, _ := s.Strings(attrAppStack)
	var out []StackFrame
	for _, e := range entries {
		sp := strings.LastIndexByte(e, ' ')
		if sp <= 0 {
			continue
		}
		loc := e[sp+1:]
		colon := strings.LastIndexByte(loc, ':')
		if colon <= 0 {
			continue
		}
		line, err := strconv.Atoi(loc[colon+1:])
		if err != nil {
			continue
		}
		out = append(out, StackFrame{Function: e[:sp], File: loc[:colon], Line: line})
	}
	return out
}
