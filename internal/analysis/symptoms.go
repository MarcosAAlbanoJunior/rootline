package analysis

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// History orders versions by the git history. It is optional: without it, FindSymptoms orders
// them by time.
type History interface {
	// IsAncestor reports whether commit a is an ancestor of commit d. A version that is not a
	// commit of the repository is no one's ancestor.
	IsAncestor(a, d string) (bool, error)
}

// How the baseline and the bad version were chosen.
const (
	OrderFlags = "flags" // both given by the user
	OrderGit   = "git"   // the baseline is the ancestor in the git history (ADR-0005)
	OrderTime  = "time"  // the baseline appeared first in the traces
)

// SymptomsQuery narrows FindSymptoms; empty fields are inferred from the traces.
type SymptomsQuery struct {
	Service, BaselineVersion, BadVersion string
}

// SymptomsReport compares every entry operation of a service between two versions (ADR-0028).
type SymptomsReport struct {
	Service         string             `json:"service"`
	BaselineVersion string             `json:"baseline_version"`
	BadVersion      string             `json:"bad_version"`
	VersionOrder    string             `json:"version_order"`
	Notices         []string           `json:"notices"`
	Operations      []OperationSymptom `json:"operations"`
	// Suggested is the operation to analyze, a suggestion and never the origin; nil without one.
	Suggested *OperationRef `json:"suggested"`
}

// OperationRef names an entry operation.
type OperationRef struct {
	Method string `json:"method"`
	Route  string `json:"route"`
}

// OperationSymptom is one entry operation in both versions. A ratio is nil when its denominator
// is zero.
type OperationSymptom struct {
	OperationRef
	Baseline     VersionStats `json:"baseline"`
	Bad          VersionStats `json:"bad"`
	NRatio       *float64     `json:"n_ratio"`
	P50Ratio     *float64     `json:"p50_ratio"`
	P95Ratio     *float64     `json:"p95_ratio"`
	EnoughSample bool         `json:"enough_sample"`
	Regression   string       `json:"regression"` // the regression type, "" without one
}

// VersionInfo describes one version found in the traces of a service.
type VersionInfo struct {
	Version     string
	Traces      int
	First, Last time.Time
}

// ServiceAmbiguityError is returned when the service must be chosen with --service.
type ServiceAmbiguityError struct {
	Services []string
	Asked    string // the requested service, when it is not in the traces
}

func (e *ServiceAmbiguityError) Error() string {
	if e.Asked != "" {
		return fmt.Sprintf("service %q has no entry operations in the traces; services: %s", e.Asked, strings.Join(e.Services, ", "))
	}
	if len(e.Services) == 0 {
		return "no entry operations (server spans) in the traces"
	}
	return "more than one service in the traces; choose one with --service: " + strings.Join(e.Services, ", ")
}

// VersionAmbiguityError is returned when the versions must be chosen with --baseline-version and
// --bad-version.
type VersionAmbiguityError struct {
	Reason   string
	Versions []VersionInfo // in order of first appearance
}

func (e *VersionAmbiguityError) Error() string {
	var b strings.Builder
	b.WriteString(e.Reason + "; choose them with --baseline-version and --bad-version:")
	for _, v := range e.Versions {
		name := v.Version
		if name == "" {
			name = "(no service.version)"
		}
		fmt.Fprintf(&b, "\n  %s: %d traces, %s .. %s", name, v.Traces,
			v.First.UTC().Format(time.RFC3339), v.Last.UTC().Format(time.RFC3339))
	}
	return b.String()
}

// opRoot is the root span of one trace of the service, with its operation.
type opRoot struct {
	op   OperationRef
	root trace.Span
}

