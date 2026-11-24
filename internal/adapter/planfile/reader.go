// Package planfile reads the execution plans recorded per version in a plans.jsonl (ADR-0025).
package planfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
)

// Line is one line of plans.jsonl: the plan of one normalized query at one version, or the reason
// it could not be explained.
type Line struct {
	Version       string             `json:"version"`
	Query         string             `json:"query"`
	Plan          json.RawMessage    `json:"plan,omitempty"`
	RelTuples     map[string]float64 `json:"reltuples,omitempty"`
	Unexplainable string             `json:"unexplainable,omitempty"`
}

type key struct{ version, query string }

// Plans serves the recorded plans by version and normalized query.
type Plans struct{ byKey map[key]detector.QueryPlan }

// Plan returns the recorded plan of the query at sha; found is false when it was not recorded.
func (p *Plans) Plan(sha, query string) (detector.QueryPlan, bool, error) {
	qp, ok := p.byKey[key{sha, query}]
	return qp, ok, nil
}

// ReadFile reads the plans.jsonl at path.
func ReadFile(path string) (*Plans, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only: a close error loses nothing
	return Read(f)
}

// Read reads plans.jsonl lines. A malformed line fails the whole read with an error naming the line.
func Read(r io.Reader) (*Plans, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	p := &Plans{byKey: map[key]detector.QueryPlan{}}
	for n := 1; ; n++ {
		raw, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(raw)) > 0 {
			var l Line
			if perr := json.Unmarshal(raw, &l); perr != nil {
				return nil, fmt.Errorf("planfile: line %d: %w", n, perr)
			}
			if l.Version == "" || l.Query == "" {
				return nil, fmt.Errorf("planfile: line %d: version and query are required", n)
			}
			p.byKey[key{l.Version, l.Query}] = detector.QueryPlan{Plan: l.Plan, RelTuples: l.RelTuples, Unexplainable: l.Unexplainable}
		}
		if errors.Is(err, io.EOF) {
			return p, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
