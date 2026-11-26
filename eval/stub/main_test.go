package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func serve(t *testing.T, h http.Handler, path string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d, want 200", path, rec.Code)
	}
}

func TestOnlyTheSecondVersionOfTheRatesIsSlow(t *testing.T) {
	var slept []time.Duration
	h := handler(func(d time.Duration) { slept = append(slept, d) })

	serve(t, h, "/healthz")
	serve(t, h, "/v1/rates")
	if len(slept) != 0 {
		t.Fatalf("the fast endpoints slept %v", slept)
	}
	serve(t, h, "/v2/rates")
	if len(slept) != 1 {
		t.Fatalf("the slow endpoint slept %d times, want once", len(slept))
	}
}

// A constant delay would look like a timeout that cuts the requests.
func TestTheSlowDelayVariesAroundItsMiddleTheSameWayInEveryRun(t *testing.T) {
	delays := func() []time.Duration {
		var slept []time.Duration
		h := handler(func(d time.Duration) { slept = append(slept, d) })
		for range 200 {
			serve(t, h, "/v2/rates")
		}
		return slept
	}
	first := delays()

	var longest, total time.Duration
	for _, d := range first {
		if d < slowDelay-slowSpread || d > slowDelay+slowSpread {
			t.Fatalf("delay %s is outside %s ± %s", d, slowDelay, slowSpread)
		}
		longest, total = max(longest, d), total+d
	}
	nearLongest := 0
	for _, d := range first {
		if d >= longest*98/100 {
			nearLongest++
		}
	}
	if nearLongest*10 >= len(first) {
		t.Errorf("%d of %d delays are within 2%% of the longest", nearLongest, len(first))
	}
	if mean := total / time.Duration(len(first)); mean < 95*time.Millisecond || mean > 105*time.Millisecond {
		t.Errorf("mean delay %s, want about %s", mean, slowDelay)
	}
	if !slices.Equal(first, delays()) {
		t.Error("two runs slept different delays")
	}
}
