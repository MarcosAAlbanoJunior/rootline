package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// twoCommitRepo returns a repository with two commits, oldest first.
func twoCommitRepo(t *testing.T) (dir, c1, c2 string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir = t.TempDir()
	gitc := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitc("init", "-q", "-b", "main")
	for i, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitc("add", "-A")
		gitc("commit", "-q", "-m", fmt.Sprintf("c%d", i+1))
		if i == 0 {
			c1 = gitc("rev-parse", "HEAD")
		}
	}
	return dir, c1, gitc("rev-parse", "HEAD")
}

// serverSpanLine is one OTLP JSON line with a single server span.
func serverSpanLine(service, version, traceID, name string, startNs, ms int64) string {
	return fmt.Sprintf(`{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":%q}},{"key":"service.version","value":{"stringValue":%q}}]},"scopeSpans":[{"scope":{"name":"otelhttp"},"spans":[{"traceId":%q,"spanId":"0000000000000001","name":%q,"kind":2,"startTimeUnixNano":"%d","endTimeUnixNano":"%d"}]}]}]}`,
		service, version, traceID, name, startNs, startNs+ms*1_000_000) + "\n"
}

// symptomsTraces: GET /orders went from 10 to 100 ms between base and bad; GET /health did not
// change. The bad version ran first in time, so only the git history orders them right.
func symptomsTraces(t *testing.T, base, bad string, extraService bool) string {
	t.Helper()
	var b strings.Builder
	const t0 = int64(1767225600000000000)
	for i := 0; i < 40; i++ {
		for _, v := range []struct {
			version string
			offset  int64
			ms      int64
		}{{bad, 0, 100}, {base, 3600, 10}} {
			start := t0 + (v.offset+int64(i))*1_000_000_000
			b.WriteString(serverSpanLine("api", v.version, fmt.Sprintf("%s-o-%d", v.version, i), "GET /orders", start, v.ms))
			b.WriteString(serverSpanLine("api", v.version, fmt.Sprintf("%s-h-%d", v.version, i), "GET /health", start, 1))
		}
	}
	if extraService {
		b.WriteString(serverSpanLine("worker", base, "w-1", "POST /jobs", t0, 5))
	}
	p := filepath.Join(t.TempDir(), "traces.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSymptoms_TextSuggestsTheRegressedOperationWithAReadyAnalyzeCommand(t *testing.T) {
	repo, c1, c2 := twoCommitRepo(t)
	traces := symptomsTraces(t, c1, c2, false)
	var stdout, stderr bytes.Buffer

	code := run([]string{"symptoms", "--traces", traces, "--repo", repo}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"| GET /orders | latency |",
		"| GET /health | no |",
		"Suggested operation (a suggestion, not the origin): GET /orders.",
		"rootline analyze --traces " + traces + " --repo " + repo + " --service api --operation 'GET /orders' --baseline-version " + c1 + " --bad-version " + c2,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "GET /orders") > strings.Index(out, "GET /health") {
		t.Errorf("the regressed operation must come first:\n%s", out)
	}
}

func TestSymptoms_JSONHasTheSameInformation(t *testing.T) {
	repo, c1, c2 := twoCommitRepo(t)
	var stdout, stderr bytes.Buffer

	code := run([]string{"symptoms", "--traces", symptomsTraces(t, c1, c2, false), "--repo", repo, "--format", "json"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	var m struct {
		BaselineVersion string `json:"baseline_version"`
		BadVersion      string `json:"bad_version"`
		VersionOrder    string `json:"version_order"`
		Operations      []struct {
			Method, Route string
			Regression    string   `json:"regression"`
			P50Ratio      *float64 `json:"p50_ratio"`
			Bad           struct {
				N   int     `json:"n"`
				P50 float64 `json:"p50_ms"`
			} `json:"bad"`
		} `json:"operations"`
		Suggested      *struct{ Method, Route string } `json:"suggested"`
		AnalyzeCommand string                          `json:"analyze_command"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if m.BaselineVersion != c1 || m.BadVersion != c2 || m.VersionOrder != "git" {
		t.Errorf("versions = %s→%s by %s", m.BaselineVersion, m.BadVersion, m.VersionOrder)
	}
	if len(m.Operations) != 2 || m.Operations[0].Route != "/orders" || m.Operations[0].Regression != "latency" ||
		*m.Operations[0].P50Ratio != 10 || m.Operations[0].Bad.N != 40 || m.Operations[0].Bad.P50 != 100 {
		t.Errorf("operations = %+v", m.Operations)
	}
	if m.Suggested == nil || m.Suggested.Route != "/orders" || !strings.Contains(m.AnalyzeCommand, "'GET /orders'") {
		t.Errorf("suggested = %+v, command %q", m.Suggested, m.AnalyzeCommand)
	}
}

func TestSymptoms_MoreThanOneServiceExitsTwoListingThem(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"symptoms", "--traces", symptomsTraces(t, "aaaaaaa", "bbbbbbb", true)}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "api, worker") || !strings.Contains(stderr.String(), "--service") {
		t.Errorf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestSymptoms_WithoutTracesExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"symptoms"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
