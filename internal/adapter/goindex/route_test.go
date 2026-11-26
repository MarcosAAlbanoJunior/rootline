// Level-2 location (ADR-0002): route -> registration -> handler function, approximate.
// API: func (i *Index) FindRouteHandler(sha, method, route string) (function, file string, found bool, err error)
// The blob reader may also implement ListGoFiles(sha) ([]string, error).
package goindex_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/goindex"
)

// tree is a fake commit: path -> content. It lists .go files like gitcli.ListGoFiles.
type tree map[string]string

func (t tree) ReadBlob(sha, p string) ([]byte, error) {
	c, ok := t[p]
	if !ok {
		return nil, fmt.Errorf("no blob %s:%s", sha, p)
	}
	return []byte(c), nil
}

func (t tree) ListGoFiles(string) ([]string, error) {
	var out []string
	for p := range t {
		if strings.HasSuffix(p, ".go") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

const demoGoMod = "module example.com/demo-app\n\ngo 1.27\n"

func demoTree() tree {
	return tree{
		"demo-app/go.mod": demoGoMod,
		"demo-app/internal/orders/handler.go": `package orders

import "net/http"

type Handler struct{}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /customers/{id}/orders", h.listOrders)
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {}
`,
	}
}

func findRoute(t *testing.T, tr tree, method, route string) (fn, file string, found bool, err error) {
	t.Helper()
	return goindex.New(tr).FindRouteHandler("sha1", method, route)
}

func TestFindRouteHandlerMethodValueWithPointerReceiver(t *testing.T) {
	fn, file, found, err := findRoute(t, demoTree(), "GET", "/customers/{id}/orders")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if fn != "example.com/demo-app/internal/orders.(*Handler).listOrders" || file != "demo-app/internal/orders/handler.go" {
		t.Fatalf("got %q in %q", fn, file)
	}
}

func TestFindRouteHandlerVariants(t *testing.T) {
	tests := []struct {
		name         string
		files        tree
		method       string
		route        string
		wantFn       string
		wantFile     string
		wantNotFound bool
	}{
		{
			name: "plain function and a pattern without method",
			files: tree{"demo-app/go.mod": demoGoMod, "demo-app/web/web.go": `package web
func Setup(mux Mux) { mux.HandleFunc("/ping", ping) }
func ping() {}
`},
			method: "GET", route: "/ping",
			wantFn: "example.com/demo-app/web.ping", wantFile: "demo-app/web/web.go",
		},
		{
			name: "value receiver",
			files: tree{"demo-app/go.mod": demoGoMod, "demo-app/api/api.go": `package api
type S struct{}
func (s S) Routes(mux Mux) { mux.HandleFunc("GET /x", s.get) }
func (s S) get() {}
`},
			method: "GET", route: "/x",
			wantFn: "example.com/demo-app/api.S.get", wantFile: "demo-app/api/api.go",
		},
		{
			name: "wrapped in http.HandlerFunc",
			files: tree{"demo-app/go.mod": demoGoMod, "demo-app/api/api.go": `package api
func Routes(mux Mux, h *H) { mux.Handle("GET /x", http.HandlerFunc(h.serve)) }
type H struct{}
func (h *H) serve() {}
`},
			method: "GET", route: "/x",
			wantFn: "example.com/demo-app/api.(*H).serve", wantFile: "demo-app/api/api.go",
		},
		{
			name: "handler declared in another file of the same package",
			files: tree{
				"demo-app/go.mod": demoGoMod,
				"demo-app/api/routes.go": `package api
func Routes(mux Mux) { mux.HandleFunc("GET /x", get) }
`,
				"demo-app/api/get.go": "package api\nfunc get() {}\n",
			},
			method: "GET", route: "/x",
			wantFn: "example.com/demo-app/api.get", wantFile: "demo-app/api/get.go",
		},
		{
			name: "module at the repository root",
			files: tree{"go.mod": "module example.com/root\n", "main.go": `package main
func main() { mux.HandleFunc("GET /x", handler) }
func handler() {}
`},
			method: "GET", route: "/x",
			wantFn: "example.com/root.handler", wantFile: "main.go",
		},
		{
			name:   "a different method does not match",
			files:  tree{"demo-app/go.mod": demoGoMod, "demo-app/a/a.go": "package a\nfunc R(m Mux) { m.HandleFunc(\"POST /x\", h) }\nfunc h() {}\n"},
			method: "GET", route: "/x", wantNotFound: true,
		},
		{
			name:   "no registration at all",
			files:  tree{"demo-app/go.mod": demoGoMod, "demo-app/a/a.go": "package a\nfunc h() {}\n"},
			method: "GET", route: "/x", wantNotFound: true,
		},
		{
			name:   "handler declared elsewhere is not guessed",
			files:  tree{"demo-app/go.mod": demoGoMod, "demo-app/a/a.go": "package a\nfunc R(m Mux) { m.HandleFunc(\"GET /x\", other.Handler) }\n"},
			method: "GET", route: "/x", wantNotFound: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fn, file, found, err := findRoute(t, tc.files, tc.method, tc.route)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNotFound {
				if found {
					t.Fatalf("want not found, got %q in %q", fn, file)
				}
				return
			}
			if !found || fn != tc.wantFn || file != tc.wantFile {
				t.Fatalf("found=%v got %q in %q, want %q in %q", found, fn, file, tc.wantFn, tc.wantFile)
			}
		})
	}
}

func TestFindRouteHandlerAmbiguityIsAnError(t *testing.T) {
	t.Run("two registrations with different handlers", func(t *testing.T) {
		tr := tree{"demo-app/go.mod": demoGoMod,
			"demo-app/a/a.go": "package a\nfunc R(m Mux) { m.HandleFunc(\"GET /x\", one) }\nfunc one() {}\n",
			"demo-app/b/b.go": "package b\nfunc R(m Mux) { m.HandleFunc(\"GET /x\", two) }\nfunc two() {}\n",
		}
		if _, _, found, err := findRoute(t, tr, "GET", "/x"); err == nil || found {
			t.Fatalf("want an ambiguity error, found=%v err=%v", found, err)
		}
	})
	t.Run("the same handler registered twice is fine", func(t *testing.T) {
		tr := tree{"demo-app/go.mod": demoGoMod,
			"demo-app/a/a.go": "package a\nfunc R(m Mux) { m.HandleFunc(\"GET /x\", one); m.HandleFunc(\"/x\", one) }\nfunc one() {}\n",
		}
		fn, _, found, err := findRoute(t, tr, "GET", "/x")
		if err != nil || !found || fn != "example.com/demo-app/a.one" {
			t.Fatalf("got %q found=%v err=%v", fn, found, err)
		}
	})
	t.Run("handler name declared twice in the package", func(t *testing.T) {
		tr := tree{"demo-app/go.mod": demoGoMod,
			"demo-app/a/a.go": "package a\nfunc R(m Mux, p *P, q *Q) { m.HandleFunc(\"GET /x\", p.get) }\ntype P struct{}\ntype Q struct{}\nfunc (p *P) get() {}\nfunc (q *Q) get() {}\n",
		}
		if _, _, found, err := findRoute(t, tr, "GET", "/x"); err == nil || found {
			t.Fatalf("want an ambiguity error, found=%v err=%v", found, err)
		}
	})
}

func TestFindRouteHandlerNeedsAReaderThatListsFiles(t *testing.T) {
	idx := goindex.New(fakeBlobs{"sha1:a.go": "package a\n"}) // only ReadBlob
	if _, _, _, err := idx.FindRouteHandler("sha1", "GET", "/x"); err == nil {
		t.Fatal("expected an error: the reader cannot list files")
	}
}

func TestFindRouteHandlerFailsWhenNoGoModExists(t *testing.T) {
	tr := tree{"a/a.go": "package a\nfunc R(m Mux) { m.HandleFunc(\"GET /x\", h) }\nfunc h() {}\n"}
	if _, _, _, err := findRoute(t, tr, "GET", "/x"); err == nil {
		t.Fatal("expected an error: the package path cannot be derived without a go.mod")
	}
}

// greppingTree counts the reads and the grep calls of a reader that supports FileGrepper.
type greppingTree struct {
	tree
	reads  map[string]int
	greps  int
	needle string
}

func (g *greppingTree) ReadBlob(sha, p string) ([]byte, error) {
	g.reads[p]++
	return g.tree.ReadBlob(sha, p)
}

func (g *greppingTree) GrepGoFiles(_, needle string) ([]string, error) {
	g.greps++
	g.needle = needle
	var out []string
	for p, c := range g.tree {
		if strings.HasSuffix(p, ".go") && strings.Contains(c, needle) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

func TestFindRouteHandlerReadsOnlyGrepCandidatesAndMemoizes(t *testing.T) {
	tr := demoTree()
	for i := 0; i < 50; i++ { // many unrelated Go files: none may be read
		tr[fmt.Sprintf("demo-app/pkg%d/f.go", i)] = "package p\n"
	}
	g := &greppingTree{tree: tr, reads: map[string]int{}}
	idx := goindex.New(g)

	for i := 0; i < 3; i++ { // once per candidate, in a real analysis
		fn, _, found, err := idx.FindRouteHandler("sha1", "GET", "/customers/{id}/orders")
		if err != nil || !found || !strings.HasSuffix(fn, "(*Handler).listOrders") {
			t.Fatalf("call %d: %q found=%v err=%v", i, fn, found, err)
		}
	}
	if g.greps != 1 || g.needle != "/orders" {
		t.Fatalf("grep ran %d times with %q, want once with the last segment of the route (memoized)", g.greps, g.needle)
	}
	for p, n := range g.reads {
		if strings.Contains(p, "/pkg") {
			t.Errorf("unrelated file %s was read %d times", p, n)
		}
	}
	if total := len(g.reads); total > 4 { // handler.go and go.mod only, a few times each
		t.Errorf("read %d distinct files, want only the handler file and go.mod: %v", total, g.reads)
	}
}

func TestFindRouteHandlerMemoizesNotFoundToo(t *testing.T) {
	g := &greppingTree{tree: demoTree(), reads: map[string]int{}}
	idx := goindex.New(g)
	for i := 0; i < 3; i++ {
		if _, _, found, err := idx.FindRouteHandler("sha1", "GET", "/nothing"); err != nil || found {
			t.Fatalf("found=%v err=%v", found, err)
		}
	}
	if g.greps != 1 {
		t.Fatalf("grep ran %d times, want 1", g.greps)
	}
}

const nestedChiRouter = `package api

func (a *api) routes() {
	r := chi.NewRouter()
	r.Route("/rooms", func(r chi.Router) {
		r.Get("/", a.listRooms)
		r.Route("/{roomID}", func(r chi.Router) {
			r.Get("/", a.getRoom)
			r.Get("/messages", a.listMessages)
			r.Post("/messages", a.postMessage)
		})
	})
}

type api struct{}

func (a *api) listRooms()    {}
func (a *api) getRoom()      {}
func (a *api) listMessages() {}
func (a *api) postMessage()  {}
`

func TestFindRouteHandlerAddsThePrefixesOfNestedRouteGroups(t *testing.T) {
	tr := tree{"demo-app/go.mod": demoGoMod, "demo-app/api/api.go": nestedChiRouter}
	fn, file, found, err := findRoute(t, tr, "POST", "/rooms/{roomID}/messages")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if fn != "example.com/demo-app/api.(*api).postMessage" || file != "demo-app/api/api.go" {
		t.Fatalf("got %q in %q", fn, file)
	}
}

func TestFindRouteHandlerInChiRouteGroups(t *testing.T) {
	api := func(body string) string {
		return "package api\nfunc R(r Router) {\n" + body + "\n}\nfunc one() {}\nfunc two() {}\n"
	}
	tests := []struct {
		name   string
		src    string
		method string
		route  string
		want   string // handler name; empty means not found
	}{
		{name: "the other method of the same path", src: nestedChiRouter, method: "GET", route: "/rooms/{roomID}/messages", want: "(*api).listMessages"},
		{name: "root of a group", src: nestedChiRouter, method: "GET", route: "/rooms", want: "(*api).listRooms"},
		{name: "root of a nested group, with a trailing slash", src: nestedChiRouter, method: "GET", route: "/rooms/{roomID}/", want: "(*api).getRoom"},
		{name: "method not registered in the group", src: nestedChiRouter, method: "DELETE", route: "/rooms/{roomID}/messages"},
		{name: "route that does not exist", src: nestedChiRouter, method: "GET", route: "/rooms/{roomID}/files"},
		{name: "the path inside a group is not the whole route", src: nestedChiRouter, method: "GET", route: "/messages"},
		{
			name:   "group inside a middleware group",
			src:    api(`r.Route("/a", func(r Router) { r.Group(func(r Router) { r.With(mw).Get("/b", one) }) })`),
			method: "GET", route: "/a/b", want: "one",
		},
		{
			name:   "group mounted at the root",
			src:    api(`r.Route("/", func(r Router) { r.Get("/b", one) })`),
			method: "GET", route: "/b", want: "one",
		},
		{
			name:   "group whose prefix is not a literal",
			src:    api(`r.Route(prefix, func(r Router) { r.Get("/b", one) })`),
			method: "GET", route: "/b",
		},
		{
			name:   "a router declared elsewhere is not the handler of its group",
			src:    api(`r.Route("/a", func(r Router) { r.Route("/b", one) })`),
			method: "GET", route: "/a/b",
		},
		{
			name:   "a router's own Route method outside a group registers a handler",
			src:    api(`app.Route("/b", one)`),
			method: "GET", route: "/b", want: "one",
		},
		{
			name:   "method in the pattern inside a group",
			src:    api(`r.Route("/a", func(r Router) { r.HandleFunc("GET /b", one); r.HandleFunc("POST /b", two) })`),
			method: "POST", route: "/a/b", want: "two",
		},
		{
			name:   "verbs outside a group follow the method too",
			src:    api(`r.Get("/b", one); r.Post("/b", two)`),
			method: "POST", route: "/b", want: "two",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fn, _, found, err := findRoute(t, tree{"demo-app/go.mod": demoGoMod, "demo-app/api/api.go": tc.src}, tc.method, tc.route)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.want != "" {
				want = "example.com/demo-app/api." + tc.want
			}
			if found != (want != "") || fn != want {
				t.Fatalf("found=%v got %q, want %q", found, fn, want)
			}
		})
	}
}

func TestFindRouteHandlerTwoGroupsServingTheSameRouteAreAmbiguous(t *testing.T) {
	tr := tree{"demo-app/go.mod": demoGoMod,
		"demo-app/a/a.go": "package a\nfunc R(r Router) { r.Route(\"/x\", func(r Router) { r.Get(\"/y\", one) }) }\nfunc one() {}\n",
		"demo-app/b/b.go": "package b\nfunc R(r Router) { r.Get(\"/x/y\", two) }\nfunc two() {}\n",
	}
	if _, _, found, err := findRoute(t, tr, "GET", "/x/y"); err == nil || found {
		t.Fatalf("want an ambiguity error, found=%v err=%v", found, err)
	}
}

// The search is by a part of the route, so it also reaches files that have nothing to do with it.
func TestFindRouteHandlerReportsAParseErrorOnlyWhenNothingElseMatches(t *testing.T) {
	broken := "package {{.Name}}\n// see /y\n"
	t.Run("the route is registered in a valid file", func(t *testing.T) {
		tr := tree{"demo-app/go.mod": demoGoMod,
			"demo-app/a/a.go":    "package a\nfunc R(m Mux) { m.HandleFunc(\"GET /x/y\", one) }\nfunc one() {}\n",
			"demo-app/gen/tm.go": broken,
		}
		fn, _, found, err := findRoute(t, tr, "GET", "/x/y")
		if err != nil || !found || fn != "example.com/demo-app/a.one" {
			t.Fatalf("got %q found=%v err=%v", fn, found, err)
		}
	})
	t.Run("nothing matches", func(t *testing.T) {
		tr := tree{"demo-app/go.mod": demoGoMod, "demo-app/gen/tm.go": broken}
		if _, _, found, err := findRoute(t, tr, "GET", "/x/y"); err == nil || found {
			t.Fatalf("want the parse error, found=%v err=%v", found, err)
		}
	})
}
