// Command rootline-eval generates, records and scores evaluation scenarios.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/goindex"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/otlpfile"
	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
	"github.com/MarcosAAlbanoJunior/rootline/internal/recording"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

const usage = `usage:
  rootline-eval generate --scenario <dir> --source <rootline root> --out <dir>
  rootline-eval record   --scenario <dir> --source <rootline root> --out <dir> [--pg-port n] [--collector-port n] [--app-port n]
  rootline-eval score    --recordings <dir> --placar eval/placar.json [--update]`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run returns the exit code: 0 ok, 1 failure or regression, 2 usage error.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "generate":
		return generate(args[1:], stdout, stderr)
	case "record":
		return record(ctx, args[1:], stdout, stderr)
	case "score":
		return score(args[1:], stdout, stderr, scoreRecordings)
	}
	fmt.Fprintln(stderr, usage)
	return 2
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func generate(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("generate", stderr)
	scenario := fs.String("scenario", "", "scenario directory")
	source := fs.String("source", "", "rootline root")
	out := fs.String("out", "", "output directory")
	if code := parse(fs, args, stderr, "scenario", "source", "out"); code != 0 {
		return code
	}
	g, err := fixture.Generate(fixture.Options{ScenarioDir: *scenario, SourceDir: *source, OutDir: *out})
	if err != nil {
		fmt.Fprintf(stderr, "rootline-eval: %v\n", err)
		return 1
	}
	for _, c := range g.Commits {
		fmt.Fprintf(stdout, "%s %s\n", c.Label, c.SHA)
	}
	fmt.Fprintf(stdout, "repo: %s\n", g.RepoDir)
	return 0
}

// parse parses fs and checks that the required flags are non-empty.
func parse(fs *flag.FlagSet, args []string, stderr io.Writer, required ...string) int {
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "rootline-eval: %v\n%s\n", err, usage)
		return 2
	}
	missing := false
	for _, name := range required {
		if fs.Lookup(name).Value.String() == "" {
			fmt.Fprintf(stderr, "rootline-eval: missing required argument --%s\n", name)
			missing = true
		}
	}
	if missing {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	return 0
}

