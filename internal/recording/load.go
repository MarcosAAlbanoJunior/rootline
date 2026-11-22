package recording

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const customerCount = 20

// DefaultLoad is the standard load: 200 requests at 50 per second.
func DefaultLoad() LoadConfig {
	return LoadConfig{Requests: 200, Rate: 50, Route: "/customers/{id}/orders"}
}

// RunLoad sends cfg.Requests GET requests to baseURL+route at cfg.Rate per second, replacing
// "{id}" with the customer id cycling 1..20. It aborts on the first reply that is not 200.
func RunLoad(ctx context.Context, baseURL string, cfg LoadConfig) error {
	if cfg.Requests <= 0 || cfg.Rate <= 0 {
		return fmt.Errorf("recording: invalid load config %+v", cfg)
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
		url := baseURL + strings.ReplaceAll(cfg.Route, "{id}", strconv.Itoa(i%customerCount+1))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("recording: request %d to %s: %w", i+1, url, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("recording: request %d to %s: status %d", i+1, url, resp.StatusCode)
		}
	}
	return nil
}
