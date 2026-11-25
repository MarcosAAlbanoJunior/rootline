// Integration tests with a temporary repo and the installed git (skipped if git is absent).
// API: see validate_test.go. Errors: gitcli.ErrModuleNotFound, gitcli.ErrAmbiguousModule.
package gitcli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
)

type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

func TestReadBlob_ReadsTheCommitNotTheWorkingCopy(t *testing.T) {
	r := newRepo(t)
	r.write("a.go", "package a // committed\n")
	sha := r.commit("c1")
	r.write("a.go", "package a // dirty working copy\n")

	got, err := gitcli.New(r.dir).ReadBlob(sha, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package a // committed\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRevList_IsOldestFirstAndExcludesBase(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "1\n")
	base := r.commit("c1")
	r.write("a.txt", "2\n")
	c2 := r.commit("c2")
	r.write("a.txt", "3\n")
	c3 := r.commit("c3")

	got, err := gitcli.New(r.dir).RevList(base, c3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{c2, c3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestChangedLines_ReportsNewSideRanges(t *testing.T) {
	r := newRepo(t)
	r.write("a.go", "l1\nl2\nl3\nl4\nl5\nl6\nl7\n")
	r.write("other.go", "x\ny\n")
	r.commit("c1")
	// line 2 changed, lines 5-6 changed, line 7 deleted (pure deletion has no new-side lines)
	r.write("a.go", "l1\nL2\nl3\nl4\nL5\nL6\n")
	r.write("new.go", "n1\nn2\nn3\n")
	c2 := r.commit("c2")

	got, err := gitcli.New(r.dir).ChangedLines(c2)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][2]int{{2, 2}, {5, 6}}; !reflect.DeepEqual(got["a.go"], want) {
		t.Errorf("a.go ranges = %v, want %v", got["a.go"], want)
	}
	if want := [][2]int{{1, 3}}; !reflect.DeepEqual(got["new.go"], want) {
		t.Errorf("new.go ranges = %v, want %v", got["new.go"], want)
	}
	if _, ok := got["other.go"]; ok {
		t.Error("other.go was not touched by the commit")
	}
}

func TestResolvePath_FindsTheModuleDirectory(t *testing.T) {
	r := newRepo(t)
	r.write("demo-app/go.mod", "module example.com/demo-app\n\ngo 1.27\n")
	r.write("demo-app/internal/orders/service.go", "package orders\n")
	r.write("hook/go.mod", "module example.com/hook\n\ngo 1.27\n")
	// decoys that must be ignored
	r.write("testdata/x/go.mod", "module example.com/demo-app\n")
	r.write("vendor/y/go.mod", "module example.com/demo-app\n")
	r.write("eval/fixtures/z/go.mod", "module example.com/demo-app\n")
	sha := r.commit("c1")
	g := gitcli.New(r.dir)

	got, err := g.ResolvePath(sha, "example.com/demo-app", "internal/orders/service.go")
	if err != nil {
		t.Fatal(err)
	}
	if got != "demo-app/internal/orders/service.go" {
		t.Fatalf("got %q", got)
	}

	if _, err := g.ResolvePath(sha, "example.com/missing", "a.go"); !errors.Is(err, gitcli.ErrModuleNotFound) {
		t.Fatalf("want ErrModuleNotFound, got %v", err)
	}
}

func TestResolvePath_RootModule(t *testing.T) {
	r := newRepo(t)
	r.write("go.mod", "module example.com/root\n")
	r.write("main.go", "package main\n")
	sha := r.commit("c1")

	got, err := gitcli.New(r.dir).ResolvePath(sha, "example.com/root", "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if got != "main.go" {
		t.Fatalf("got %q", got)
	}
}

func TestResolvePath_AmbiguityIsAnError(t *testing.T) {
	r := newRepo(t)
	r.write("a/go.mod", "module example.com/dup\n")
	r.write("b/go.mod", "module example.com/dup\n")
	sha := r.commit("c1")

	_, err := gitcli.New(r.dir).ResolvePath(sha, "example.com/dup", "x.go")
	if !errors.Is(err, gitcli.ErrAmbiguousModule) {
		t.Fatalf("want ErrAmbiguousModule, got %v", err)
	}
}

func TestReadBlob_RefusesNonBlobEntries(t *testing.T) {
	r := newRepo(t)
	r.write("ok.go", "package ok\n")
	if err := os.Symlink("/etc/passwd", filepath.Join(r.dir, "link.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	r.commit("c1")
	// gitlink (submodule) entry
	r.git("update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("a", 40)+",sub")
	r.git("commit", "-q", "-m", "c2")
	sha := r.git("rev-parse", "HEAD")
	g := gitcli.New(r.dir)

	if _, err := g.ReadBlob(sha, "ok.go"); err != nil {
		t.Fatalf("regular file must be readable: %v", err)
	}
	for _, p := range []string{"link.go", "sub", "does-not-exist.go", "nodir"} {
		if _, err := g.ReadBlob(sha, p); err == nil {
			t.Errorf("ReadBlob(%q) must be refused (symlink, submodule, or not a blob)", p)
		}
	}
}

func TestHasCommit_TellsKnownCommitsFromUnknownOnes(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "a\n")
	sha := r.commit("c1")
	g := gitcli.New(r.dir)

	for _, tc := range []struct {
		sha  string
		want bool
	}{
		{sha, true},
		{sha[:7], true},
		{"0123456789abcdef0123456789abcdef01234567", false},
		{"1.2.0", false}, // a service.version that is not a SHA
	} {
		got, err := g.HasCommit(tc.sha)
		if err != nil {
			t.Fatalf("HasCommit(%q): %v", tc.sha, err)
		}
		if got != tc.want {
			t.Errorf("HasCommit(%q) = %v, want %v", tc.sha, got, tc.want)
		}
	}
}

func TestHasCommit_OutsideARepositoryIsAnError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := gitcli.New(t.TempDir()).HasCommit("0123456789abcdef0123456789abcdef01234567"); err == nil {
		t.Fatal("want an error outside a repository")
	}
}

func TestIsAncestor_FollowsTheHistory(t *testing.T) {
	r := newRepo(t)
	r.write("a.txt", "a\n")
	c1 := r.commit("c1")
	r.write("a.txt", "b\n")
	c2 := r.commit("c2")
	g := gitcli.New(r.dir)

	for _, tc := range []struct {
		a, d string
		want bool
	}{
		{c1, c2, true},
		{c2, c1, false},
		{"0123456789abcdef0123456789abcdef01234567", c2, false}, // not a commit of the repository
		{"1.2.0", c2, false},
	} {
		got, err := g.IsAncestor(tc.a, tc.d)
		if err != nil {
			t.Fatalf("IsAncestor(%s, %s): %v", tc.a, tc.d, err)
		}
		if got != tc.want {
			t.Errorf("IsAncestor(%s, %s) = %v, want %v", tc.a, tc.d, got, tc.want)
		}
	}
}
