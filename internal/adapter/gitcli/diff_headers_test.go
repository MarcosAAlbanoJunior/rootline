package gitcli_test

import (
	"reflect"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
)

// An added line whose content starts with "++" appears as "+++ ..." in the diff body. It must not
// be taken for a file header, or later hunks would be attributed to a bogus file.
func TestChangedLinesAddedLineStartingWithPlusPlusIsNotAFileHeader(t *testing.T) {
	r := newRepo(t)
	r.write("a.go", "l1\nl2\nl3\nl4\nl5\nl6\n")
	r.commit("c1")
	r.write("a.go", "++ x\nl1\nl2\nl3\nl4\nL5\nl6\n") // inserts line 1, changes old line 5 (now line 6)
	c2 := r.commit("c2")

	got, err := gitcli.New(r.dir).ChangedLines(c2)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][2]int{{1, 1}, {6, 6}}; !reflect.DeepEqual(got["a.go"], want) {
		t.Fatalf("a.go ranges = %v, want %v (all: %v)", got["a.go"], want, got)
	}
	if len(got) != 1 {
		t.Fatalf("only a.go changed, got %v", got)
	}
}
