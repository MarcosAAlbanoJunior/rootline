package recording

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

const customerCount = 20

// DefaultLoad is the load of a scenario without a load block (fixture.DefaultLoad).
func DefaultLoad() LoadConfig {
	return loadConfigOf(fixture.DefaultLoad())
}

// loadConfigOf converts the scenario's load block into the load the recording sends.
func loadConfigOf(l fixture.Load) LoadConfig {
	cfg := LoadConfig{Requests: l.Requests, Rate: l.RatePerSecond}
	for _, r := range l.Routes {
		cfg.Routes = append(cfg.Routes, LoadRoute{Route: r.Route, Weight: r.Weight})
	}
	return cfg
}

// RunLoad sends cfg.Requests GET requests at cfg.Rate per second. The cycle repeats each route
// Weight times, in the declared order; request i (from 0) goes to cycle[i mod len(cycle)], with
// "{id}" replaced by the customer id i mod 20 + 1. There is no randomness, so two runs with the
// same config send the same paths. It aborts on the first reply that is not 200.
func RunLoad(ctx context.Context, baseURL string, cfg LoadConfig) error {
	cycle, err := cfg.cycle()
	if err != nil {
		return err
	}
	interval := time.Second / time.Duration(cfg.Rate)
	client := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(max(interval, time.Nanosecond))
	defer ticker.Stop()

	for i := 0; i < cfg.Requests; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
		route := cycle[i%len(cycle)]
		url := baseURL + strings.ReplaceAll(route, "{id}", strconv.Itoa(i%customerCount+1))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("recording: request %d to %s: %w", i+1, url, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("recording: request %d to %s: status %d", i+1, url, resp.StatusCode)
		}
	}
	return nil
}

// cycle validates the config and returns its route cycle.
func (cfg LoadConfig) cycle() ([]string, error) {
	if cfg.Requests <= 0 || cfg.Rate <= 0 {
		return nil, fmt.Errorf("recording: invalid load config %+v", cfg)
	}
	return routeCycle(cfg.Routes)
}

// routeCycle repeats each route Weight times, in the declared order.
func routeCycle(routes []LoadRoute) ([]string, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("recording: load has no routes")
	}
	var cycle []string
	for _, r := range routes {
		if r.Weight <= 0 {
			return nil, fmt.Errorf("recording: load route %q has weight %d", r.Route, r.Weight)
		}
		for range r.Weight {
			cycle = append(cycle, r.Route)
		}
	}
	return cycle, nil
}
