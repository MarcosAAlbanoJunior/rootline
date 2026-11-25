package analysis

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/correlation"
	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// Analyze builds the Result for the symptom from the recorded traces, the code index, git and the
// execution plans. plans may be nil: the missing-index detector then does not run.
func Analyze(src TraceSource, idx CodeIndex, git Git, plans Explainer, s Symptom) (Result, error) {
	spans, err := src.Spans()
	if err != nil {
		return Result{}, err
	}
	res := Result{
		SchemaVersion: SchemaVersion,
		Mode:          ModeDetectors,
		Symptom:       symptomInfo(s),
		Status:        StatusInconclusive,
		Candidates:    []Candidate{},
		Evidence:      []any{},
		Observations:  []string{},
		Checks:        []Check{},
		Warnings:      []string{},
	}

	byTrace := groupByTrace(spans)
	bad := tracesOf(byTrace, s, s.BadVersion)
	var good []versionTrace
	if s.BaselineVersion != "" {
		good = tracesOf(byTrace, s, s.BaselineVersion)
	}
	if len(bad) == 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("no traces of %s %s %s in version %s",
			s.Service, s.Operation.Method, s.Operation.Route, s.BadVersion))
		res.Checks = append(res.Checks,
			fromDetector(detector.NPlusOneCheck(nil, nil, nil)),
			fromDetector(detector.MissingIndexCheck(nil, plans != nil, nil, nil)),
			Check{ID: CheckCommitCorrelation, Status: CheckNotApplicable, Reason: "no traces of the operation in the bad version"})
		return res, nil
	}
	if s.BaselineVersion != "" && len(good) == 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("no traces of the operation in baseline version %s", s.BaselineVersion))
	}

	if op, ok := trace.OperationOf(bad[0].root); ok {
		res.Symptom.Operation.RouteSource = op.RouteSource // the real source, not the caller's guess
	}

	cands, discarded, err := detector.Detect(env{idx: idx, git: git}, s.BadVersion, spansOf(bad))
	if err != nil {
		return Result{}, err
	}
	for _, d := range discarded {
		res.Observations = append(res.Observations, fmt.Sprintf("pagination_shape: %s (%d traces)", d.Query, d.Traces))
	}
	res.Checks = append(res.Checks, fromDetector(detector.NPlusOneCheck(spansOf(bad), cands, discarded)))
	var idxCands []detector.Candidate
	var skipped []detector.Skipped
	if plans != nil {
		idxCands, skipped, err = detector.DetectMissingIndex(env{idx: idx, git: git}, plans, s.BadVersion, spansOf(bad))
		if err != nil {
			return Result{}, err
		}
		cands = append(cands, idxCands...)
	}
	res.Checks = append(res.Checks, fromDetector(detector.MissingIndexCheck(spansOf(bad), plans != nil, idxCands, skipped)))

	corr := correlationCheck(git, s)
	correlate := corr.Status == "" // still undecided: the versions are in the repository

	scored := make([]scoredCandidate, 0, len(cands))
	for _, c := range cands {
		sc := scoredCandidate{c: c, score: c.Score, loc: toLocation(c.Location)}
		if c.Location == nil {
			sc.loc = levelTwo(idx, s, c, &res)
		}
		for _, w := range c.Warnings {
			res.Warnings = appendUnique(res.Warnings, w)
		}
		if correlate && len(c.Stack) > 0 {
			corr.Status = CheckRan
			touch, ok, err := bestTouch(idx, git, s, c)
			if err != nil {
				return Result{}, err
			}
			if ok {
				sc.touch = &touch
				if touch.Strength == correlation.StrengthLocation {
					sc.score += 0.5
				} else {
					sc.score += 0.25
				}
			}
		}
		scored = append(scored, sc)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })

	nextEvidence := 0
	newID := func() string { nextEvidence++; return fmt.Sprintf("E%d", nextEvidence) }
	for i, sc := range scored {
		cand := Candidate{
			ID: fmt.Sprintf("C%d", i+1), Type: sc.c.Type, Target: sc.c.Target, Score: sc.score,
			Confidence: sc.c.Confidence, Location: sc.loc, Evidence: []string{},
		}
		id := newID()
		res.Evidence = append(res.Evidence, causeEvidence(id, sc.c))
		cand.Evidence = append(cand.Evidence, id)
		if sc.touch != nil {
			st := StackTouchEvidence{
				ID: newID(), Kind: "stack_touch", Commit: sc.touch.Commit,
				Function: sc.touch.Function, Strength: sc.touch.Strength,
			}
			res.Evidence = append(res.Evidence, st)
			cand.Evidence = append(cand.Evidence, st.ID)
		}
		res.Candidates = append(res.Candidates, cand)
	}

	res.Evidence = append(res.Evidence, statsEvidence(newID(), s.BadVersion, bad))
	if len(good) > 0 {
		res.Evidence = append(res.Evidence, statsEvidence(newID(), s.BaselineVersion, good))
	}

	res.Checks = append(res.Checks, finishCorrelation(corr, s, res.Candidates, scored))

	if len(scored) > 0 {
		top := res.Candidates[0]
		cause := &Cause{CandidateID: top.ID, Type: top.Type, Target: top.Target, Confidence: top.Confidence, Location: top.Location}
		if t := scored[0].touch; t != nil {
			cause.Commit = &CommitRef{SHA: t.Commit, Touch: t.Strength}
		}
		res.Cause = cause
		res.Status = StatusCauseFound
	}
	return res, nil
}

