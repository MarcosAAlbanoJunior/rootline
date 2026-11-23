// Distractors declared in scenario.json: the marker is removed before the commit and the line is
// followed up to the bad commit with git blame (ADR-0022).
package fixture_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

const (
	pagesFile        = "demo-app/internal/pages/pages.go"
	pagesFunction    = "example.com/demo-app/internal/pages.(*Service).Pages"
	distractorMarker = " // rootline:distractor"
)

// pagesWithDistractor is the pages file of the source tree, so the distractor is born in the
// first commit, on line 4.
func pagesWithDistractor() string {
	return lines(
		"package pages",
		"func (s *Service) Pages(ids []int) {",
		"\tfor {",
		"\t\ts.repo.Page()"+distractorMarker,
		"\t}",
		"}",
	)
}

// pagesPatch rewrites the pages file (written without the distractor marker, which is already
// gone from the commit the patch applies to) by inserting body after line 2.
func pagesPatch(body ...string) string {
	header := lines(
		"diff --git a/"+pagesFile+" b/"+pagesFile,
		"--- a/"+pagesFile,
		"+++ b/"+pagesFile,
	)
	hunk := []string{" package pages", " func (s *Service) Pages(ids []int) {"}
	for _, b := range body {
		hunk = append(hunk, "+"+b)
	}
	hunk = append(hunk, " \tfor {", " \t\ts.repo.Page()", " \t}", " }")
	return header + "@@ -1,6 +1," + strconv.Itoa(6+len(body)) + " @@\n" + lines(hunk...)
}

// setupDistractor builds the base scenario plus the pages file with a distractor, and makes the
// regression commit apply regression instead of the default patch. answerInPages moves the answer
// key to the pages file.
func setupDistractor(t *testing.T, regression string, answerInPages bool) (source, scenario string) {
	t.Helper()
	source, scenario = setup(t)
	write(t, filepath.Join(source, pagesFile), pagesWithDistractor())
	write(t, filepath.Join(scenario, "regressao.patch"), regression)
	editScenario(t, scenario, func(sc map[string]any) {
		sc["distractors"] = []map[string]string{{"function": pagesFunction, "file": pagesFile, "marker": distractorMarker}}
		if answerInPages {
			answer := sc["answer"].(map[string]any)
			answer["file"], answer["function"] = pagesFile, pagesFunction
		}
	})
	return source, scenario
}

func editScenario(t *testing.T, scenario string, edit func(map[string]any)) {
	t.Helper()
	path := filepath.Join(scenario, "scenario.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sc map[string]any
	if err := json.Unmarshal(data, &sc); err != nil {
		t.Fatal(err)
	}
	edit(sc)
	b, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(b))
}

func generateErr(t *testing.T, source, scenario string) error {
	t.Helper()
	_, err := fixture.Generate(fixture.Options{ScenarioDir: scenario, SourceDir: source, OutDir: filepath.Join(t.TempDir(), "out")})
	return err
}

