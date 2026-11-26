package fixture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Options configures Generate.
type Options struct {
	ScenarioDir string // directory with scenario.json and the patches
	SourceDir   string // rootline root; demo-app/ and hook/ are copied from it
	OutDir      string // must not exist or be empty
}

// Commit is a generated commit.
type Commit struct{ Label, SHA string }

// AnswerKey is the full answer key, with the computed line. Distractors is omitted when the
// scenario declares none.
type AnswerKey struct {
	Cause       string       `json:"cause"`
	Function    string       `json:"function"`
	Target      string       `json:"target,omitempty"`
	File        string       `json:"file"`
	Marker      string       `json:"marker"`
	CommitLabel string       `json:"commit_label"`
	Line        int          `json:"line"`
	Distractors []Distractor `json:"distractors,omitempty"`
}

// Distractor is a distractor of the answer key, with its line at the bad commit of the symptom.
type Distractor struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// Generated describes the result of Generate.
type Generated struct {
	RepoDir   string
	Commits   []Commit
	AnswerKey AnswerKey
}

var (
	authorName  = "Rootline Fixture"
	authorEmail = "fixture@rootline.invalid"
	epoch       = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	copiedDirs  = []string{"demo-app", "hook"}
	skippedDirs = map[string]bool{"bin": true, "out": true, ".git": true}
)

// leakedString is the prefix of every marker: no commit may contain it, because the modes with an
// LLM read the code.
const leakedString = "rootline:"

// markedLine is where a distractor marker was found: the commit index and the 1-based line, and
// the origin of that line by git blame (the line may come from an earlier commit when the patch
// only appended the marker).
type markedLine struct {
	commit, line int
	originSHA    string
	originLine   int
}

// Generate creates <OutDir>/repo, a git repo with one commit per scenario commit, and
// <OutDir>/answer_key.json. The marker of the answer commit is read to compute the line and
// removed before the commit, so no commit contains it. A distractor marker is removed in the
// commit where it appears, and its line is followed up to the bad commit of the symptom with git
// blame (ADR-0022).
func Generate(opts Options) (Generated, error) {
	sc, err := LoadScenario(opts.ScenarioDir)
	if err != nil {
		return Generated{}, err
	}
	if err := ensureEmptyDir(opts.OutDir); err != nil {
		return Generated{}, err
	}
	repo := filepath.Join(opts.OutDir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		return Generated{}, err
	}
	r := &repoRunner{dir: repo, home: opts.OutDir}
	if _, err := r.git(0, "init", "-q", "-b", "main", "--template="); err != nil {
		return Generated{}, err
	}

	out := Generated{RepoDir: repo, AnswerKey: AnswerKey{
		Cause: sc.Answer.Cause, Function: sc.Answer.Function, Target: sc.Answer.Target, File: sc.Answer.File,
		Marker: sc.Answer.Marker, CommitLabel: sc.Answer.Commit,
	}}
	foundAnswer := false
	answerAt := sc.Answer.Commit
	if answerAt == "" && sc.Answer.Cause == causeSlowCall {
		// No commit is expected of a slow call (ADR-0031); its line is read at the bad version.
		answerAt = sc.Symptom.Bad
	}
	bad := -1
	marked := make([]*markedLine, len(sc.Distractors))
	for i, c := range sc.Commits {
		if i == 0 {
			for _, d := range copiedDirs {
				if err := copyTree(filepath.Join(opts.SourceDir, d), filepath.Join(repo, d)); err != nil {
					return Generated{}, err
				}
			}
		}
		if c.Patch != "" {
			if err := applyPatch(r, opts.ScenarioDir, c.Patch); err != nil {
				return Generated{}, err
			}
		}
		if sc.Answer.Cause != CauseInconclusive && c.Label == answerAt {
			line, err := stripMarker(filepath.Join(repo, filepath.FromSlash(sc.Answer.File)), sc.Answer.Marker)
			if err != nil {
				return Generated{}, err
			}
			out.AnswerKey.Line = line
			foundAnswer = true
		}
		if bad < 0 {
			if err := stripDistractors(repo, sc.Distractors, marked, i); err != nil {
				return Generated{}, err
			}
		}
		sha, err := r.commit(i, c.Label)
		if err != nil {
			return Generated{}, err
		}
		if err := r.assertNoLeak(sha, c.Label); err != nil {
			return Generated{}, err
		}
		out.Commits = append(out.Commits, Commit{Label: c.Label, SHA: sha})
		for j, m := range marked {
			if m != nil && m.commit == i {
				if m.originSHA, m.originLine, err = r.originOf(sha, sc.Distractors[j].File, m.line); err != nil {
					return Generated{}, fmt.Errorf("fixture: distractors[%d]: %w", j, err)
				}
			}
		}
		if c.Label == sc.Symptom.Bad {
			bad = i
		}
	}
	if !foundAnswer && sc.Answer.Cause != CauseInconclusive {
		return Generated{}, fmt.Errorf("fixture: no commit labeled %q in scenario %s", answerAt, sc.Name)
	}
	for j, d := range sc.Distractors {
		m := marked[j]
		if m == nil {
			return Generated{}, fmt.Errorf("fixture: distractors[%d]: no line ends with the marker %q in %s up to commit %q", j, d.Marker, d.File, sc.Symptom.Bad)
		}
		line, err := r.lineAt(out.Commits[bad].SHA, m.originSHA, d.File, m.originLine)
		if err != nil {
			return Generated{}, fmt.Errorf("fixture: distractors[%d]: %w", j, err)
		}
		if line == 0 {
			return Generated{}, fmt.Errorf("fixture: distractors[%d]: the distractor line in %s (line %d of commit %q) was edited before commit %q",
				j, d.File, m.line, sc.Commits[m.commit].Label, sc.Symptom.Bad)
		}
		out.AnswerKey.Distractors = append(out.AnswerKey.Distractors, Distractor{Function: d.Function, File: d.File, Line: line})
	}

	data, err := json.MarshalIndent(out.AnswerKey, "", "  ")
	if err != nil {
		return Generated{}, err
	}
	if err := os.WriteFile(filepath.Join(opts.OutDir, "answer_key.json"), append(data, '\n'), 0o644); err != nil {
		return Generated{}, err
	}
	return out, nil
}

func ensureEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return os.MkdirAll(dir, 0o755)
	case err != nil:
		return err
	case len(entries) > 0:
		return fmt.Errorf("fixture: output directory %s already exists and is not empty", dir)
	}
	return nil
}

// copyTree copies regular files (keeping only the executable bit), skipping bin/, out/ and .git.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel != "." && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil // symlinks and special files are not copied
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		return os.WriteFile(filepath.Join(dst, rel), data, mode)
	})
}

func applyPatch(r *repoRunner, scenarioDir, patch string) error {
	if patch != filepath.Base(patch) || strings.HasPrefix(patch, "-") {
		return fmt.Errorf("fixture: patch name %q must be a plain file name", patch)
	}
	abs, err := filepath.Abs(filepath.Join(scenarioDir, patch))
	if err != nil {
		return err
	}
	// Read the patch here, not only in the git subprocess: go test caches by the files the test
	// process opens, so a changed patch reruns the test that generates every scenario.
	if _, err := os.ReadFile(abs); err != nil {
		return fmt.Errorf("fixture: patch %s: %w", patch, err)
	}
	if _, err := r.git(0, "apply", "--whitespace=nowarn", "--", abs); err != nil {
		return fmt.Errorf("fixture: patch %s: %w", patch, err)
	}
	return nil
}

// stripDistractors removes, in the working tree of commit index i, the marker of every distractor
// not marked yet whose file has it, and records where it was.
func stripDistractors(repo string, ds []ScenarioDistractor, marked []*markedLine, i int) error {
	for j, d := range ds {
		if marked[j] != nil {
			continue
		}
		path := filepath.Join(repo, filepath.FromSlash(d.File))
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		line, err := removeMarker(path, d.Marker)
		if err != nil {
			return fmt.Errorf("fixture: distractors[%d]: %w", j, err)
		}
		if line > 0 {
			marked[j] = &markedLine{commit: i, line: line}
		}
	}
	return nil
}