func fromDetector(c detector.Check) Check {
	return Check{ID: c.ID, Status: c.Status, Reason: c.Reason, Hint: c.Hint}
}

// correlationCheck decides, before the candidates are correlated, whether the correlation can run:
// it needs a baseline and both versions in the repository. An empty Status means it can. A git
// failure skips the correlation instead of failing the analysis.
func correlationCheck(git Git, s Symptom) Check {
	c := Check{ID: CheckCommitCorrelation}
	if s.BaselineVersion == "" {
		c.Status, c.Reason = CheckNotApplicable, "no baseline version: there is no commit range to inspect"
		c.Hint = "Pass --baseline-version with the version before the regression to look for the commit that introduced the cause."
		return c
	}
	for _, sha := range []string{s.BaselineVersion, s.BadVersion} {
		ok, err := git.HasCommit(sha)
		if err != nil {
			c.Status, c.Reason = CheckSkipped, fmt.Sprintf("could not read the repository: %v", err)
			c.Hint = "--repo must be a git checkout of the service."
			return c
		}
		if !ok {
			c.Status, c.Reason = CheckSkipped, fmt.Sprintf("version %s is not a commit of the repository", sha)
			c.Hint = "service.version must be the git SHA of the deployed commit, and --repo the repository of the service."
			return c
		}
	}
	return c
}

// finishCorrelation completes the commit_correlation check after the candidates were correlated.
func finishCorrelation(c Check, s Symptom, cands []Candidate, scored []scoredCandidate) Check {
	if c.Status == CheckRan {
		var touched []string
		for i, sc := range scored {
			if sc.touch != nil {
				touched = append(touched, fmt.Sprintf("%s by commit %s (%s)", cands[i].ID, sc.touch.Commit, sc.touch.Strength))
			}
		}
		if len(touched) == 0 {
			c.Reason = fmt.Sprintf("%d candidate(s) checked; no commit in %s..%s changed a function on their stacks",
				len(scored), s.BaselineVersion, s.BadVersion)
		} else {
			c.Reason = "touched: " + strings.Join(touched, "; ")
		}
		return c
	}
	if c.Status == "" {
		c.Status = CheckNotApplicable
		if len(scored) == 0 {
			c.Reason = "no candidate to correlate"
		} else {
			c.Reason = "no candidate has an app stack to compare with the changed lines"
			c.Hint = "Install the caller hook to get the app stack on database spans; see docs/guia-instrumentacao.md."
		}
	}
	return c
}

