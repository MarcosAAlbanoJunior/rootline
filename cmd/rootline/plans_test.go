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

const (
	repoFunc    = "example.com/demo-app/internal/orders.(*Repo).ByCustomer"
	ordersQuery = "SELECT id FROM orders WHERE customer_id = $1"
)

// repoWithRepository commits a go.mod and a repository file and returns the directory and the SHA.
func repoWithRepository(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	src := "package orders\n" + strings.Repeat("\n", 28) + "func (r *Repo) ByCustomer() { r.db.Query() }\n"
	files := map[string]string{
		"go.mod":                        "module example.com/demo-app\n",
		"internal/orders/repository.go": src,
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
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
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-q", "-m", "c1")
	return dir, git("rev-parse", "HEAD")
}

// singleQueryTraces writes n traces, each running ordersQuery once from the repository line 30.
func singleQueryTraces(t *testing.T, sha string, n int) string {
	t.Helper()
	var b strings.Builder
	for i := 0; i < n; i++ {
		tid := fmt.Sprintf("0af7651916cd43dd8448eb211c8031%02d", i)
		srv := fmt.Sprintf("b7ad6b71692033%02d", i)
		q := fmt.Sprintf("c7ad6b71692033%02d", i)
		fmt.Fprintf(&b, `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"demo-app"}},{"key":"service.version","value":{"stringValue":%q}}]},"scopeSpans":[{"scope":{"name":"otelhttp"},"spans":[{"traceId":%q,"spanId":%q,"name":"GET /customers/{id}/orders","kind":2,"startTimeUnixNano":"1767225600000000000","endTimeUnixNano":"1767225600001000000","attributes":[]},{"traceId":%q,"spanId":%q,"parentSpanId":%q,"name":"query","kind":3,"startTimeUnixNano":"1767225600000100000","endTimeUnixNano":"1767225600000200000","attributes":[{"key":"db.query.text","value":{"stringValue":%q}},{"key":"pgx.rows_affected","value":{"intValue":"10"}},{"key":"rootline.db.phase","value":{"stringValue":"query"}},{"key":"rootline.module.path","value":{"stringValue":"example.com/demo-app"}},{"key":"rootline.app_stack","value":{"arrayValue":{"values":[{"stringValue":%q}]}}}]}]}]}]}`+"\n",
			sha, tid, srv, tid, q, srv, ordersQuery, repoFunc+" internal/orders/repository.go:30")
	}
	p := filepath.Join(t.TempDir(), "traces.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func plansFile(t *testing.T, sha string) string {
	t.Helper()
	line := fmt.Sprintf(`{"version":%q,"query":%q,"plan":[{"Plan":{"Node Type":"Seq Scan","Relation Name":"orders","Plan Rows":10,"Filter":"(customer_id = $1)"}}],"reltuples":{"orders":50000}}`+"\n", sha, ordersQuery)
	p := filepath.Join(t.TempDir(), "plans.jsonl")
	if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun_AnalyzeWithPlansFindsTheMissingIndex(t *testing.T) {
	repo, sha := repoWithRepository(t)
	var stdout, stderr bytes.Buffer

	code := run([]string{"analyze", "--traces", singleQueryTraces(t, sha, 4), "--plans", plansFile(t, sha),
		"--repo", repo, "--service", "demo-app", "--operation", "GET /customers/{id}/orders",
		"--bad-version", sha}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	var res struct {
		Cause struct {
			Type     string `json:"type"`
			Target   string `json:"target"`
			Location struct {
				Level    int    `json:"level"`
				Function string `json:"function"`
				File     string `json:"file"`
				Line     int    `json:"line"`
				InLoop   *bool  `json:"in_loop"`
			} `json:"location"`
		} `json:"cause"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	c := res.Cause
	if c.Type != "missing_index" || c.Target != "orders" {
		t.Fatalf("cause = %+v, want missing_index on orders\n%s", c, stdout.String())
	}
	if c.Location.Level != 1 || c.Location.File != "internal/orders/repository.go" || c.Location.Line != 30 ||
		c.Location.Function != repoFunc || c.Location.InLoop != nil {
		t.Errorf("location = %+v, want level 1 at internal/orders/repository.go:30 without in_loop", c.Location)
	}
}

func TestRun_UnreadablePlansFileExitsOne(t *testing.T) {
	repo, sha := repoWithRepository(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"analyze", "--traces", singleQueryTraces(t, sha, 1), "--plans", filepath.Join(t.TempDir(), "none.jsonl"),
		"--repo", repo, "--service", "demo-app", "--operation", "GET /customers/{id}/orders",
		"--bad-version", sha}, &stdout, &stderr)
	if code != 1 || stderr.Len() == 0 {
		t.Fatalf("exit %d, stderr %q; want 1 with an error", code, stderr.String())
	}
}
