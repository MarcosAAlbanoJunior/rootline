package gitcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerTimesOutOnAHungProcess(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-git")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := execRunner{bin: bin, timeout: 200 * time.Millisecond}

	start := time.Now()
	_, err := r.Run(t.TempDir(), "rev-list", "x")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Run took %s, the timeout did not bound it", elapsed)
	}
}

func TestNewUsesABoundedTimeout(t *testing.T) {
	g := New(".")
	r, ok := g.runner.(execRunner)
	if !ok || r.timeout <= 0 || r.timeout > 5*time.Minute {
		t.Fatalf("New must use an execRunner with a finite timeout, got %#v", g.runner)
	}
}

func TestSubcommandSkipsLeadingConfigPairs(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"rev-list", "--reverse", "a..b"}, "rev-list"},
		{[]string{"-c", "core.quotepath=off", "diff-tree", "-p"}, "diff-tree"},
		{[]string{"-c", "a=b", "-c", "c=d", "show"}, "show"},
		{nil, "?"},
	}
	for _, tc := range tests {
		if got := subcommand(tc.args); got != tc.want {
			t.Errorf("subcommand(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
