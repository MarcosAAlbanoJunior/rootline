package goindex_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/goindex"
)

type fakeBlobs map[string]string // "sha:path" -> content

func (f fakeBlobs) ReadBlob(sha, path string) ([]byte, error) {
	c, ok := f[sha+":"+path]
	if !ok {
		return nil, fmt.Errorf("no blob %s:%s", sha, path)
	}
	return []byte(c), nil
}

// Line numbers are noted at the right of each line of the source below (1-based).
var src = strings.Join([]string{
	"package orders",                      // 1
	"",                                    // 2
	"type Service struct{}",               // 3
	"type Repo struct{}",                  // 4
	"",                                    // 5
	"func (s *Service) List(ids []int) {", // 6
	"\tbefore()",                          // 7
	"\tfor _, id := range ids {",          // 8
	"\t\tcall(id)",                        // 9
	"\t}",                                 // 10
	"\tafter()",                           // 11
	"}",                                   // 12
	"",                                    // 13
	"func (r Repo) Get(id int) {",         // 14
	"\tfor i := 0; i < id; i++ {",         // 15
	"\t\tfn := func() {",                  // 16
	"\t\t\tinner()",                       // 17
	"\t\t}",                               // 18
	"\t\tfn()",                            // 19
	"\t}",                                 // 20
	"}",                                   // 21
	"",                                    // 22
	"func Helper() {",                     // 23
	"\tfor range 3 {",                     // 24
	"\t\twork()",                          // 25
	"\t}",                                 // 26
	"}",                                   // 27
	"",                                    // 28
	"func Closer() {",                     // 29
	"\tgo func() {",                       // 30
	"\t\tplain()",                         // 31
	"\t}()",                               // 32
	"}",                                   // 33
}, "\n") + "\n"

func newIndex() *goindex.Index {
	return goindex.New(fakeBlobs{"sha1:svc.go": src})
}

func TestInLoop_IsTrueOnlyStrictlyInsideTheLoopBody(t *testing.T) {
	tests := []struct {
		line int
		want bool
	}{
		{7, false},  // before the loop
		{8, false},  // the range header itself is not the body
		{9, true},   // body
		{10, false}, // closing brace
		{11, false}, // after the loop
		{15, false}, // for header
		{16, true},  // for body (closure start)
		{17, true},  // closure inside the loop
		{25, true},  // range over int
		{31, false}, // closure outside any loop
		{1, false},
	}
	idx := newIndex()
	for _, tc := range tests {
		got, err := idx.InLoop("sha1", "svc.go", tc.line)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("InLoop(line %d) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestInLoop_ReadErrorIsReported(t *testing.T) {
	if _, err := newIndex().InLoop("sha1", "missing.go", 3); err == nil {
		t.Fatal("expected an error when the blob cannot be read")
	}
}

func TestFuncRange_ResolvesRuntimeSymbolsToTheDeclaredFunction(t *testing.T) {
	tests := []struct {
		function   string
		start, end int
	}{
		{"github.com/x/orders.(*Service).List", 6, 12},   // pointer receiver
		{"github.com/x/orders.Repo.Get", 14, 21},         // value receiver
		{"github.com/x/orders.Repo.Get.func1", 14, 21},   // closure -> declared function
		{"github.com/x/orders.Repo.Get.func1.1", 14, 21}, // nested closure
		{"github.com/x/orders.Helper", 23, 27},           // plain function
		{"github.com/x/orders.Helper-range1", 23, 27},    // range-over-func body
		{"orders.Closer.func1", 29, 33},                  // closure without import path prefix
		{"github.com/x/orders.(*Service).List.func2", 6, 12},
	}
	idx := newIndex()
	for _, tc := range tests {
		s, e, found, err := idx.FuncRange("sha1", "svc.go", tc.function)
		if err != nil {
			t.Fatal(err)
		}
		if !found || s != tc.start || e != tc.end {
			t.Errorf("FuncRange(%q) = (%d,%d,%v), want (%d,%d,true)", tc.function, s, e, found, tc.start, tc.end)
		}
	}
}

func TestFuncRange_NotFound(t *testing.T) {
	_, _, found, err := newIndex().FuncRange("sha1", "svc.go", "github.com/x/orders.DoesNotExist")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected found=false")
	}
}