// FindSymptoms compares the entry operations of a service between a baseline and a bad version,
// with the regression rule of ADR-0028, and suggests the operation to analyze. h may be nil.
func FindSymptoms(spans []trace.Span, q SymptomsQuery, h History) (SymptomsReport, error) {
	service, err := chooseService(spans, q.Service)
	if err != nil {
		return SymptomsReport{}, err
	}
	roots := rootsOf(spans, service)
	rep := SymptomsReport{Service: service, Notices: []string{}, Operations: []OperationSymptom{}}
	if err := chooseVersions(&rep, roots, q, h); err != nil {
		return SymptomsReport{}, err
	}

	samples := map[OperationRef]*[2]VersionSample{}
	var ops []OperationRef
	for _, r := range roots {
		i := -1
		switch r.root.Version {
		case rep.BaselineVersion:
			i = 0
		case rep.BadVersion:
			i = 1
		}
		if i < 0 {
			continue
		}
		s, ok := samples[r.op]
		if !ok {
			s = &[2]VersionSample{}
			samples[r.op] = s
			ops = append(ops, r.op)
		}
		s[i].DurationsMs = append(s[i].DurationsMs, durationMs(r.root))
		if r.root.Failed() {
			s[i].Errors++
		}
	}

	for _, op := range ops {
		s := samples[op]
		r := Regress(s[0], s[1])
		rep.Operations = append(rep.Operations, OperationSymptom{
			OperationRef: op, Baseline: r.Baseline, Bad: r.Bad, EnoughSample: r.Enough,
			NRatio: finite(r.NRatio()), P50Ratio: finite(r.P50Ratio()), P95Ratio: finite(r.P95Ratio()),
			Regression: r.Type(),
		})
	}
	sortOperations(rep.Operations)
	if len(rep.Operations) > 0 && rep.Operations[0].Regression != "" {
		top := rep.Operations[0].OperationRef
		rep.Suggested = &top
	}
	return rep, nil
}

// sortOperations puts the regressed operations first, then the others with the minimum sample,
// then the rest; each group by the largest p50 ratio, and by name on ties.
func sortOperations(ops []OperationSymptom) {
	group := func(o OperationSymptom) int {
		switch {
		case o.Regression != "":
			return 0
		case o.EnoughSample:
			return 1
		}
		return 2
	}
	sort.SliceStable(ops, func(i, j int) bool {
		a, b := ops[i], ops[j]
		if group(a) != group(b) {
			return group(a) < group(b)
		}
		if ra, rb := rawP50Ratio(a), rawP50Ratio(b); ra != rb {
			return ra > rb
		}
		return a.Method+" "+a.Route < b.Method+" "+b.Route
	})
}

// rawP50Ratio is the p50 ratio with +Inf over a zero baseline, for ordering: the JSON ratio is nil
// there.
func rawP50Ratio(o OperationSymptom) float64 { return ratio(o.Bad.P50, o.Baseline.P50) }

func finite(v float64) *float64 {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return nil
	}
	return &v
}

// chooseService returns the asked service, or the only one with entry operations.
func chooseService(spans []trace.Span, asked string) (string, error) {
	seen := map[string]bool{}
	for _, s := range spans {
		if s.Kind == trace.KindServer && s.Service != "" {
			seen[s.Service] = true
		}
	}
	services := make([]string, 0, len(seen))
	for s := range seen {
		services = append(services, s)
	}
	sort.Strings(services)
	switch {
	case asked != "" && seen[asked]:
		return asked, nil
	case asked != "":
		return "", &ServiceAmbiguityError{Services: services, Asked: asked}
	case len(services) == 1:
		return services[0], nil
	}
	return "", &ServiceAmbiguityError{Services: services}
}

