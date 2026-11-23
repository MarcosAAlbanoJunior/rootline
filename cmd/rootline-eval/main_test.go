// Tests of the `score` command with an injected scorer, so no recording is needed.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

var oneOfOne = evaluation.Metric{Hits: 1, Total: 1}

func board(cause, line, commit, strict evaluation.Metric) evaluation.Scoreboard {
	return evaluation.Scoreboard{"detectors_only": {Cause: cause, Line: line, Commit: commit, Strict: strict}}
}

func fixedScorer(sb evaluation.Scoreboard, err error) scorer {
	return func(string) (evaluation.Scoreboard, error) { return sb, err }
}

func writeBoard(t *testing.T, sb evaluation.Scoreboard) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "placar.json")
	var buf bytes.Buffer
	if err := evaluation.WriteScoreboard(&buf, sb); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runScore(t *testing.T, sc scorer, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = score(args, &out, &errb, sc)
	return code, out.String(), errb.String()
}

func TestScoreExitsOneNamingTheMetricThatDropped(t *testing.T) {
	placar := writeBoard(t, board(oneOfOne, oneOfOne, oneOfOne, oneOfOne))
	zero := evaluation.Metric{Hits: 0, Total: 1}

	code, _, stderr := runScore(t, fixedScorer(board(oneOfOne, zero, oneOfOne, zero), nil),
		"--recordings", "r", "--placar", placar)

	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, want := range []string{"detectors_only.line", "detectors_only.strict"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr must name %s, got %q", want, stderr)
		}
	}
	if strings.Contains(stderr, "detectors_only.cause") {
		t.Errorf("cause did not drop, stderr = %q", stderr)
	}
}

func TestScoreExitsZeroWhenNothingDropped(t *testing.T) {
	placar := writeBoard(t, board(oneOfOne, oneOfOne, oneOfOne, oneOfOne))
	code, stdout, stderr := runScore(t, fixedScorer(board(oneOfOne, oneOfOne, oneOfOne, oneOfOne), nil),
		"--recordings", "r", "--placar", placar)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "1/1, 100%") {
		t.Errorf("stdout must show the metrics as hits/total, pct%%, got %q", stdout)
	}
}