func readAnswerKey(t *testing.T, g fixture.Generated) fixture.AnswerKey {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(g.RepoDir), "answer_key.json"))
	if err != nil {
		t.Fatal(err)
	}
	var key fixture.AnswerKey
	if err := json.Unmarshal(raw, &key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestGenerate_DistractorLineFollowsLinesInsertedBeforeItUpToTheBadCommit(t *testing.T) {
	regression := patch() + pagesPatch("\tx := 1", "\ty := 2", "\t_ = x + y")
	source, scenario := setupDistractor(t, regression, false)
	g := generate(t, source, scenario)

	want := []fixture.Distractor{{Function: pagesFunction, File: pagesFile, Line: 4 + 3}}
	if key := readAnswerKey(t, g); !reflect.DeepEqual(key.Distractors, want) {
		t.Fatalf("answer_key.json distractors = %+v, want %+v", key.Distractors, want)
	}
	if !reflect.DeepEqual(g.AnswerKey.Distractors, want) {
		t.Fatalf("Generated distractors = %+v, want %+v", g.AnswerKey.Distractors, want)
	}
	blob := git(t, g.RepoDir, "show", g.Commits[1].SHA+":"+pagesFile)
	if got := strings.Split(blob, "\n")[want[0].Line-1]; got != "\t\ts.repo.Page()" {
		t.Fatalf("line %d at the bad commit = %q, want the page call without the marker", want[0].Line, got)
	}
	if first := git(t, g.RepoDir, "show", g.Commits[0].SHA+":"+pagesFile); strings.Contains(first, "rootline:") {
		t.Fatalf("the marker must be removed in the commit where the distractor is born:\n%s", first)
	}
}

func TestGenerate_DistractorLineEditedBeforeTheBadCommitIsAnErrorCitingTheFile(t *testing.T) {
	edit := lines(
		"diff --git a/"+pagesFile+" b/"+pagesFile,
		"--- a/"+pagesFile,
		"+++ b/"+pagesFile,
		"@@ -2,4 +2,4 @@",
		" func (s *Service) Pages(ids []int) {",
		" \tfor {",
		"-\t\ts.repo.Page()",
		"+\t\ts.repo.Page(ids)",
		" \t}",
	)
	source, scenario := setupDistractor(t, patch()+edit, false)
	err := generateErr(t, source, scenario)
	if err == nil || !strings.Contains(err.Error(), pagesFile) || !strings.Contains(err.Error(), "was edited") {
		t.Fatalf("err = %v, want an error citing %s and saying the distractor line was edited", err, pagesFile)
	}
}

func TestGenerate_DistractorAndAnswerInTheSameFileGetTheirLinesAtTheBadCommit(t *testing.T) {
	regression := pagesPatch("\tfor _, id := range ids {", "\t\ts.repo.ItemsByOrder(id)"+marker, "\t}")
	source, scenario := setupDistractor(t, regression, true)
	g := generate(t, source, scenario)

	key := readAnswerKey(t, g)
	if key.File != pagesFile || key.Line != 4 {
		t.Errorf("answer = %s:%d, want %s:4", key.File, key.Line, pagesFile)
	}
	want := []fixture.Distractor{{Function: pagesFunction, File: pagesFile, Line: 7}}
	if !reflect.DeepEqual(key.Distractors, want) {
		t.Fatalf("distractors = %+v, want %+v", key.Distractors, want)
	}
	ls := strings.Split(git(t, g.RepoDir, "show", g.Commits[1].SHA+":"+pagesFile), "\n")
	if ls[3] != "\t\ts.repo.ItemsByOrder(id)" || ls[6] != "\t\ts.repo.Page()" {
		t.Fatalf("bad commit lines 4 and 7 = %q, %q", ls[3], ls[6])
	}
}

func TestGenerate_LeftoverDistractorMarkerInAnyFileFailsCitingTheCommit(t *testing.T) {
	source, scenario := setupDistractor(t, patch(), false)
	write(t, filepath.Join(source, "demo-app/internal/other/other.go"), "package other // rootline:distractor\n")
	err := generateErr(t, source, scenario)
	if err == nil || !strings.Contains(err.Error(), `"referencia"`) || !strings.Contains(err.Error(), "rootline:") {
		t.Fatalf("err = %v, want a leak error citing commit \"referencia\"", err)
	}
}

func TestGenerate_DistractorMarkerOnTwoLinesOrOnNoneFails(t *testing.T) {
	t.Run("two lines", func(t *testing.T) {
		source, scenario := setupDistractor(t, patch(), false)
		write(t, filepath.Join(source, pagesFile), pagesWithDistractor()+"// again"+distractorMarker+"\n")
		if err := generateErr(t, source, scenario); err == nil || !strings.Contains(err.Error(), "more than one line") {
			t.Fatalf("err = %v, want an error about more than one marked line", err)
		}
	})
	t.Run("no line", func(t *testing.T) {
		source, scenario := setupDistractor(t, patch(), false)
		write(t, filepath.Join(source, pagesFile), strings.ReplaceAll(pagesWithDistractor(), distractorMarker, ""))
		if err := generateErr(t, source, scenario); err == nil || !strings.Contains(err.Error(), "distractors[0]") {
			t.Fatalf("err = %v, want an error citing distractors[0]", err)
		}
	})
}

func TestGenerate_ScenarioWithoutDistractorsWritesNoDistractorsField(t *testing.T) {
	source, scenario := setup(t)
	g := generate(t, source, scenario)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(g.RepoDir), "answer_key.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "distractors") {
		t.Fatalf("answer_key.json must omit distractors:\n%s", raw)
	}
}

func TestGenerate_MarkerAddedToALineFromAnEarlierCommitKeepsThatLine(t *testing.T) {
	source, scenario := setupDistractor(t, "", false)
	write(t, filepath.Join(source, pagesFile), strings.ReplaceAll(pagesWithDistractor(), distractorMarker, ""))
	markExisting := lines(
		"diff --git a/"+pagesFile+" b/"+pagesFile,
		"--- a/"+pagesFile,
		"+++ b/"+pagesFile,
		"@@ -3,3 +3,3 @@",
		" \tfor {",
		"-\t\ts.repo.Page()",
		"+\t\ts.repo.Page()"+distractorMarker,
		" \t}",
	)
	write(t, filepath.Join(scenario, "regressao.patch"), patch()+markExisting)
	g := generate(t, source, scenario)

	want := []fixture.Distractor{{Function: pagesFunction, File: pagesFile, Line: 4}}
	if !reflect.DeepEqual(g.AnswerKey.Distractors, want) {
		t.Fatalf("distractors = %+v, want %+v", g.AnswerKey.Distractors, want)
	}
}