// causeEvidence is the evidence of what the detector saw: the repeated query of an N+1 or the plan
// node of a missing index.
func causeEvidence(id string, c detector.Candidate) any {
	if c.Plan != nil {
		return QueryPlanEvidence{
			ID: id, Kind: "query_plan", Query: c.Query, NodeType: c.Plan.NodeType, Table: c.Plan.Table,
			RelTuples: c.Plan.RelTuples, PlanRows: c.Plan.PlanRows, Selectivity: c.Plan.Selectivity,
			Filter: c.Plan.Filter, FilterColumns: c.Plan.FilterColumns, GenericPlan: c.Plan.GenericPlan,
			TracesAffected: c.TracesAffected, TracesTotal: c.TracesTotal, SampleTraceID: c.SampleTraceID,
		}
	}
	return RepeatedQueryEvidence{
		ID: id, Kind: "repeated_query", Query: c.Query,
		GroupSizeMedian: c.GroupSizeMedian, RowsMedian: c.RowsMedian,
		PlusOneMatchedRatio: c.PlusOneMatchedRatio,
		TracesAffected:      c.TracesAffected, TracesTotal: c.TracesTotal,
		PhaseInferred: c.PhaseInferred, SampleTraceID: c.SampleTraceID,
		SampleParentSpanID:  c.SampleParentSpanID,
		CollapsedParentName: c.CollapsedParentName, CollapsedParentCount: c.CollapsedParentCount,
	}
}

type scoredCandidate struct {
	c     detector.Candidate
	score float64
	touch *correlation.Touch
	loc   *Location
}

// levelTwo tries the approximate route-to-handler location when level 1 is unavailable. It records
// why in the observations and warnings; it returns nil when level 2 is not possible either.
func levelTwo(idx CodeIndex, s Symptom, c detector.Candidate, res *Result) *Location {
	why := fmt.Sprintf("level 1 unavailable for query %q (%s)", c.Query, c.LocationReason)
	res.Observations = append(res.Observations, "location unavailable: "+why)
	rl, ok := idx.(RouteLocator)
	if !ok {
		res.Warnings = appendUnique(res.Warnings, "location unavailable: "+why+"; the index has no level-2 support")
		return nil
	}
	fn, file, found, err := rl.FindRouteHandler(s.BadVersion, s.Operation.Method, s.Operation.Route)
	switch {
	case err != nil:
		res.Warnings = appendUnique(res.Warnings, fmt.Sprintf("location unavailable: %s; level 2 failed: %v", why, err))
		return nil
	case !found:
		res.Warnings = appendUnique(res.Warnings, fmt.Sprintf("location unavailable: %s; level 2 found no registration of %s %s",
			why, s.Operation.Method, s.Operation.Route))
		return nil
	}
	res.Warnings = appendUnique(res.Warnings, "location is approximate (level 2, route to handler): "+why)
	return &Location{Level: 2, Function: fn, File: file, Approximate: true}
}

func symptomInfo(s Symptom) SymptomInfo {
	info := SymptomInfo{
		Service: s.Service,
		Operation: OperationInfo{
			Method: s.Operation.Method, Route: s.Operation.Route, RouteSource: s.Operation.RouteSource,
		},
		BadVersion: s.BadVersion,
	}
	if s.BaselineVersion != "" {
		v := s.BaselineVersion
		info.BaselineVersion = &v
	}
	return info
}

func toLocation(l *detector.Location) *Location {
	if l == nil {
		return nil
	}
	loc := &Location{Level: l.Level, Function: l.Function, File: l.File, Line: l.Line}
	if l.LoopChecked {
		inLoop := l.InLoop
		loc.InLoop = &inLoop
	}
	return loc
}

// env adapts the analysis ports to detector.Env.
type env struct {
	idx CodeIndex
	git Git
}

func (e env) ResolvePath(sha, modulePath, relFile string) (string, error) {
	return e.git.ResolvePath(sha, modulePath, relFile)
}

func (e env) InLoop(sha, file string, line int) (bool, error) { return e.idx.InLoop(sha, file, line) }

// corrPorts adapts the analysis ports to correlation.Ports.
type corrPorts struct {
	idx CodeIndex
	git Git
}

