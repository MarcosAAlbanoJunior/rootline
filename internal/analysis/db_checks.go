package analysis

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// Check ids of the instrumentation.
const (
	CheckCallerHook = "caller_hook"
	CheckDBSpans    = "db_spans"
)

// Span attributes read by the instrumentation checks.
const (
	attrAppStack     = "rootline.app_stack"
	attrHookVersion  = "rootline.hook.version"
	attrDBSystemName = "db.system.name"
	attrDBSystem     = "db.system"
)

// supportedDBSystem is the database whose client spans the detectors read in v1.
const supportedDBSystem = "postgresql"

const guide = "docs/guia-instrumentacao.md"

// compatibleHook is the major.minor of the hook whose attributes this analyze reads. Before 1.0 an
// incompatible change to them bumps the minor, so any patch of it is compatible (ADR-0029).
const compatibleHook = "0.1"

// dbChecks are the caller_hook and db_spans checks over the traces of the bad version (ADR-0027,
// ADR-0029): which database spans Rootline recognized, which it ignored, and how many carry the
// app stack.
func dbChecks(bad []versionTrace) (Check, Check) {
	hook := Check{ID: CheckCallerHook}
	db := Check{ID: CheckDBSpans}
	if len(bad) == 0 {
		hook.Status, hook.Reason = CheckNotApplicable, "no traces of the operation in the bad version"
		db.Status, db.Reason = CheckNotApplicable, hook.Reason
		return hook, db
	}

	var queries, prepares, withStack, pgNoText int
	versions := map[string]bool{}
	ignored := map[string]int{}
	for _, vt := range bad {
		for _, sp := range vt.spans {
			switch detector.PhaseOf(sp) {
			case detector.PhaseQuery:
				queries++
				if st, ok := sp.Strings(attrAppStack); ok && len(st) > 0 {
					withStack++
				}
				if v, ok := sp.String(attrHookVersion); ok && v != "" {
					versions[v] = true
				}
				continue
			case detector.PhasePrepare:
				prepares++
				continue
			}
			if sp.Kind != trace.KindClient {
				continue
			}
			switch sys := dbSystem(sp); sys {
			case "":
			case supportedDBSystem:
				pgNoText++
			default:
				ignored[sys]++
			}
		}
	}

	db.Status = CheckRan
	db.Reason = fmt.Sprintf("%d database spans recognized (%d query, %d prepare) in %d traces",
		queries+prepares, queries, prepares, len(bad))
	if len(ignored) > 0 {
		db.Reason += "; ignored database client spans by system: " + countsBySystem(ignored)
		db.Hint = "Only PostgreSQL is supported in v1 (pgx, with db.query.text on the spans); other databases are on the roadmap."
	}
	if queries+prepares == 0 {
		if pgNoText > 0 {
			db.Reason += fmt.Sprintf("; %d %s client spans carry no db.query.text", pgNoText, supportedDBSystem)
		}
		db.Hint = joinHints(db.Hint, "Without recognized database spans the detectors have nothing to read; see "+guide+".")
	}

	if queries == 0 {
		hook.Status, hook.Reason = CheckNotApplicable, "no database query spans to carry the app stack"
		return hook, db
	}
	hook.Status = CheckRan
	hook.Reason = fmt.Sprintf("%d of %d database query spans (%.0f%%) carry the app stack; %s",
		withStack, queries, 100*float64(withStack)/float64(queries), hookVersions(versions))
	switch {
	case withStack == 0:
		hook.Hint = "The caller hook is not installed, or there is no database instrumentation layer for it to annotate; without the app stack the location is at most the route handler. See " + guide + "."
	case withStack < queries:
		hook.Hint = "Some database spans have no app stack: part of the queries bypass the instrumented driver or the hook. See " + guide + "."
	}
	if older, newer := incompatibleHooks(versions); len(older)+len(newer) > 0 {
		hook.Hint = joinHints(hook.Hint, incompatibleHint(older, newer))
	}
	return hook, db
}

func dbSystem(sp trace.Span) string {
	if v, ok := sp.String(attrDBSystemName); ok && v != "" {
		return v
	}
	v, _ := sp.String(attrDBSystem)
	return v
}

func countsBySystem(m map[string]int) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, fmt.Sprintf("%s: %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

func hookVersions(seen map[string]bool) string {
	if len(seen) == 0 {
		return "hook version not reported"
	}
	vs := make([]string, 0, len(seen))
	for v := range seen {
		vs = append(vs, v)
	}
	sort.Strings(vs)
	if len(vs) == 1 {
		return "hook version " + vs[0]
	}
	return "hook versions " + strings.Join(vs, ", ")
}

// incompatibleHooks splits, sorted, the seen hook versions whose major.minor is not
// compatibleHook into older (or unreadable) and newer ones. A leading "v" is accepted.
func incompatibleHooks(seen map[string]bool) (older, newer []string) {
	want := majorMinor(compatibleHook)
	for v := range seen {
		got := majorMinor(strings.TrimPrefix(v, "v"))
		switch {
		case got == want:
		case got[0] > want[0] || got[0] == want[0] && got[1] > want[1]:
			newer = append(newer, v)
		default:
			older = append(older, v)
		}
	}
	sort.Strings(older)
	sort.Strings(newer)
	return older, newer
}

// majorMinor reads "X.Y[.Z]"; an unreadable version is {-1, -1}, older than any.
func majorMinor(v string) [2]int {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return [2]int{-1, -1}
	}
	x, err1 := strconv.Atoi(parts[0])
	y, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return [2]int{-1, -1}
	}
	return [2]int{x, y}
}

func incompatibleHint(older, newer []string) string {
	var hints []string
	if len(newer) > 0 {
		hints = append(hints, fmt.Sprintf("Hook version %s is incompatible with this analyze, which reads hook %s.x; upgrade rootline to a release that reads it.",
			strings.Join(newer, ", "), compatibleHook))
	}
	if len(older) > 0 {
		hints = append(hints, fmt.Sprintf("Hook version %s is incompatible with this analyze, which reads hook %s.x; update the hook in the app with go get github.com/MarcosAAlbanoJunior/rootline/hook@v%s.",
			strings.Join(older, ", "), compatibleHook, compatibleHook))
	}
	return strings.Join(hints, " ")
}