func TestScoreDifferentDenominatorIsNotARegression(t *testing.T) {
	placar := writeBoard(t, board(oneOfOne, oneOfOne, oneOfOne, oneOfOne))
	twoTotal := evaluation.Metric{Hits: 1, Total: 2}
	code, _, stderr := runScore(t, fixedScorer(board(twoTotal, twoTotal, twoTotal, twoTotal), nil),
		"--recordings", "r", "--placar", placar)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestScoreUpdateRewritesTheScoreboardAtomically(t *testing.T) {
	placar := writeBoard(t, board(oneOfOne, oneOfOne, oneOfOne, oneOfOne))
	two := evaluation.Metric{Hits: 2, Total: 2}

	code, _, stderr := runScore(t, fixedScorer(board(two, two, two, two), nil),
		"--recordings", "r", "--placar", placar, "--update")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	f, err := os.Open(placar)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	got, err := evaluation.ReadScoreboard(f)
	if err != nil {
		t.Fatal(err)
	}
	if got["detectors_only"].Strict != two {
		t.Fatalf("scoreboard not rewritten: %+v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(placar))
	if len(entries) != 1 {
		t.Fatalf("no temporary file may be left behind, dir has %d entries", len(entries))
	}
	info, _ := os.Stat(placar)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("scoreboard mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestScoreUpdateReportsTheRewriteAndWarnsAboutLockedInRegressions(t *testing.T) {
	placar := writeBoard(t, board(oneOfOne, oneOfOne, oneOfOne, oneOfOne))
	zero := evaluation.Metric{Hits: 0, Total: 1}

	code, stdout, stderr := runScore(t, fixedScorer(board(oneOfOne, zero, oneOfOne, zero), nil),
		"--recordings", "r", "--placar", placar, "--update")

	if code != 0 {
		t.Fatalf("--update proceeds (exit 0), got %d", code)
	}
	if !strings.Contains(stdout, "updated "+placar) {
		t.Errorf("stdout must say the file was rewritten, got %q", stdout)
	}
	for _, want := range []string{"detectors_only.line", "detectors_only.strict"} {
		if !strings.Contains(stderr, "warning") || !strings.Contains(stderr, want) {
			t.Errorf("stderr must warn about %s, got %q", want, stderr)
		}
	}
}

func TestScoreUpdateFailureKeepsTheOldContentWhenTheDirectoryIsReadOnly(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	placar := writeBoard(t, board(oneOfOne, oneOfOne, oneOfOne, oneOfOne))
	before, err := os.ReadFile(placar)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(placar)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	two := evaluation.Metric{Hits: 2, Total: 2}

	code, _, stderr := runScore(t, fixedScorer(board(two, two, two, two), nil),
		"--recordings", "r", "--placar", placar, "--update")

	if code != 1 || stderr == "" {
		t.Fatalf("exit %d stderr %q, want 1 with a message", code, stderr)
	}
	after, err := os.ReadFile(placar)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("the old scoreboard must be untouched: %v\nbefore %s\nafter  %s", err, before, after)
	}
}

func TestScoreUpdateFailureRemovesTheTemporaryFile(t *testing.T) {
	// A directory in place of the file makes the final rename fail after the temp file is written.
	dir := filepath.Join(t.TempDir(), "placar.json")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	two := evaluation.Metric{Hits: 2, Total: 2}

	code, _, stderr := runScore(t, fixedScorer(board(two, two, two, two), nil),
		"--recordings", "r", "--placar", dir, "--update")
	if code != 1 || stderr == "" {
		t.Fatalf("exit %d stderr %q, want 1 with a message", code, stderr)
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	if len(entries) != 1 {
		t.Fatalf("the temporary file must be removed after a failed update, found %d entries", len(entries))
	}
}

func TestScoreMissingArgumentsExitTwo(t *testing.T) {
	for _, args := range [][]string{{"--placar", "p.json"}, {"--recordings", "r"}, {}} {
		code, _, stderr := runScore(t, fixedScorer(nil, nil), args...)
		if code != 2 || !strings.Contains(stderr, "missing required argument") {
			t.Errorf("args %v: exit %d stderr %q, want 2 and a missing-argument message", args, code, stderr)
		}
	}
}

func TestScoreScorerErrorExitsOne(t *testing.T) {
	placar := writeBoard(t, board(oneOfOne, oneOfOne, oneOfOne, oneOfOne))
	code, _, stderr := runScore(t, fixedScorer(nil, os.ErrNotExist), "--recordings", "r", "--placar", placar)
	if code != 1 || stderr == "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestScoreMissingPlacarFileExitsOne(t *testing.T) {
	code, _, stderr := runScore(t, fixedScorer(board(oneOfOne, oneOfOne, oneOfOne, oneOfOne), nil),
		"--recordings", "r", "--placar", filepath.Join(t.TempDir(), "absent.json"))
	if code != 1 || stderr == "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestScoreRecordingsWithoutManifestsIsAnError(t *testing.T) {
	if _, err := scoreRecordings(t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory without recordings")
	}
}

func resultWithLocation(l *analysis.Location) analysis.Result {
	return analysis.Result{Cause: &analysis.Cause{
		Type: "n_plus_one", Location: l, Commit: &analysis.CommitRef{SHA: "sha-reg", Touch: "location"},
	}}
}

func TestFoundFromResultOnlyLevelOneCountsAsALocation(t *testing.T) {
	level1 := foundFromResult(resultWithLocation(&analysis.Location{Level: 1, Function: "f", File: "a.go", Line: 7}))
	if !level1.HasCause || !level1.HasLocation || level1.File != "a.go" || level1.Line != 7 || level1.Function != "f" || level1.CommitSHA != "sha-reg" {
		t.Fatalf("level 1: %+v", level1)
	}

	level2 := foundFromResult(resultWithLocation(&analysis.Location{Level: 2, Function: "handler", File: "h.go", Approximate: true}))
	if level2.HasLocation || level2.File != "" || level2.Line != 0 {
		t.Fatalf("a level-2 location must not count for Line: %+v", level2)
	}
	if !level2.HasCause || level2.Function != "handler" {
		t.Fatalf("the cause and function are still reported: %+v", level2)
	}

	none := foundFromResult(resultWithLocation(nil))
	if none.HasLocation || !none.HasCause {
		t.Fatalf("absent location: %+v", none)
	}
	if got := foundFromResult(analysis.Result{}); got != (evaluation.Found{}) {
		t.Fatalf("no cause must give an empty Found, got %+v", got)
	}
}

func TestLevelTwoOrAbsentLocationIsALineErrorInTheDenominator(t *testing.T) {
	key := evaluation.Key{CauseType: "n_plus_one", Function: "svc.List", File: "demo-app/service.go", Line: 42, CommitLabel: "regressao"}
	labels := map[string]string{"sha-reg": "regressao"}

	var total evaluation.Score
	total.Add(evaluation.ScoreOne(key, foundFromResult(resultWithLocation(
		&analysis.Location{Level: 1, Function: "svc.List", File: "demo-app/service.go", Line: 42})), labels))
	total.Add(evaluation.ScoreOne(key, foundFromResult(resultWithLocation(
		&analysis.Location{Level: 2, Function: "h.listOrders", File: "demo-app/handler.go", Approximate: true})), labels))
	total.Add(evaluation.ScoreOne(key, foundFromResult(resultWithLocation(nil)), labels))

	if total.Line != (evaluation.Metric{Hits: 1, Total: 3}) {
		t.Fatalf("Line = %s, want 1/3", total.Line)
	}
	if total.Commit != (evaluation.Metric{Hits: 3, Total: 3}) {
		t.Fatalf("Commit = %s: the commit is scored independently of the location", total.Commit)
	}
}
