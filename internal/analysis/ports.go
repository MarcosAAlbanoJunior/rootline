package analysis

import "github.com/MarcosAAlbanoJunior/rootline/internal/trace"

// TraceSource provides the recorded spans.
type TraceSource interface {
	// Spans returns all spans of the file (any service, version or trace).
	Spans() ([]trace.Span, error)
}

// CodeIndex answers code-structure questions at a commit.
type CodeIndex interface {
	InLoop(sha, file string, line int) (bool, error)
	FuncRange(sha, file, function string) (start, end int, found bool, err error)
}

// Git reads repository history.
type Git interface {
	ResolvePath(sha, modulePath, relFile string) (string, error)
	RevList(base, bad string) ([]string, error)
	ChangedLines(commit string) (map[string][][2]int, error)
}

// RouteLocator is an optional capability of a CodeIndex: the level-2 location of ADR-0002. It maps
// the entry route to the declared handler function (runtime symbol and repo-relative file).
// Analyze uses it only when the index implements it and level 1 is unavailable.
type RouteLocator interface {
	FindRouteHandler(sha, method, route string) (function, file string, found bool, err error)
}