// stripMarker finds the single line ending with marker, removes the marker (keeping the line
// number) and returns the 1-based line number.
func stripMarker(file, marker string) (int, error) {
	line, err := removeMarker(file, marker)
	if err == nil && line == 0 {
		err = fmt.Errorf("fixture: no line ends with the marker %q in %s", marker, file)
	}
	return line, err
}

// removeMarker is stripMarker without the error for a file that has no marker: it returns 0.
func removeMarker(file, marker string) (int, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(data), "\n")
	found := 0
	for i, l := range lines {
		if strings.HasSuffix(l, marker) {
			if found != 0 {
				return 0, fmt.Errorf("fixture: more than one line ends with the marker in %s", file)
			}
			found = i + 1
			lines[i] = strings.TrimSuffix(l, marker)
		}
	}
	if found == 0 {
		return 0, nil
	}
	return found, os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o644)
}

// repoRunner runs git with a fixed, hermetic environment.
type repoRunner struct{ dir, home string }

func (r *repoRunner) env(commitIndex int) []string {
	when := "@" + strconv.FormatInt(epoch.Add(time.Duration(commitIndex)*time.Hour).Unix(), 10) + " +0000"
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.home,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=" + authorName, "GIT_AUTHOR_EMAIL=" + authorEmail, "GIT_AUTHOR_DATE=" + when,
		"GIT_COMMITTER_NAME=" + authorName, "GIT_COMMITTER_EMAIL=" + authorEmail, "GIT_COMMITTER_DATE=" + when,
	}
}

func (r *repoRunner) git(commitIndex int, args ...string) (string, error) {
	full := append([]string{
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null",
		"-c", "commit.template=", "-c", "core.autocrlf=false", "-c", "core.fileMode=true",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.dir
	cmd.Env = r.env(commitIndex)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("fixture: git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *repoRunner) commit(index int, label string) (string, error) {
	if _, err := r.git(index, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := r.git(index, "commit", "-q", "--no-verify", "--no-gpg-sign", "-m", label); err != nil {
		return "", err
	}
	return r.git(index, "rev-parse", "HEAD")
}

// originOf returns the commit and line that git blame gives as the origin of line of file at sha.
func (r *repoRunner) originOf(sha, file string, line int) (string, int, error) {
	n := strconv.Itoa(line)
	out, err := r.git(0, "blame", "--porcelain", "-L", n+","+n, sha, "--", file)
	if err != nil {
		return "", 0, err
	}
	f := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	if len(f) < 3 {
		return "", 0, fmt.Errorf("fixture: unexpected git blame output for %s:%d", file, line)
	}
	orig, err := strconv.Atoi(f[1])
	return f[0], orig, err
}

// lineAt returns the line of file at commit bad whose origin, by git blame, is line of commit
// origin; 0 when no line has that origin (a later commit edited it).
func (r *repoRunner) lineAt(bad, origin, file string, line int) (int, error) {
	out, err := r.git(0, "blame", "--porcelain", bad, "--", file)
	if err != nil {
		return 0, err
	}
	for _, l := range strings.Split(out, "\n") {
		// Group headers: "<sha> <original line> <final line> [<lines in group>]".
		f := strings.Fields(l)
		if strings.HasPrefix(l, "\t") || len(f) < 3 || f[0] != origin {
			continue
		}
		if orig, err := strconv.Atoi(f[1]); err == nil && orig == line {
			return strconv.Atoi(f[2])
		}
	}
	return 0, nil
}

// assertNoLeak fails if any file in the commit contains the marker prefix.
func (r *repoRunner) assertNoLeak(sha, label string) error {
	cmd := exec.Command("git", "grep", "-q", leakedString, sha)
	cmd.Dir = r.dir
	cmd.Env = r.env(0)
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return fmt.Errorf("fixture: commit %q (%s) still contains %q", label, sha, leakedString)
	case errors.As(err, &ee) && ee.ExitCode() == 1:
		return nil
	}
	return fmt.Errorf("fixture: git grep: %w", err)
}
