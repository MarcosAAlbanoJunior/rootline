// Command loadgen sends a fixed, sequential load to each spike endpoint and
// prints client-side latency percentiles.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"time"
)

func main() {
	n := flag.Int("n", 300, "measured requests per endpoint")
	warm := flag.Int("warmup", 30, "warmup requests per endpoint")
	label := flag.String("label", "", "label printed with each line")
	flag.Parse()
	endpoints := []string{"explicit", "helper", "errgroup", "rangefunc"}
	for _, ep := range endpoints {
		var lat []time.Duration
		for i := range *warm + *n {
			url := fmt.Sprintf("http://localhost:58080/customers/%d/orders/%s", i%20+1, ep)
			start := time.Now()
			resp, err := http.Get(url)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				fmt.Fprintln(os.Stderr, url, resp.Status)
				os.Exit(1)
			}
			if i >= *warm {
				lat = append(lat, time.Since(start))
			}
		}
		slices.Sort(lat)
		p := func(q float64) time.Duration { return lat[int(q*float64(len(lat)-1))].Round(time.Microsecond) }
		fmt.Printf("%s\t%-10s\tp50=%v\tp95=%v\tn=%d\n", *label, ep, p(0.50), p(0.95), len(lat))
	}
}
