package gitcli_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
)

const (
	gomodHash = "0123456789abcdef0123456789abcdef01234567"
	fileHash  = "1123456789abcdef0123456789abcdef01234567"
)

// scriptedRunner answers the git commands the adapter issues and counts them by subcommand.
type scriptedRunner struct {
	mu      sync.Mutex
	counts  map[string]int
	failing bool
	gomod   string // content of the single go.mod blob
}

func (r *scriptedRunner) count(cmd string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[cmd]
}

func (r *scriptedRunner) Run(_ string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts == nil {
		r.counts = map[string]int{}
	}
	r.counts[args[0]]++
	if r.failing {
		return nil, errors.New("git unavailable")
	}
	switch args[0] {
	case "ls-tree":
		rows := []string{
			"100644 blob " + gomodHash + "\tdemo-app/go.mod",
			"100644 blob " + fileHash + "\tdemo-app/a.go",
			"100644 blob " + fileHash + "\tdemo-app/internal/b.go",
			"100755 blob " + fileHash + "\tdemo-app/internal/c/d.go",
			"100644 blob " + fileHash + "\tdemo-app/internal/c/d_test.go",
			"100644 blob " + fileHash + "\tdemo-app/testdata/skip.go",
			"120000 blob " + fileHash + "\tdemo-app/link.go",
			"160000 commit " + fileHash + "\tdemo-app/sub",
		}
		return []byte(strings.Join(rows, "\x00") + "\x00"), nil
	case "cat-file":
		if args[len(args)-1] == gomodHash {
			return []byte(r.gomod), nil
		}
		return []byte("package x\n"), nil
	}
	return nil, errors.New("unexpected command " + strings.Join(args, " "))
}

func TestResolvePathNeedsOneTreeListingPerShaAndNoProcessPerFile(t *testing.T) {
	r := &scriptedRunner{gomod: "module example.com/demo-app\n"}
	g := gitcli.NewWithRunner("/repo", r)

	for _, rel := range []string{"a.go", "internal/b.go", "internal/c/d.go", "a.go"} {
		got, err := g.ResolvePath("abc1234", "example.com/demo-app", rel)
		if err != nil {
			t.Fatal(err)
		}
		if got != "demo-app/"+rel {
			t.Fatalf("got %q", got)
		}
	}
	if r.count("ls-tree") != 1 || r.count("cat-file") != 1 {
		t.Fatalf("ls-tree ran %d times and cat-file %d times; want 1 and 1 for any number of files",
			r.count("ls-tree"), r.count("cat-file"))
	}

	// A different SHA is a different tree.
	if _, err := g.ResolvePath("def5678", "example.com/demo-app", "a.go"); err != nil {
		t.Fatal(err)
	}
	if r.count("ls-tree") != 2 {
		t.Fatalf("a new SHA needs its own listing: ls-tree ran %d times, want 2", r.count("ls-tree"))
	}
}

func TestResolvePathChecksExistenceAndTypeAgainstTheTree(t *testing.T) {
	r := &scriptedRunner{gomod: "module example.com/demo-app\n"}
	g := gitcli.NewWithRunner("/repo", r)

	for _, rel := range []string{"missing.go", "internal", "link.go", "sub", "internal/nope/x.go"} {
		if got, err := g.ResolvePath("abc1234", "example.com/demo-app", rel); err == nil {
			t.Errorf("ResolvePath(%q) = %q, want an error (absent, directory, symlink or submodule)", rel, got)
		}
	}
	if r.count("ls-tree") != 1 || r.count("cat-file") != 1 {
		t.Fatalf("the checks must not spawn processes: ls-tree=%d cat-file=%d", r.count("ls-tree"), r.count("cat-file"))
	}
}

func TestReadBlobSharesTheCachedTree(t *testing.T) {
	r := &scriptedRunner{gomod: "module example.com/demo-app\n"}
	g := gitcli.NewWithRunner("/repo", r)

	if _, err := g.ResolvePath("abc1234", "example.com/demo-app", "a.go"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"demo-app/a.go", "demo-app/internal/b.go"} {
		if _, err := g.ReadBlob("abc1234", p); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"demo-app/link.go", "demo-app/sub", "demo-app/absent.go", "demo-app"} {
		if _, err := g.ReadBlob("abc1234", p); err == nil {
			t.Errorf("ReadBlob(%q) must be refused", p)
		}
	}
	if r.count("ls-tree") != 1 {
		t.Fatalf("ls-tree ran %d times, want 1 for everything at one SHA", r.count("ls-tree"))
	}
}

func TestListGoFilesSkipsTestsAndIgnoredFolders(t *testing.T) {
	r := &scriptedRunner{gomod: "module example.com/demo-app\n"}
	g := gitcli.NewWithRunner("/repo", r)

	got, err := g.ListGoFiles("abc1234")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"demo-app/a.go", "demo-app/internal/b.go", "demo-app/internal/c/d.go"}
	// link.go is a symlink and must not be listed either.
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := g.ListGoFiles("--output=/tmp/x"); err == nil {
		t.Fatal("invalid SHA must be refused")
	}
}

func TestResolvePathCachesNotFoundButNotRunnerFailures(t *testing.T) {
	r := &scriptedRunner{gomod: "module example.com/other\n"}
	g := gitcli.NewWithRunner("/repo", r)
	for i := 0; i < 3; i++ {
		if _, err := g.ResolvePath("abc1234", "example.com/missing", "a.go"); !errors.Is(err, gitcli.ErrModuleNotFound) {
			t.Fatalf("want ErrModuleNotFound, got %v", err)
		}
	}
	if r.count("ls-tree") != 1 || r.count("cat-file") != 1 {
		t.Fatalf("not-found is definitive and must be cached: ls-tree=%d cat-file=%d", r.count("ls-tree"), r.count("cat-file"))
	}

	flaky := &scriptedRunner{failing: true, gomod: "module example.com/demo-app\n"}
	g = gitcli.NewWithRunner("/repo", flaky)
	if _, err := g.ResolvePath("abc1234", "example.com/demo-app", "a.go"); err == nil {
		t.Fatal("expected the runner failure")
	}
	flaky.mu.Lock()
	flaky.failing = false
	flaky.mu.Unlock()
	if got, err := g.ResolvePath("abc1234", "example.com/demo-app", "a.go"); err != nil || got != "demo-app/a.go" {
		t.Fatalf("a transient failure must not be cached: %q %v", got, err)
	}
}
