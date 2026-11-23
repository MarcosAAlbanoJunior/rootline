package recording_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/recording"
)

func TestDefaultLoad_Is200RequestsAt50PerSecond(t *testing.T) {
	got := recording.DefaultLoad()
	want := recording.LoadConfig{Requests: 200, Rate: 50, Route: "/customers/{id}/orders"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

type recorder struct {
	mu     sync.Mutex
	paths  []string
	status func(n int) int // n is the 1-based request number
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.paths = append(r.paths, req.URL.Path)
	n := len(r.paths)
	r.mu.Unlock()
	w.WriteHeader(r.status(n))
}

func TestRunLoad_SendsFixedCountCyclingCustomers(t *testing.T) {
	rec := &recorder{status: func(int) int { return 200 }}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	cfg := recording.LoadConfig{Requests: 45, Rate: 1_000_000, Route: "/customers/{id}/orders"}
	if err := recording.RunLoad(context.Background(), srv.URL, cfg); err != nil {
		t.Fatal(err)
	}
	if len(rec.paths) != 45 {
		t.Fatalf("server saw %d requests, want 45", len(rec.paths))
	}
	for i, p := range rec.paths {
		want := fmt.Sprintf("/customers/%d/orders", i%20+1)
		if p != want {
			t.Fatalf("request %d path = %q, want %q", i+1, p, want)
		}
	}
}

func TestRunLoad_AbortsOnFirstNon200(t *testing.T) {
	rec := &recorder{status: func(n int) int {
		if n == 3 {
			return 500
		}
		return 200
	}}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	cfg := recording.LoadConfig{Requests: 50, Rate: 1_000_000, Route: "/customers/{id}/orders"}
	err := recording.RunLoad(context.Background(), srv.URL, cfg)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention the status, got %q", err)
	}
	if len(rec.paths) != 3 {
		t.Fatalf("load must stop at the first failure: server saw %d requests, want 3", len(rec.paths))
	}
}