func record(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("record", stderr)
	scenario := fs.String("scenario", "", "scenario directory")
	source := fs.String("source", "", "rootline root")
	out := fs.String("out", "", "output directory")
	pg := fs.Int("pg-port", 0, "Postgres host port")
	col := fs.Int("collector-port", 0, "Collector OTLP HTTP host port")
	app := fs.Int("app-port", 0, "demo-app host port")
	if code := parse(fs, args, stderr, "scenario", "source", "out"); code != 0 {
		return code
	}
	err := recording.Record(ctx, recording.RecordOptions{
		ScenarioDir: *scenario, SourceDir: *source, OutDir: *out,
		PostgresPort: *pg, CollectorPort: *col, AppPort: *app,
	})
	if err != nil {
		fmt.Fprintf(stderr, "rootline-eval: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "recorded to %s\n", *out)
	return 0
}

type fileSource []trace.Span

func (f fileSource) Spans() ([]trace.Span, error) { return f, nil }

// scorer analyzes the recordings under a directory and returns the accumulated scoreboard.
type scorer func(recordingsDir string) (evaluation.Scoreboard, error)

func score(args []string, stdout, stderr io.Writer, scoreAll scorer) int {
	fs := newFlags("score", stderr)
	recordings := fs.String("recordings", "", "directory with recordings (each with manifest.json and traces.jsonl)")
	placar := fs.String("placar", "", "scoreboard file")
	update := fs.Bool("update", false, "rewrite the scoreboard with the current result")
	if code := parse(fs, args, stderr, "recordings", "placar"); code != 0 {
		return code
	}
	current, err := scoreAll(*recordings)
	if err != nil {
		fmt.Fprintf(stderr, "rootline-eval: %v\n", err)
		return 1
	}
	baseline, baseErr := readScoreboardFile(*placar)
	printScore(stdout, current["detectors_only"], baseline["detectors_only"], baseErr == nil)

	if *update {
		if baseErr == nil {
			for _, r := range evaluation.Regressions(baseline, current) {
				fmt.Fprintf(stderr, "rootline-eval: warning: updating over a regression in %s\n", r)
			}
		}
		if err := writeScoreboardAtomically(*placar, current); err != nil {
			fmt.Fprintf(stderr, "rootline-eval: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "updated %s\n", *placar)
		return 0
	}

	if baseErr != nil {
		fmt.Fprintf(stderr, "rootline-eval: %v\n", baseErr)
		return 1
	}
	if reg := evaluation.Regressions(baseline, current); len(reg) > 0 {
		for _, r := range reg {
			fmt.Fprintf(stderr, "rootline-eval: regression in %s\n", r)
		}
		return 1
	}
	return 0
}

// readScoreboardFile reads the scoreboard at path; a parse error names the file.
func readScoreboardFile(path string) (evaluation.Scoreboard, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err // already carries the path
	}
	defer func() { _ = f.Close() }() // read-only
	sb, err := evaluation.ReadScoreboard(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return sb, nil
}

// printScore prints the aggregate lines and then one line per scenario, alphabetically. With a
// readable scoreboard (hasBaseline), a scenario it does not have ends with " (novo)".
func printScore(w io.Writer, s, base evaluation.Score, hasBaseline bool) {
	fmt.Fprintf(w, "detectors_only\n  Causa:  %s\n  Linha:  %s\n  Commit: %s\n  Estrito: %s\n", s.Cause, s.Line, s.Commit, s.Strict)
	fmt.Fprintf(w, "  Níveis da Linha: 1=%d 2=%d ausente=%d\n", s.LineLevels.Level1, s.LineLevels.Level2, s.LineLevels.None)
	names := make([]string, 0, len(s.Scenarios))
	for name := range s.Scenarios {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		o := s.Scenarios[name]
		suffix := ""
		if _, known := base.Scenarios[name]; hasBaseline && !known {
			suffix = " (novo)"
		}
		fmt.Fprintf(w, "  %s: Causa=%s Linha=%s Commit=%s Estrito=%s Nível=%d%s\n",
			name, okOrError(o.Cause), okOrError(o.Line), okOrError(o.Commit), okOrError(o.Strict), o.LineLevel, suffix)
	}
}

func okOrError(hit bool) string {
	if hit {
		return "ok"
	}
	return "erro"
}

// scoreRecordings analyzes every recording under dir and returns the detectors-only score per
// scenario (the name in the manifest). Two recordings of the same scenario are an error, found
// before any analysis runs.
func scoreRecordings(dir string) (evaluation.Scoreboard, error) {
	manifests, err := filepath.Glob(filepath.Join(dir, "*", "manifest.json"))
	if err != nil {
		return nil, err
	}
	if direct := filepath.Join(dir, "manifest.json"); fileExists(direct) {
		manifests = append(manifests, direct)
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("no recordings (manifest.json) under %s", dir)
	}
	sort.Strings(manifests)
	recOf := map[string]string{} // scenario name -> recording directory
	loaded := make([]recording.Manifest, len(manifests))
	for i, mp := range manifests {
		m, err := recording.ReadManifest(mp)
		if err != nil {
			return nil, err // the open/parse error already carries the path
		}
		rec := filepath.Dir(mp)
		if other, dup := recOf[m.Scenario]; dup {
			return nil, fmt.Errorf("scenario %q is recorded twice: %s and %s", m.Scenario, other, rec)
		}
		recOf[m.Scenario], loaded[i] = rec, m
	}
	outcomes := map[string]evaluation.Outcome{}
	for i, mp := range manifests {
		rec := filepath.Dir(mp)
		o, err := scoreManifest(rec, loaded[i], analyzeRecording)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rec, err)
		}
		outcomes[loaded[i].Scenario] = o
	}
	return evaluation.Scoreboard{"detectors_only": evaluation.ScoreScenarios(outcomes)}, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// analyzer runs the analysis of one recording for a symptom.
type analyzer func(rec string, s analysis.Symptom) (analysis.Result, error)

// analyzeRecording analyzes the recorded traces against the fixture repository of the recording.
func analyzeRecording(rec string, s analysis.Symptom) (analysis.Result, error) {
	spans, err := otlpfile.ReadFile(filepath.Join(rec, "traces.jsonl"))
	if err != nil {
		return analysis.Result{}, err
	}
	git := gitcli.New(filepath.Join(rec, "repo"))
	return analysis.Analyze(fileSource(spans), goindex.New(git), git, s)
}

// scoreManifest builds the Symptom from the manifest, runs the analysis and scores the Result
// against the manifest's answer key.
func scoreManifest(rec string, m recording.Manifest, analyze analyzer) (evaluation.Outcome, error) {
	symptom, err := symptomFromManifest(m)
	if err != nil {
		return evaluation.Outcome{}, err
	}
	labelOf := map[string]string{}
	for _, c := range m.Commits {
		labelOf[c.SHA] = c.Label
	}
	res, err := analyze(rec, symptom)
	if err != nil {
		return evaluation.Outcome{}, err
	}

	key := evaluation.Key{CauseType: m.AnswerKey.Cause, Function: m.AnswerKey.Function, File: m.AnswerKey.File,
		Line: m.AnswerKey.Line, CommitLabel: m.AnswerKey.CommitLabel}
	found := foundFromResult(res)
	return evaluation.ScoreOne(key, found, labelOf), nil
}

// symptomFromManifest builds the analysis Symptom only from the manifest: service, method, route
// and route source from its symptom block, versions from the SHAs of the bad and baseline labels.
// A manifest without the block or without symptom.route (older recording) is an error, never a
// default: the load may send several routes, so it cannot say which one is analyzed.
func symptomFromManifest(m recording.Manifest) (analysis.Symptom, error) {
	sy := m.Symptom
	if sy == (fixture.Symptom{}) {
		return analysis.Symptom{}, fmt.Errorf("manifest has no symptom block (recorded before it existed); record again")
	}
	if sy.Route == "" {
		return analysis.Symptom{}, fmt.Errorf("manifest has no symptom.route (recorded before it existed); record again")
	}
	shaOf := map[string]string{}
	for _, c := range m.Commits {
		shaOf[c.Label] = c.SHA
	}
	bad := shaOf[sy.Bad]
	if bad == "" {
		return analysis.Symptom{}, fmt.Errorf("manifest symptom.bad %q is not a recorded commit", sy.Bad)
	}
	var baseline string
	if sy.Baseline != "" {
		if baseline = shaOf[sy.Baseline]; baseline == "" {
			return analysis.Symptom{}, fmt.Errorf("manifest symptom.baseline %q is not a recorded commit", sy.Baseline)
		}
	}
	return analysis.Symptom{
		Service:         sy.Service,
		Operation:       trace.Operation{Method: sy.Method, Route: sy.Route, RouteSource: sy.RouteSource},
		BadVersion:      bad,
		BaselineVersion: baseline,
	}, nil
}

// writeScoreboardAtomically writes to a temporary file in the same directory and renames it over
// path, so a failed write never leaves a truncated scoreboard behind.
func writeScoreboardAtomically(path string, sb evaluation.Scoreboard) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".placar-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = evaluation.WriteScoreboard(tmp, sb); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// foundFromResult reduces a Result to what is scored. Only a level-1 location counts for Line:
// a level-2 (approximate) or absent location is a Line error, and stays in the denominator.
func foundFromResult(res analysis.Result) evaluation.Found {
	var found evaluation.Found
	if res.Cause == nil {
		return found
	}
	found.HasCause, found.CauseType = true, res.Cause.Type
	if l := res.Cause.Location; l != nil {
		found.Function, found.Level = l.Function, l.Level
		if l.Level == 1 {
			found.HasLocation, found.File, found.Line = true, l.File, l.Line
		}
	}
	if res.Cause.Commit != nil {
		found.CommitSHA = res.Cause.Commit.SHA
	}
	return found
}
