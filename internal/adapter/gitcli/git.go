// Package gitcli reads repository history through the installed git binary. SHAs and paths come
// from telemetry, so they are validated before any process runs and options are always separated
// from values (ADR-0007).
package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Errors returned by ResolvePath.
var (
	ErrModuleNotFound  = errors.New("gitcli: no go.mod with that module path")
	ErrAmbiguousModule = errors.New("gitcli: more than one go.mod with that module path")
)

// Runner runs `git <args>` in dir and returns its standard output.
type Runner interface {
	Run(dir string, args ...string) ([]byte, error)
}

// defaultTimeout bounds every git process.
const defaultTimeout = 60 * time.Second

type execRunner struct {
	bin     string
	timeout time.Duration
}

// subcommand returns the git subcommand of args, skipping leading "-c key=value" pairs.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		return args[i]
	}
	return "?"
}

func (r execRunner) Run(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.WaitDelay = time.Second // do not wait forever on pipes held by grandchildren
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("git %s: timed out after %s", subcommand(args), r.timeout)
		}
		return nil, fmt.Errorf("git %s: %w: %s", subcommand(args), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Git reads one repository.
type Git struct {
	dir    string
	runner Runner

	mu      sync.Mutex
	trees   map[string]map[string]treeEntry // one `git ls-tree -r` per SHA, keyed by path
	modDirs map[modKey]modResult            // memoized module directories; a SHA never changes
}

type modKey struct{ sha, modulePath string }

type modResult struct {
	dir string
	err error
}

// New returns a Git over repoDir using the real git binary.
func New(repoDir string) *Git {
	return NewWithRunner(repoDir, execRunner{bin: "git", timeout: defaultTimeout})
}

// NewWithRunner returns a Git that runs git through r.
func NewWithRunner(repoDir string, r Runner) *Git {
	return &Git{dir: repoDir, runner: r, trees: map[string]map[string]treeEntry{}, modDirs: map[modKey]modResult{}}
}

func (g *Git) run(args ...string) ([]byte, error) { return g.runner.Run(g.dir, args...) }

type treeEntry struct {
	mode, typ, hash, path string
}

// tree returns every entry of the commit's tree (files only, recursively), keyed by path. It runs a
// single `git ls-tree -r` per SHA; a SHA never changes, so the result is cached. A failure to run
// git is not cached.
func (g *Git) tree(sha string) (map[string]treeEntry, error) {
	g.mu.Lock()
	if t, ok := g.trees[sha]; ok {
		g.mu.Unlock()
		return t, nil
	}
	g.mu.Unlock()

	out, err := g.run("ls-tree", "-r", "-z", sha)
	if err != nil {
		return nil, err
	}
	t := map[string]treeEntry{}
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, name, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("gitcli: unexpected ls-tree record %q", rec)
		}
		t[name] = treeEntry{mode: fields[0], typ: fields[1], hash: fields[2], path: name}
	}
	g.mu.Lock()
	g.trees[sha] = t
	g.mu.Unlock()
	return t, nil
}

