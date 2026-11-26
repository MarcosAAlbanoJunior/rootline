package gitcli_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/gitcli"
	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/goindex"
)

// Level 2 end to end over a real repository: goindex reads blobs and lists Go files through gitcli.
func TestRouteHandlerLookupOverARealRepository(t *testing.T) {
	r := newRepo(t)
	r.write("demo-app/go.mod", "module example.com/demo-app\n\ngo 1.27\n")
	r.write("demo-app/internal/orders/handler.go", `package orders

import "net/http"

type Handler struct{}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /customers/{id}/orders", h.listOrders)
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {}
`)
	r.write("demo-app/internal/orders/handler_test.go", "package orders\n\nfunc init() { mux.HandleFunc(\"GET /customers/{id}/orders\", other) }\n")
	r.write("demo-app/testdata/x.go", "package x\n\nfunc init() { mux.HandleFunc(\"GET /customers/{id}/orders\", other) }\n")
	sha := r.commit("c1")

	idx := goindex.New(gitcli.New(r.dir))
	fn, file, found, err := idx.FindRouteHandler(sha, "GET", "/customers/{id}/orders")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if fn != "example.com/demo-app/internal/orders.(*Handler).listOrders" || file != "demo-app/internal/orders/handler.go" {
		t.Fatalf("got %q in %q (test files and testdata must be ignored)", fn, file)
	}
}

// The whole route is written nowhere: the grep has to find the file by a part of it.
func TestRouteHandlerLookupInNestedRouteGroupsOverARealRepository(t *testing.T) {
	r := newRepo(t)
	r.write("go.mod", "module example.com/chat\n\ngo 1.27\n")
	r.write("httpapi/server.go", `package httpapi

func (a *api) routes(r Router) {
	r.Route("/rooms", func(r Router) {
		r.Route("/{roomID}", func(r Router) {
			r.Get("/messages", a.listMessages)
			r.Post("/messages", a.postMessage)
		})
	})
}
`)
	r.write("httpapi/messages.go", `package httpapi

type api struct{}

func (a *api) listMessages() {}
func (a *api) postMessage()  {}
`)
	sha := r.commit("c1")

	idx := goindex.New(gitcli.New(r.dir))
	fn, file, found, err := idx.FindRouteHandler(sha, "POST", "/rooms/{roomID}/messages")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if fn != "example.com/chat/httpapi.(*api).postMessage" || file != "httpapi/messages.go" {
		t.Fatalf("got %q in %q", fn, file)
	}
}
