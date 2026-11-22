package recording

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthClientHasATimeout(t *testing.T) {
	if healthClient.Timeout <= 0 || healthClient.Timeout > 30*time.Second {
		t.Fatalf("healthClient.Timeout = %s, want a short positive timeout", healthClient.Timeout)
	}
}

func TestWaitHealthyDoesNotHangOnAServerThatNeverAnswers(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)

	client := &http.Client{Timeout: 100 * time.Millisecond}
	done := make(chan error, 1)
	go func() {
		done <- waitHealthy(context.Background(), client, srv.URL, make(chan error), 600*time.Millisecond)
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "did not become healthy") {
			t.Fatalf("got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waitHealthy hung: the probe has no timeout")
	}
}

func TestWaitHealthyReturnsOnceTheServerAnswers200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	err := waitHealthy(context.Background(), srv.Client(), srv.URL, make(chan error), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWaitHealthyReportsAnEarlyExit(t *testing.T) {
	exited := make(chan error, 1)
	exited <- context.Canceled
	err := waitHealthy(context.Background(), http.DefaultClient, "http://127.0.0.1:1", exited, 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "exited early") {
		t.Fatalf("got %v", err)
	}
}