func (p corrPorts) RevList(base, bad string) ([]string, error) { return p.git.RevList(base, bad) }
func (p corrPorts) ChangedLines(c string) (map[string][][2]int, error) {
	return p.git.ChangedLines(c)
}
func (p corrPorts) FuncRange(sha, file, fn string) (int, int, bool, error) {
	return p.idx.FuncRange(sha, file, fn)
}
func (p corrPorts) ResolvePath(sha, mod, rel string) (string, error) {
	return p.git.ResolvePath(sha, mod, rel)
}

// bestTouch returns the strongest stack touch (earliest commit on ties) for the candidate.
func bestTouch(idx CodeIndex, git Git, s Symptom, c detector.Candidate) (correlation.Touch, bool, error) {
	in := correlation.Input{BaseSHA: s.BaselineVersion, BadSHA: s.BadVersion, ModulePath: c.ModulePath}
	for _, f := range c.Stack {
		in.Stack = append(in.Stack, correlation.Frame{Function: f.Function, File: f.File, Line: f.Line})
	}
	if c.Location != nil {
		in.LocationFunction = c.Location.Function
	}
	touches, err := correlation.StackTouches(corrPorts{idx: idx, git: git}, in)
	if err != nil {
		return correlation.Touch{}, false, err
	}
	var best correlation.Touch
	found := false
	for _, t := range touches {
		if !found || (t.Strength == correlation.StrengthLocation && best.Strength != correlation.StrengthLocation) {
			best, found = t, true
		}
	}
	return best, found, nil
}

type versionTrace struct {
	root  trace.Span
	spans []trace.Span
}

func groupByTrace(spans []trace.Span) map[string][]trace.Span {
	m := map[string][]trace.Span{}
	for _, sp := range spans {
		m[sp.TraceID] = append(m[sp.TraceID], sp)
	}
	return m
}

// tracesOf selects, in a stable order, the traces whose service root is the symptom's operation
// at the given version.
func tracesOf(byTrace map[string][]trace.Span, s Symptom, version string) []versionTrace {
	ids := make([]string, 0, len(byTrace))
	for id := range byTrace {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []versionTrace
	for _, id := range ids {
		root, ok := trace.NewTree(byTrace[id]).Root(s.Service)
		if !ok || root.Version != version {
			continue
		}
		op, ok := trace.OperationOf(root)
		if !ok || op.Method != s.Operation.Method || op.Route != s.Operation.Route {
			continue
		}
		out = append(out, versionTrace{root: root, spans: byTrace[id]})
	}
	return out
}

func spansOf(vts []versionTrace) [][]trace.Span {
	out := make([][]trace.Span, 0, len(vts))
	for _, vt := range vts {
		out = append(out, vt.spans)
	}
	return out
}

func statsEvidence(id, version string, vts []versionTrace) SymptomStatsEvidence {
	durations := make([]float64, 0, len(vts))
	counts := make([]int, 0, len(vts))
	for _, vt := range vts {
		durations = append(durations, float64(vt.root.End.Sub(vt.root.Start))/float64(time.Millisecond))
		n := 0
		for _, sp := range vt.spans {
			if detector.PhaseOf(sp) == detector.PhaseQuery {
				n++
			}
		}
		counts = append(counts, n)
	}
	return SymptomStatsEvidence{
		ID: id, Kind: "symptom_stats", Version: version, Traces: len(vts),
		DurationMsMedian: medianFloat(durations), DBQuerySpansMedian: medianInt(counts),
	}
}

func medianFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	if len(c)%2 == 1 {
		return c[len(c)/2]
	}
	return (c[len(c)/2-1] + c[len(c)/2]) / 2
}

func medianInt(v []int) int {
	if len(v) == 0 {
		return 0
	}
	c := append([]int(nil), v...)
	sort.Ints(c)
	return c[len(c)/2]
}

func appendUnique(list []string, msg string) []string {
	for _, m := range list {
		if m == msg {
			return list
		}
	}
	return append(list, msg)
}