// rootsOf returns the root span of the service in each trace, when it is an entry operation.
func rootsOf(spans []trace.Span, service string) []opRoot {
	byTrace := groupByTrace(spans)
	ids := make([]string, 0, len(byTrace))
	for id := range byTrace {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []opRoot
	for _, id := range ids {
		root, ok := trace.NewTree(byTrace[id]).Root(service)
		if !ok {
			continue
		}
		op, ok := trace.OperationOf(root)
		if !ok {
			continue
		}
		out = append(out, opRoot{op: OperationRef{Method: op.Method, Route: op.Route}, root: root})
	}
	return out
}

// versionsOf lists the versions of the roots in order of first appearance.
func versionsOf(roots []opRoot) []VersionInfo {
	at := map[string]int{}
	var vs []VersionInfo
	for _, r := range roots {
		i, ok := at[r.root.Version]
		if !ok {
			i = len(vs)
			at[r.root.Version] = i
			vs = append(vs, VersionInfo{Version: r.root.Version, First: r.root.Start, Last: r.root.End})
		}
		v := &vs[i]
		v.Traces++
		if r.root.Start.Before(v.First) {
			v.First = r.root.Start
		}
		if r.root.End.After(v.Last) {
			v.Last = r.root.End
		}
	}
	sort.SliceStable(vs, func(i, j int) bool { return vs[i].First.Before(vs[j].First) })
	return vs
}

// chooseVersions fills the baseline and the bad version: from the flags, or, with exactly two
// versions, by the git history when h is given and by time otherwise (ADR-0028).
func chooseVersions(rep *SymptomsReport, roots []opRoot, q SymptomsQuery, h History) error {
	vs := versionsOf(roots)
	if q.BaselineVersion != "" && q.BadVersion != "" {
		if q.BaselineVersion == q.BadVersion {
			return &VersionAmbiguityError{Reason: "--baseline-version and --bad-version are the same version", Versions: vs}
		}
		for _, want := range []string{q.BaselineVersion, q.BadVersion} {
			if !hasVersion(vs, want) {
				return &VersionAmbiguityError{Reason: fmt.Sprintf("version %s is not in the traces", want), Versions: vs}
			}
		}
		rep.BaselineVersion, rep.BadVersion, rep.VersionOrder = q.BaselineVersion, q.BadVersion, OrderFlags
		return nil
	}
	for _, v := range vs {
		if v.Version == "" {
			return &VersionAmbiguityError{Reason: "some traces have no service.version", Versions: vs}
		}
	}
	if len(vs) != 2 {
		return &VersionAmbiguityError{Reason: fmt.Sprintf("%d versions in the traces, and the comparison needs two", len(vs)), Versions: vs}
	}
	first, second := vs[0].Version, vs[1].Version
	if q.BaselineVersion != "" || q.BadVersion != "" {
		// One flag given: the other version is the remaining one.
		known := q.BaselineVersion + q.BadVersion
		other := first
		if other == known {
			other = second
		} else if second != known {
			return &VersionAmbiguityError{Reason: fmt.Sprintf("version %s is not in the traces", known), Versions: vs}
		}
		rep.BaselineVersion, rep.BadVersion, rep.VersionOrder = q.BaselineVersion, q.BadVersion, OrderFlags
		if rep.BaselineVersion == "" {
			rep.BaselineVersion = other
		} else {
			rep.BadVersion = other
		}
		return nil
	}
	if h != nil {
		firstIsAncestor, err := h.IsAncestor(first, second)
		if err != nil {
			return err
		}
		secondIsAncestor, err := h.IsAncestor(second, first)
		if err != nil {
			return err
		}
		switch {
		case firstIsAncestor:
			rep.BaselineVersion, rep.BadVersion, rep.VersionOrder = first, second, OrderGit
			return nil
		case secondIsAncestor:
			rep.BaselineVersion, rep.BadVersion, rep.VersionOrder = second, first, OrderGit
			return nil
		}
		rep.BaselineVersion, rep.BadVersion, rep.VersionOrder = first, second, OrderTime
		rep.Notices = append(rep.Notices, fmt.Sprintf(
			"Neither version is an ancestor of the other in the repository, or one is not a commit of it; the baseline is %s because it appeared first in the traces. Choose them with --baseline-version and --bad-version.", first))
		return nil
	}
	rep.BaselineVersion, rep.BadVersion, rep.VersionOrder = first, second, OrderTime
	rep.Notices = append(rep.Notices, fmt.Sprintf(
		"The baseline is %s because it appeared first in the traces; pass --repo to order the versions by the git history, or --baseline-version and --bad-version.", first))
	return nil
}

func hasVersion(vs []VersionInfo, v string) bool {
	for _, x := range vs {
		if x.Version == v {
			return true
		}
	}
	return false
}