// GrepGoFiles returns the Go files of the commit that contain the fixed string needle, using a
// single `git grep` process (instead of reading every file). Files in testdata, vendor and
// fixtures folders and test files are skipped. No match is not an error.
func (g *Git) GrepGoFiles(sha, needle string) ([]string, error) {
	if err := ValidateSHA(sha); err != nil {
		return nil, err
	}
	if needle == "" || strings.ContainsRune(needle, 0) || strings.ContainsAny(needle, "\n") {
		return nil, errors.New("gitcli: invalid search string")
	}
	out, err := g.run("grep", "-l", "-z", "-F", "-e", needle, sha, "--", "*.go")
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 { // no match
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, rec := range strings.Split(string(out), "\x00") {
		_, p, ok := strings.Cut(rec, ":")
		if !ok || p == "" || strings.HasSuffix(p, "_test.go") || inIgnoredDir(p) {
			continue
		}
		files = append(files, p)
	}
	sort.Strings(files)
	return files, nil
}

// regularFile looks p up in the tree of sha and requires a regular-file blob (symlinks,
// submodules and directories are refused).
func (g *Git) regularFile(sha, p string) (treeEntry, error) {
	t, err := g.tree(sha)
	if err != nil {
		return treeEntry{}, err
	}
	e, ok := t[p]
	if !ok {
		return treeEntry{}, fmt.Errorf("gitcli: %s not found at %s", p, sha)
	}
	if !isRegularBlob(e) {
		return treeEntry{}, fmt.Errorf("gitcli: %s is not a regular file at %s (mode %s, type %s)", p, sha, e.mode, e.typ)
	}
	return e, nil
}

// ListGoFiles returns the regular, non-test .go files of the commit, sorted, skipping testdata,
// vendor and fixtures folders.
func (g *Git) ListGoFiles(sha string) ([]string, error) {
	if err := ValidateSHA(sha); err != nil {
		return nil, err
	}
	t, err := g.tree(sha)
	if err != nil {
		return nil, err
	}
	var files []string
	for p, e := range t {
		if isRegularBlob(e) && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") && !inIgnoredDir(p) {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	return files, nil
}

func isRegularBlob(e treeEntry) bool {
	return e.typ == "blob" && (e.mode == "100644" || e.mode == "100755")
}

func (g *Git) catBlob(hash string) ([]byte, error) {
	if err := ValidateSHA(hash); err != nil {
		return nil, err
	}
	return g.run("cat-file", "blob", hash)
}

// ReadBlob returns the content of path at sha. The path must be a regular-file blob in that
// commit's tree (symlinks, submodules and directories are refused).
func (g *Git) ReadBlob(sha, p string) ([]byte, error) {
	if err := ValidateSHA(sha); err != nil {
		return nil, err
	}
	if err := ValidatePath(p); err != nil {
		return nil, err
	}
	e, err := g.regularFile(sha, p)
	if err != nil {
		return nil, err
	}
	return g.catBlob(e.hash)
}

// RevList returns the commits in base..bad, oldest first.
func (g *Git) RevList(base, bad string) ([]string, error) {
	if err := ValidateSHA(base); err != nil {
		return nil, err
	}
	if err := ValidateSHA(bad); err != nil {
		return nil, err
	}
	out, err := g.run("rev-list", "--reverse", base+".."+bad)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// ChangedLines returns, per file, the new-side line ranges [start,end] changed by commit.
func (g *Git) ChangedLines(commit string) (map[string][][2]int, error) {
	if err := ValidateSHA(commit); err != nil {
		return nil, err
	}
	out, err := g.run("-c", "core.quotepath=off", "diff-tree", "-p", "--root", "-r", "--no-commit-id",
		"--no-renames", "--no-ext-diff", "--no-color", "--unified=0", commit)
	if err != nil {
		return nil, err
	}
	return parseDiff(string(out))
}

func parseDiff(diff string) (map[string][][2]int, error) {
	ranges := map[string][][2]int{}
	file := ""
	inHunk := false // "+++ " inside a hunk is added content, not a file header
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
		case !inHunk && strings.HasPrefix(line, "+++ "):
			name := strings.TrimRight(strings.TrimPrefix(line, "+++ "), "\t")
			if name == "/dev/null" {
				file = ""
			} else {
				file = strings.TrimPrefix(name, "b/")
			}
		case strings.HasPrefix(line, "@@ "):
			inHunk = true
			if file == "" {
				continue
			}
			start, count, err := parseHunk(line)
			if err != nil {
				return nil, err
			}
			if count > 0 {
				ranges[file] = append(ranges[file], [2]int{start, start + count - 1})
			}
		}
	}
	return ranges, nil
}

// parseHunk reads the new side of "@@ -a,b +c,d @@".
func parseHunk(line string) (start, count int, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
		return 0, 0, fmt.Errorf("gitcli: malformed hunk header %q", line)
	}
	startStr, countStr, hasCount := strings.Cut(strings.TrimPrefix(fields[2], "+"), ",")
	if start, err = strconv.Atoi(startStr); err != nil {
		return 0, 0, fmt.Errorf("gitcli: malformed hunk header %q", line)
	}
	count = 1
	if hasCount {
		if count, err = strconv.Atoi(countStr); err != nil {
			return 0, 0, fmt.Errorf("gitcli: malformed hunk header %q", line)
		}
	}
	return start, count, nil
}

var ignoredDirs = map[string]bool{"testdata": true, "vendor": true, "fixtures": true}

// ResolvePath maps a module-relative file to a repo-relative path by finding, at sha, the go.mod
// whose module directive equals modulePath (ignoring testdata, vendor and fixtures folders). The
// module directory is memoized per (sha, modulePath), and the resolved file must exist as a
// regular file in the commit's tree (checked in memory, no extra process).
func (g *Git) ResolvePath(sha, modulePath, relFile string) (string, error) {
	if err := ValidateSHA(sha); err != nil {
		return "", err
	}
	if err := ValidatePath(relFile); err != nil {
		return "", err
	}
	dir, err := g.moduleDir(sha, modulePath)
	if err != nil {
		return "", err
	}
	full := relFile
	if dir != "." {
		full = path.Join(dir, relFile)
	}
	if _, err := g.regularFile(sha, full); err != nil { // existence and type, from the in-memory tree
		return "", err
	}
	return full, nil
}

// moduleDir finds the directory of the module at sha. Definitive answers (found, not found,
// ambiguous) are cached; failures to run git are not.
func (g *Git) moduleDir(sha, modulePath string) (string, error) {
	key := modKey{sha, modulePath}
	g.mu.Lock()
	if r, ok := g.modDirs[key]; ok {
		g.mu.Unlock()
		return r.dir, r.err
	}
	g.mu.Unlock()

	dir, err, definitive := g.findModuleDir(sha, modulePath)
	if definitive {
		g.mu.Lock()
		g.modDirs[key] = modResult{dir: dir, err: err}
		g.mu.Unlock()
	}
	return dir, err
}

func (g *Git) findModuleDir(sha, modulePath string) (dir string, err error, definitive bool) {
	entries, err := g.tree(sha)
	if err != nil {
		return "", err, false
	}
	var dirs []string
	for _, e := range entries {
		if !isRegularBlob(e) || path.Base(e.path) != "go.mod" || inIgnoredDir(e.path) {
			continue
		}
		data, err := g.catBlob(e.hash)
		if err != nil {
			return "", err, false
		}
		if moduleDirective(string(data)) == modulePath {
			dirs = append(dirs, path.Dir(e.path))
		}
	}
	switch len(dirs) {
	case 0:
		return "", fmt.Errorf("%w: %q at %s", ErrModuleNotFound, modulePath, sha), true
	case 1:
		return dirs[0], nil, true
	}
	sort.Strings(dirs)
	return "", fmt.Errorf("%w: %q at %s in %s", ErrAmbiguousModule, modulePath, sha, strings.Join(dirs, ", ")), true
}

func inIgnoredDir(p string) bool {
	segs := strings.Split(path.Dir(p), "/")
	for _, s := range segs {
		if ignoredDirs[s] {
			return true
		}
	}
	return false
}

func moduleDirective(gomod string) string {
	for _, line := range strings.Split(gomod, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`+"`")
		}
	}
	return ""
}
