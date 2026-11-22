package gitcli_test

import (
	"reflect"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
)

func TestGrepGoFilesFindsCandidatesWithOneProcessAndSkipsTestsAndIgnoredFolders(t *testing.T) {
	r := newRepo(t)
	r.write("a/a.go", "package a\n// GET /x\n")
	r.write("b/b.go", "package b\n// nothing here\n")
	r.write("a/a_test.go", "package a\n// GET /x\n")
	r.write("testdata/t.go", "package t\n// GET /x\n")
	r.write("vendor/v/v.go", "package v\n// GET /x\n")
	r.write("notes.md", "GET /x\n")
	r.write("dir with space/s.go", "package s\n// GET /x\n")
	sha := r.commit("c1")
	g := gitcli.New(r.dir)

	got, err := g.GrepGoFiles(sha, "GET /x")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/a.go", "dir with space/s.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	none, err := g.GrepGoFiles(sha, "no such text anywhere")
	if err != nil || len(none) != 0 {
		t.Fatalf("no match is not an error: %v %v", none, err)
	}
}

func TestGrepGoFilesTreatsTheNeedleAsDataNotAnOption(t *testing.T) {
	r := newRepo(t)
	r.write("a.go", "package a\n// --output=/tmp/x\n")
	sha := r.commit("c1")
	g := gitcli.New(r.dir)

	got, err := g.GrepGoFiles(sha, "--output=/tmp/x")
	if err != nil || !reflect.DeepEqual(got, []string{"a.go"}) {
		t.Fatalf("a needle starting with '-' must be searched literally: %v %v", got, err)
	}
	for _, bad := range []string{"", "a\nb", "a\x00b"} {
		if _, err := g.GrepGoFiles(sha, bad); err == nil {
			t.Errorf("needle %q must be refused", bad)
		}
	}
	if _, err := g.GrepGoFiles("--output=/tmp/x", "x"); err == nil {
		t.Error("invalid SHA must be refused")
	}
}
