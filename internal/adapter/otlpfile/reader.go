// Package otlpfile reads the OTLP JSON lines written by the Collector file exporter (ADR-0008)
// into domain spans.
package otlpfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

// ReadFile reads the spans of the OTLP JSON lines file at path.
func ReadFile(path string) ([]trace.Span, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read reads OTLP JSON lines (one ExportTraceServiceRequest per line). A malformed line fails the
// whole read with an error naming the line; no partial result is returned.
func Read(r io.Reader) ([]trace.Span, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var spans []trace.Span
	for n := 1; ; n++ {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			got, perr := parseLine(line)
			if perr != nil {
				return nil, fmt.Errorf("otlpfile: line %d: %w", n, perr)
			}
			spans = append(spans, got...)
		}
		if errors.Is(err, io.EOF) {
			return spans, nil
		}
		if err != nil {
			return nil, fmt.Errorf("otlpfile: line %d: %w", n, err)
		}
	}
}

type request struct {
	ResourceSpans []struct {
		Resource struct {
			Attributes []keyValue `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Scope struct {
				Name string `json:"name"`
			} `json:"scope"`
			Spans []struct {
				TraceID           string      `json:"traceId"`
				SpanID            string      `json:"spanId"`
				ParentSpanID      string      `json:"parentSpanId"`
				Name              string      `json:"name"`
				Kind              json.Number `json:"kind"`
				StartTimeUnixNano json.Number `json:"startTimeUnixNano"`
				EndTimeUnixNano   json.Number `json:"endTimeUnixNano"`
				Attributes        []keyValue  `json:"attributes"`
			} `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

type keyValue struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type anyValue struct {
	StringValue *string      `json:"stringValue"`
	IntValue    *json.Number `json:"intValue"`
	BoolValue   *bool        `json:"boolValue"`
	ArrayValue  *struct {
		Values []anyValue `json:"values"`
	} `json:"arrayValue"`
}

func (v anyValue) toGo() (any, bool, error) {
	switch {
	case v.StringValue != nil:
		return *v.StringValue, true, nil
	case v.IntValue != nil:
		n, err := strconv.ParseInt(v.IntValue.String(), 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("invalid intValue %q", v.IntValue.String())
		}
		return n, true, nil
	case v.BoolValue != nil:
		return *v.BoolValue, true, nil
	case v.ArrayValue != nil:
		out := make([]string, 0, len(v.ArrayValue.Values))
		for _, e := range v.ArrayValue.Values {
			if e.StringValue == nil {
				return nil, false, nil // only lists of strings are supported; skip others
			}
			out = append(out, *e.StringValue)
		}
		return out, true, nil
	}
	return nil, false, nil
}

func attrsToMap(kvs []keyValue) (map[string]any, error) {
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		v, ok, err := kv.Value.toGo()
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", kv.Key, err)
		}
		if ok {
			m[kv.Key] = v
		}
	}
	return m, nil
}

func parseNano(n json.Number) (time.Time, error) {
	if n == "" {
		return time.Time{}, nil
	}
	v, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q", n.String())
	}
	return time.Unix(0, v), nil
}

func parseLine(line []byte) ([]trace.Span, error) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, err
	}
	var out []trace.Span
	for _, rs := range req.ResourceSpans {
		res, err := attrsToMap(rs.Resource.Attributes)
		if err != nil {
			return nil, err
		}
		service, _ := res["service.name"].(string)
		version, _ := res["service.version"].(string)
		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				attrs, err := attrsToMap(sp.Attributes)
				if err != nil {
					return nil, err
				}
				start, err := parseNano(sp.StartTimeUnixNano)
				if err != nil {
					return nil, err
				}
				end, err := parseNano(sp.EndTimeUnixNano)
				if err != nil {
					return nil, err
				}
				kind := 0
				if sp.Kind != "" {
					k, err := strconv.Atoi(sp.Kind.String())
					if err != nil {
						return nil, fmt.Errorf("invalid span kind %q", sp.Kind.String())
					}
					kind = k
				}
				out = append(out, trace.Span{
					TraceID: sp.TraceID, SpanID: sp.SpanID, ParentSpanID: sp.ParentSpanID,
					Name: sp.Name, Kind: kind, Scope: ss.Scope.Name,
					Start: start, End: end, Attrs: attrs, Service: service, Version: version,
				})
			}
		}
	}
	return out, nil
}
