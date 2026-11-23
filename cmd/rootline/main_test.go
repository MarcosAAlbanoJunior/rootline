// The CLI is tested in-process through run, so no binary is built or spawned as a subprocess.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo-app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "c1"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return dir + "|" + strings.TrimSpace(string(out))
}

func tracesFile(t *testing.T, sha string) string {
	t.Helper()
	line := `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"demo-app"}},{"key":"service.version","value":{"stringValue":"` + sha + `"}}]},"scopeSpans":[{"scope":{"name":"otelhttp"},"spans":[{"traceId":"0af7651916cd43dd8448eb211c80319c","spanId":"b7ad6b7169203331","name":"GET /customers/{id}/orders","kind":2,"startTimeUnixNano":"1767225600000000000","endTimeUnixNano":"1767225600001000000","attributes":[]}]}]}]}` + "\n"
	p := filepath.Join(t.TempDir(), "traces.jsonl")
	if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun_AnalyzeWritesResultJSONAndExitsZero(t *testing.T) {
	parts := strings.SplitN(fixtureRepo(t), "|", 2)
	repo, sha := parts[0], parts[1]
	var stdout, stderr bytes.Buffer

	code := run([]string{"analyze", "--traces", tracesFile(t, sha), "--repo", repo, "--service", "demo-app",
		"--operation", "GET /customers/{id}/orders", "--bad-version", sha}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	var m map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if m["schema_version"] != "0" || m["mode"] != "detectors_only" || m["status"] != "inconclusive" {
		t.Errorf("unexpected result: %v", m)
	}
}

func TestRun_MissingRequiredArgumentExitsTwo(t *testing.T) {
	full := []string{"analyze", "--traces", "t.jsonl", "--repo", ".", "--service", "demo-app",
		"--operation", "GET /x", "--bad-version", "abc1234"}
	for _, drop := range []string{"--traces", "--repo", "--service", "--operation", "--bad-version"} {
		var args []string
		for i := 0; i < len(full); i++ {
			if full[i] == drop {
				i++ // skip its value
				continue
			}
			args = append(args, full[i])
		}
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("without %s: exit %d, want 2", drop, code)
		}
		if stderr.Len() == 0 || stdout.Len() != 0 {
			t.Errorf("without %s: error must go to stderr only (stdout=%q stderr=%q)", drop, stdout.String(), stderr.String())
		}
	}
}

func TestRun_InvalidSHAExitsTwo(t *testing.T) {
	for _, flag := range []string{"--bad-version", "--baseline-version"} {
		var stdout, stderr bytes.Buffer
		args := []string{"analyze", "--traces", "t.jsonl", "--repo", ".", "--service", "demo-app",
			"--operation", "GET /x", "--bad-version", "abc1234"}
		if flag == "--bad-version" {
			args[len(args)-1] = "--output=/tmp/x"
		} else {
			args = append(args, "--baseline-version", "--output=/tmp/x")
		}
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%s invalid: exit %d, want 2", flag, code)
		}
		if stderr.Len() == 0 || stdout.Len() != 0 {
			t.Errorf("%s invalid: error must go to stderr only", flag)
		}
	}
}
