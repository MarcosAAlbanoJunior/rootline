// Command stub is the external HTTP service of the evaluation (ADR-0031): a rates API with a fast
// endpoint and a slow one, for the scenarios whose cause is a call that got slower, and a plan API
// whose second version is only a little slower, for the distractors. It runs as a service of
// eval/compose.yml and uses only the standard library, so its image builds from this file alone.
package main

import (
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// The slow endpoint takes slowDelay on average, from slowDelay-slowSpread to slowDelay+slowSpread
// in turn: a constant delay would look like a timeout cutting the requests (ADR-0030).
const (
	slowDelay  = 100 * time.Millisecond
	slowSpread = 20 * time.Millisecond
)

// planDelay is what the second version of the plan takes: more than twice the first, and little
// next to a cause.
const planDelay = 2 * time.Millisecond

func main() {
	srv := &http.Server{Addr: ":8090", Handler: handler(time.Sleep), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func handler(sleep func(time.Duration)) http.Handler {
	reply := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body) // the client is gone; there is nothing to do
	}
	var slowCalls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /v1/rates", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, `{"standard_cents":1200}`)
	})
	mux.HandleFunc("GET /v2/rates", func(w http.ResponseWriter, _ *http.Request) {
		steps := int64(2*slowSpread/time.Millisecond) + 1
		sleep(slowDelay - slowSpread + time.Duration((slowCalls.Add(1)-1)%steps)*time.Millisecond)
		reply(w, `{"standard_cents":1200,"carriers":[{"name":"north","cents":1200}]}`)
	})
	mux.HandleFunc("GET /v1/plan", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, `{"plan":"standard"}`)
	})
	mux.HandleFunc("GET /v2/plan", func(w http.ResponseWriter, _ *http.Request) {
		sleep(planDelay)
		reply(w, `{"plan":"standard"}`)
	})
	return mux
}
