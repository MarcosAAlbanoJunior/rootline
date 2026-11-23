package gitcli_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
)

func TestValidateSHA_AcceptsOnlyHexObjectNames(t *testing.T) {
	good := []string{"abc1234", "0123456789abcdef", "0123456789abcdef0123456789abcdef01234567", strings.Repeat("a", 64)}
	for _, s := range good {
		if err := gitcli.ValidateSHA(s); err != nil {
			t.Errorf("ValidateSHA(%q) = %v, want nil", s, err)
		}
	}
	bad := []string{
		"", "abc123", "--output=/tmp/x", "-abc1234", "ABC1234", "abc123g", "abc1234 ", "abc1234\n",
		"HEAD", "main", "abc1234^", "abc1234..def5678", "abc1234;rm", strings.Repeat("a", 65),
	}
	for _, s := range bad {
		if err := gitcli.ValidateSHA(s); err == nil {
			t.Errorf("ValidateSHA(%q) = nil, want an error", s)
		}
	}
}

func TestValidatePath_RefusesHostilePaths(t *testing.T) {
	bad := []string{
		"../../etc/passwd", "..", "a/../b.go", "-rf", "-", "/etc/passwd", "a\\b.go", "a\x00.go",
		"", "./a.go", "a//b.go", "a/", "a/./b.go",
	}
	for _, p := range bad {
		if err := gitcli.ValidatePath(p); err == nil {
			t.Errorf("ValidatePath(%q) = nil, want an error", p)
		}
	}
	for _, p := range []string{"a.go", "demo-app/internal/orders/service.go", "dir with space/a.go"} {
		if err := gitcli.ValidatePath(p); err != nil {
			t.Errorf("ValidatePath(%q) = %v, want nil", p, err)
		}
	}
}

type countingRunner struct{ calls [][]string }

func (c *countingRunner) Run(_ string, args ...string) ([]byte, error) {
	c.calls = append(c.calls, args)
	return nil, errors.New("fake runner: must not be reached")
}

func TestGit_InvalidSHARefusedWithoutRunningAProcess(t *testing.T) {
	const evil = "--output=/tmp/x"
	r := &countingRunner{}
	g := gitcli.NewWithRunner("/repo", r)

	if _, err := g.ReadBlob(evil, "a.go"); err == nil {
		t.Error("ReadBlob accepted the hostile SHA")
	}
	if _, err := g.RevList(evil, "abc1234"); err == nil {
		t.Error("RevList accepted the hostile base")
	}
	if _, err := g.RevList("abc1234", evil); err == nil {
		t.Error("RevList accepted the hostile bad version")
	}
	if _, err := g.ChangedLines(evil); err == nil {
		t.Error("ChangedLines accepted the hostile SHA")
	}
	if _, err := g.ResolvePath(evil, "example.com/m", "a.go"); err == nil {
		t.Error("ResolvePath accepted the hostile SHA")
	}
	if len(r.calls) != 0 {
		t.Fatalf("no process may run for an invalid SHA, got calls: %v", r.calls)
	}
}

func TestGit_HostilePathRefusedWithoutRunningAProcess(t *testing.T) {
	r := &countingRunner{}
	g := gitcli.NewWithRunner("/repo", r)
	for _, p := range []string{"../../etc/passwd", "-rf", "/etc/passwd", "a\\b.go", "a\x00.go"} {
		if _, err := g.ReadBlob("abc1234", p); err == nil {
			t.Errorf("ReadBlob accepted %q", p)
		}
	}
	if len(r.calls) != 0 {
		t.Fatalf("syntactically hostile paths must be refused before git runs, got calls: %v", r.calls)
	}
}
