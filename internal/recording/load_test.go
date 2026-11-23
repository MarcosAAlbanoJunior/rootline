package recording_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/recording"
)

func TestDefaultLoad_Is200RequestsAt50PerSecond(t *testing.T) {
	got := recording.DefaultLoad()
	want := recording.LoadConfig{Requests: 200, Rate: 50, Routes: []recording.LoadRoute{{Route: "/customers/{id}/orders", Weight: 1}}}
	if !reflect.DeepEqual(got, want) {
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

	cfg := recording.LoadConfig{Requests: 45, Rate: 1_000_000, Routes: []recording.LoadRoute{{Route: "/customers/{id}/orders", Weight: 1}}}
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

	cfg := recording.LoadConfig{Requests: 50, Rate: 1_000_000, Routes: []recording.LoadRoute{{Route: "/customers/{id}/orders", Weight: 1}}}
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

func paths(t *testing.T, cfg recording.LoadConfig) []string {
	t.Helper()
	rec := &recorder{status: func(int) int { return 200 }}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	if err := recording.RunLoad(context.Background(), srv.URL, cfg); err != nil {
		t.Fatal(err)
	}
	return rec.paths
}

func TestRunLoad_WeightedRoutesFollowTheDeclaredCycle(t *testing.T) {
	cfg := recording.LoadConfig{Requests: 8, Rate: 1_000_000, Routes: []recording.LoadRoute{
		{Route: "/a/{id}", Weight: 3},
		{Route: "/b/{id}", Weight: 1},
	}}
	got := strings.Join(paths(t, cfg), " ")
	if want := "/a/1 /a/2 /a/3 /b/4 /a/5 /a/6 /a/7 /b/8"; got != want {
		t.Fatalf("paths = %s, want %s", got, want)
	}
}

func TestRunLoad_SameConfigGivesTheSameSequence(t *testing.T) {
	cfg := recording.LoadConfig{Requests: 30, Rate: 1_000_000, Routes: []recording.LoadRoute{
		{Route: "/a/{id}", Weight: 2},
		{Route: "/b/{id}", Weight: 5},
		{Route: "/c", Weight: 1},
	}}
	first, second := strings.Join(paths(t, cfg), " "), strings.Join(paths(t, cfg), " ")
	if first != second {
		t.Fatalf("two runs differ:\n%s\n%s", first, second)
	}
}

func TestRunLoad_RejectsALoadWithoutRoutesOrWithANonPositiveWeight(t *testing.T) {
	cases := map[string][]recording.LoadRoute{
		"no routes":   nil,
		"zero weight": {{Route: "/a", Weight: 0}},
	}
	for name, routes := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := recording.LoadConfig{Requests: 1, Rate: 1, Routes: routes}
			if err := recording.RunLoad(context.Background(), "http://127.0.0.1:1", cfg); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// An invalid load fails before anything is generated or started (no Docker needed to see it).
func TestRecord_InvalidLoadFailsBeforeStartingAnything(t *testing.T) {
	out := filepath.Join(t.TempDir(), "rec")
	err := recording.Record(context.Background(), recording.RecordOptions{
		ScenarioDir: t.TempDir(), SourceDir: t.TempDir(), OutDir: out,
		Load: recording.LoadConfig{Requests: 100, Rate: 20},
	})
	if err == nil || !strings.Contains(err.Error(), "no routes") {
		t.Fatalf("err = %v, want an error about the load having no routes", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("nothing must be created before the load is validated (stat %v)", statErr)
	}
}
