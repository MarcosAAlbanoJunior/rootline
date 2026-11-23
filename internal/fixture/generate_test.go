package fixture_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

const (
	serviceFile = "demo-app/internal/orders/service.go"
	marker      = " // rootline:answer"
)

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

func healthyService() string {
	return lines(
		"package orders",
		"func (s *Service) List(ids []int) {",
		"\ts.repo.Items(ids)",
		"}",
	)
}

func patch() string {
	return lines(
		"diff --git a/"+serviceFile+" b/"+serviceFile,
		"--- a/"+serviceFile,
		"+++ b/"+serviceFile,
		"@@ -1,4 +1,6 @@",
		" package orders",
		" func (s *Service) List(ids []int) {",
		"-\ts.repo.Items(ids)",
		"+\tfor _, id := range ids {",
		"+\t\ts.repo.ItemsByOrder(id)"+marker,
		"+\t}",
		" }",
	)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setup builds a tiny rootline source root and scenario dir.
func setup(t *testing.T) (source, scenario string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	source, scenario = t.TempDir(), t.TempDir()
	write(t, filepath.Join(source, serviceFile), healthyService())
	write(t, filepath.Join(source, "demo-app/go.mod"), "module example.com/demo-app\n")
	write(t, filepath.Join(source, "hook/callerhook.go"), "package callerhook\n")
	write(t, filepath.Join(source, "demo-app/bin/app"), "binary")
	write(t, filepath.Join(source, "hook/out/x.txt"), "generated")
	write(t, filepath.Join(source, "out/y.txt"), "generated")
	write(t, filepath.Join(source, "bin/rootline"), "binary")
	write(t, filepath.Join(source, "NOT-COPIED.md"), "x")
	write(t, filepath.Join(scenario, "regressao.patch"), patch())
	sc := map[string]any{
		"name":       "n1-basic",
		"assumption": "loop in the service, query in the repository",
		"commits": []map[string]string{
			{"label": "referencia"},
			{"label": "regressao", "patch": "regressao.patch"},
		},
		"answer": map[string]string{
			"cause": "n_plus_one", "function": "example.com/demo-app/internal/orders.(*Service).List",
			"file": serviceFile, "marker": marker, "commit": "regressao",
		},
		"symptom": map[string]string{
			"service": "demo-app", "method": "GET", "route": "/customers/{id}/orders", "route_source": "span_name",
			"bad": "regressao", "baseline": "referencia",
		},
	}
	b, _ := json.Marshal(sc)
	write(t, filepath.Join(scenario, "scenario.json"), string(b))
	return source, scenario
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func generate(t *testing.T, source, scenario string) fixture.Generated {
	t.Helper()
	g, err := fixture.Generate(fixture.Options{ScenarioDir: scenario, SourceDir: source, OutDir: filepath.Join(t.TempDir(), "out")})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGenerate_TwoCommitsInScenarioOrder(t *testing.T) {
	source, scenario := setup(t)
	g := generate(t, source, scenario)

	if len(g.Commits) != 2 || g.Commits[0].Label != "referencia" || g.Commits[1].Label != "regressao" {
		t.Fatalf("commits = %+v", g.Commits)
	}
	if g.RepoDir != filepath.Join(filepath.Dir(g.RepoDir), "repo") {
		t.Errorf("repo must be <out>/repo, got %s", g.RepoDir)
	}
	revs := strings.Fields(git(t, g.RepoDir, "rev-list", "--reverse", "HEAD"))
	if len(revs) != 2 || revs[0] != g.Commits[0].SHA || revs[1] != g.Commits[1].SHA {
		t.Fatalf("rev-list %v does not match commits %+v", revs, g.Commits)
	}

	first := git(t, g.RepoDir, "ls-tree", "-r", "--name-only", g.Commits[0].SHA)
	for _, want := range []string{serviceFile, "demo-app/go.mod", "hook/callerhook.go"} {
		if !strings.Contains(first, want) {
			t.Errorf("first commit lacks %s:\n%s", want, first)
		}
	}
	for _, banned := range []string{"bin/", "out/", "NOT-COPIED"} {
		if strings.Contains(first, banned) {
			t.Errorf("first commit must not contain %q:\n%s", banned, first)
		}
	}
	if blob := git(t, g.RepoDir, "show", g.Commits[0].SHA+":"+serviceFile); !strings.Contains(blob, "s.repo.Items(ids)") {
		t.Errorf("first commit must be the healthy version:\n%s", blob)
	}
	if blob := git(t, g.RepoDir, "show", g.Commits[1].SHA+":"+serviceFile); !strings.Contains(blob, "ItemsByOrder") {
		t.Errorf("second commit must have the patch applied:\n%s", blob)
	}
}

func TestGenerate_AnswerKeyPointsToTheCallInsideTheLoopAndMarkerIsRemoved(t *testing.T) {
	source, scenario := setup(t)
	g := generate(t, source, scenario)

	key := g.AnswerKey
	if key.File != serviceFile || key.Cause != "n_plus_one" || key.CommitLabel != "regressao" {
		t.Fatalf("answer key = %+v", key)
	}
	blob := git(t, g.RepoDir, "show", g.Commits[1].SHA+":"+key.File)
	ls := strings.Split(blob, "\n")
	if key.Line != 4 {
		t.Fatalf("key.Line = %d, want 4 (the marker line of the patch)", key.Line)
	}
	got := ls[key.Line-1]
	if got != "\t\ts.repo.ItemsByOrder(id)" {
		t.Fatalf("line %d = %q: must be the ItemsByOrder call with the marker removed and no trailing space", key.Line, got)
	}
	if !strings.Contains(ls[key.Line-2], "for ") {
		t.Errorf("the line before the call must be the loop header, got %q", ls[key.Line-2])
	}
	if len(ls) != 7 { // 6 lines + trailing newline => 7 elements
		t.Errorf("line count changed: %d elements", len(ls))
	}

	// answer_key.json is written outside the repo and carries the computed line.
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(g.RepoDir), "answer_key.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fromFile fixture.AnswerKey
	if err := json.Unmarshal(raw, &fromFile); err != nil {
		t.Fatal(err)
	}
	if fromFile != key {
		t.Errorf("answer_key.json = %+v, want %+v", fromFile, key)
	}
	if _, err := os.Stat(filepath.Join(g.RepoDir, "answer_key.json")); err == nil {
		t.Error("answer_key.json must not live inside the fixture repo")
	}
}

func TestGenerate_NoCommitContainsTheMarkerString(t *testing.T) {
	source, scenario := setup(t)
	g := generate(t, source, scenario)

	for _, c := range g.Commits {
		cmd := exec.Command("git", "grep", "-q", "rootline:answer", c.SHA)
		cmd.Dir = g.RepoDir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		err := cmd.Run()
		ee, isExit := err.(*exec.ExitError)
		if err == nil {
			t.Errorf("commit %s (%s) still contains rootline:answer", c.Label, c.SHA)
		} else if !isExit || ee.ExitCode() != 1 {
			t.Errorf("git grep failed unexpectedly on %s: %v", c.SHA, err)
		}
	}
}

func TestGenerate_FixedIdentityDatesAndDeterministicSHAs(t *testing.T) {
	source, scenario := setup(t)
	g1 := generate(t, source, scenario)

	// Hostile ambient git configuration must not change anything.
	home := t.TempDir()
	write(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Evil\n\temail = evil@example.com\n[commit]\n\tgpgsign = true\n")
	t.Setenv("HOME", home)
	t.Setenv("GIT_AUTHOR_NAME", "Evil")
	t.Setenv("GIT_AUTHOR_EMAIL", "evil@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Evil")
	t.Setenv("GIT_COMMITTER_EMAIL", "evil@example.com")
	t.Setenv("GIT_AUTHOR_DATE", "2030-05-05T05:05:05Z")
	t.Setenv("GIT_COMMITTER_DATE", "2030-05-05T05:05:05Z")
	g2 := generate(t, source, scenario)

	for i := range g1.Commits {
		if g1.Commits[i].SHA != g2.Commits[i].SHA {
			t.Errorf("commit %s: SHA differs between runs: %s vs %s", g1.Commits[i].Label, g1.Commits[i].SHA, g2.Commits[i].SHA)
		}
	}

	// 2026-01-01T00:00:00Z = 1767225600; +1h per commit.
	for i, wantTime := range []string{"1767225600", "1767229200"} {
		out := strings.TrimSpace(git(t, g1.RepoDir, "log", "-1", "--format=%an|%ae|%cn|%ce|%at|%ct", g1.Commits[i].SHA))
		want := "Rootline Fixture|fixture@rootline.invalid|Rootline Fixture|fixture@rootline.invalid|" + wantTime + "|" + wantTime
		if out != want {
			t.Errorf("commit %d metadata = %q, want %q", i+1, out, want)
		}
	}
}
