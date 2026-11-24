package recording

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/planfile"
	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
	"github.com/MarcosAAlbanoJunior/rootline/internal/explain"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// versionQuery is one distinct normalized query of a version and the original text of a
// representative span, the one that is explained.
type versionQuery struct{ Normalized, Text string }

// queriesOf returns the distinct normalized queries of the query spans of a version, from every
// route (the recording does not depend on what the analysis will look at; ADR-0025), sorted by the
// normalized text. The representative text is the first span's.
func queriesOf(spans []trace.Span, version string) []versionQuery {
	seen := map[string]bool{}
	var out []versionQuery
	for _, s := range spans {
		if s.Version != version || detector.PhaseOf(s) != detector.PhaseQuery {
			continue
		}
		text, _ := s.String("db.query.text")
		n := detector.Normalize(text)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, versionQuery{Normalized: n, Text: text})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Normalized < out[j].Normalized })
	return out
}

// versionTraces fails when the traces taken after a version have none of its spans: they were lost
// (the Collector was not receiving), and its plans would silently be missing too.
func versionTraces(spans []trace.Span, version string) error {
	for _, s := range spans {
		if s.Version == version {
			return nil
		}
	}
	return fmt.Errorf("the Collector received no span of version %s", version)
}

// planLine builds the plans.jsonl line of an explained query: the raw plan, compacted, and the
// estimated size of the tables that appear in it.
func planLine(version, query, explainOut string, sizes map[string]float64) (planfile.Line, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(strings.TrimSpace(explainOut))); err != nil {
		return planfile.Line{}, fmt.Errorf("EXPLAIN output is not JSON: %w", err)
	}
	var roots []json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &roots); err != nil {
		return planfile.Line{}, fmt.Errorf("EXPLAIN output is not a plan list: %w", err)
	}
	rel := map[string]float64{}
	for _, r := range roots {
		collectRelations(r, func(name string) {
			if n, ok := sizes[name]; ok {
				rel[name] = n
			}
		})
	}
	return planfile.Line{Version: version, Query: query, Plan: buf.Bytes(), RelTuples: rel, GenericPlan: true}, nil
}

// collectRelations calls f with every "Relation Name" found in a JSON plan.
func collectRelations(raw json.RawMessage, f func(string)) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return
	}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			if name, ok := t["Relation Name"].(string); ok {
				f(name)
			}
			for _, c := range t {
				walk(c)
			}
		case []any:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(v)
}

// sqlRunner runs SQL statements in one session and returns their output.
type sqlRunner func(ctx context.Context, statements ...string) (string, error)

// psql runs SQL statements, each in its own -c, in one psql session inside the Postgres container,
// and returns the unaligned, tuples-only output.
func (s *stack) psql(ctx context.Context, statements ...string) (string, error) {
	args := []string{"exec", "-T", "postgres", "psql", "-U", "postgres", "-d", "shop", "-X", "-q", "-A", "-t",
		"-v", "ON_ERROR_STOP=1"}
	for _, st := range statements {
		args = append(args, "-c", st)
	}
	return s.compose(ctx, args...)
}

// serverVersion is the Postgres server_version.
func serverVersion(ctx context.Context, run sqlRunner) (string, error) {
	out, err := run(ctx, "SHOW server_version")
	return strings.TrimSpace(out), err
}

// tableSizes runs ANALYZE and returns pg_class.reltuples of every table of the public schema.
func tableSizes(ctx context.Context, run sqlRunner) (map[string]float64, error) {
	out, err := run(ctx, "ANALYZE",
		`SELECT coalesce(json_object_agg(c.relname, c.reltuples), '{}') FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')`)
	if err != nil {
		return nil, err
	}
	sizes := map[string]float64{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &sizes); err != nil {
		return nil, fmt.Errorf("recording: reading reltuples: %w", err)
	}
	return sizes, nil
}

// explainVersion records the plan of every distinct query of a version, right after its load
// (ADR-0025): ANALYZE, then EXPLAIN (GENERIC_PLAN, FORMAT JSON) of each one inside a read-only
// transaction. A query that must not or could not be explained gets a line with the reason.
func explainVersion(ctx context.Context, run sqlRunner, version string, spans []trace.Span) ([]planfile.Line, error) {
	sizes, err := tableSizes(ctx, run)
	if err != nil {
		return nil, err
	}
	var lines []planfile.Line
	for _, q := range queriesOf(spans, version) {
		if reason := explain.Unexplainable(q.Text); reason != "" {
			lines = append(lines, planfile.Line{Version: version, Query: q.Normalized, Unexplainable: reason})
			continue
		}
		out, err := run(ctx, "SET default_transaction_read_only = on",
			"EXPLAIN (GENERIC_PLAN, FORMAT JSON) "+strings.TrimSuffix(strings.TrimSpace(q.Text), ";"))
		if err != nil {
			lines = append(lines, planfile.Line{Version: version, Query: q.Normalized, Unexplainable: "explain failed: " + oneLine(err.Error())})
			continue
		}
		l, err := planLine(version, q.Normalized, out, sizes)
		if err != nil {
			lines = append(lines, planfile.Line{Version: version, Query: q.Normalized, Unexplainable: oneLine(err.Error())})
			continue
		}
		lines = append(lines, l)
	}
	return lines, nil
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// writePlans writes the plans.jsonl lines.
func writePlans(path string, lines []planfile.Line) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			return err
		}
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
