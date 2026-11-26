package analysis_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func durations(groups ...[2]float64) []float64 {
	var out []float64
	for _, g := range groups {
		for i := 0; i < int(g[0]); i++ {
			out = append(out, g[1])
		}
	}
	return out
}

func TestCeilingOf(t *testing.T) {
	longTail := []float64{}
	for i := 1; i <= 40; i++ {
		longTail = append(longTail, float64(i*i)) // 1 .. 1600 ms, only the max near the max
	}
	for _, tc := range []struct {
		name   string
		d      []float64
		found  bool
		traces int
	}{
		{"a fifth of the traces cut at 10 s", durations([2]float64{32, 120}, [2]float64{4, 9990}, [2]float64{4, 10000}), true, 8},
		{"traces spread under the max", durations([2]float64{38, 120}, [2]float64{1, 9000}, [2]float64{1, 10000}), false, 1},
		{"long tail with no cut", longTail, false, 1},
		{"every trace cut at 10 s", durations([2]float64{30, 9990}, [2]float64{10, 10000}), true, 40},
		{"below the minimum sample", durations([2]float64{10, 120}, [2]float64{10, 10000}), false, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := analysis.CeilingOf(tc.d)
			if c.Found != tc.found || c.Traces != tc.traces {
				t.Errorf("CeilingOf = %+v, want found %v with %d traces", c, tc.found, tc.traces)
			}
			if c.Found && c.AtMs != 10000 {
				t.Errorf("AtMs = %v, want the max duration", c.AtMs)
			}
		})
	}
}

// lasting sets the duration of the trace's server span.
func lasting(ms float64, fn func(string) []trace.Span) func(string) []trace.Span {
	return func(id string) []trace.Span {
		spans := fn(id)
		spans[0].End = spans[0].Start.Add(time.Duration(ms * float64(time.Millisecond)))
		return spans
	}
}

// cutTrace stopped at the timeout after two of the repeated queries.
func cutTrace(id string) []trace.Span {
	srv := server(id, bad)
	return []trace.Span{srv, query(id, id+"-plus", srv.SpanID, 100, orders, 10),
		query(id, id+"-q0", srv.SpanID, 200, items, 3), query(id, id+"-q1", srv.SpanID, 220, items, 3)}
}

// ceilingSource has 16 complete traces and 20 cut at the 1 s ceiling: with the cut ones counted,
// the repeated query is in fewer than half of the traces.
func ceilingSource() source {
	var src source
	for i := 0; i < 16; i++ {
		src = append(src, lasting(100+float64(i), badTrace)(fmt.Sprintf("bad%02d", i))...)
	}
	for i := 0; i < 20; i++ {
		src = append(src, lasting(1000-float64(i%3), cutTrace)(fmt.Sprintf("cut%02d", i))...)
	}
	return src
}

func TestNPlusOne_TracesAtTheCeilingAreLeftOutOfTheCount(t *testing.T) {
	m := run(t, ceilingSource(), fakeGit{}, symptom)
	if m["status"] != analysis.StatusCauseFound {
		t.Fatalf("status = %v, want the N+1 found in the complete traces", m["status"])
	}
	ev := evidenceOfKind(t, m, "repeated_query")[0]
	if ev["traces_total"] != 16.0 || ev["traces_affected"] != 16.0 {
		t.Errorf("repeated_query = %v, want 16 of 16 traces", ev)
	}
	contains(t, check(t, m, "n_plus_one"), "reason", "20 traces at the ceiling left out")
}

func TestTruncatedTracesCheck_ReportsTheCeiling(t *testing.T) {
	c := check(t, run(t, ceilingSource(), fakeGit{}, symptom), "truncated_traces")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "20 of 36 traces")
	contains(t, c, "reason", "1000.0 ms")
	contains(t, c, "hint", "real latency is higher")
	contains(t, c, "hint", "fewer child spans")
}

func TestTruncatedTracesCheck_WithoutCeilingNothingChanges(t *testing.T) {
	var src source
	for i := 0; i < 36; i++ {
		src = append(src, lasting(100+float64(i*10), badTrace)(fmt.Sprintf("bad%02d", i))...)
	}
	m := run(t, src, fakeGit{}, symptom)
	c := check(t, m, "truncated_traces")
	wantStatus(t, c, analysis.CheckRan)
	contains(t, c, "reason", "no ceiling")
	if ev := evidenceOfKind(t, m, "repeated_query")[0]; ev["traces_total"] != 36.0 {
		t.Errorf("traces_total = %v, want all 36", ev["traces_total"])
	}
}

func TestTruncatedTracesCheck_SkippedBelowTheMinimumSample(t *testing.T) {
	wantStatus(t, check(t, run(t, allSpans(4, 4, badTrace), fakeGit{}, symptom), "truncated_traces"), analysis.CheckSkipped)
}

func TestNPlusOne_WhenEveryTraceStoppedTogetherTheyAreAllCounted(t *testing.T) {
	// A steady 500 ms N+1 looks like a ceiling; with no complete trace left, the N+1 still counts.
	var src source
	for i := 0; i < 30; i++ {
		src = append(src, lasting(500+float64(i)/100, badTrace)(fmt.Sprintf("bad%02d", i))...)
	}
	m := run(t, src, fakeGit{}, symptom)
	if m["status"] != analysis.StatusCauseFound {
		t.Fatalf("status = %v, want the N+1 found", m["status"])
	}
	c := check(t, m, "truncated_traces")
	contains(t, c, "reason", "30 of 30 traces")
	contains(t, c, "hint", "counted them anyway")
}

// cutRequests have no database spans, so the N+1 detector does not run: 20 of 40 stop at 1 s.
func cutRequests(failed int) source {
	return joined(timedTraces("good", baseline, 40, 10, 0), timedTraces("cut", bad, 20, 1000, failed), timedTraces("fast", bad, 20, 100, 0))
}

func TestTruncatedTracesCheck_DoesNotSpeakOfTheNPlusOneWhenItDidNotRun(t *testing.T) {
	m := run(t, cutRequests(0), fakeGit{}, symptom)
	wantStatus(t, check(t, m, "n_plus_one"), analysis.CheckSkipped)
	c := check(t, m, "truncated_traces")
	contains(t, c, "hint", "real latency is higher")
	if hint := c["hint"].(string); strings.Contains(hint, "N+1") {
		t.Errorf("hint = %q, want no word about the N+1", hint)
	}
}

func TestErrorRateCheck_WithACeilingAndNoErrorSaysClientTimeoutsDoNotShow(t *testing.T) {
	c := check(t, run(t, cutRequests(0), fakeGit{}, symptom), "error_rate")
	contains(t, c, "hint", "client gave up")
	contains(t, c, "hint", "If the ceiling")
}

func TestErrorRateCheck_SaysNothingOfClientTimeoutsWhenTheServerRecordedErrors(t *testing.T) {
	c := check(t, run(t, cutRequests(1), fakeGit{}, symptom), "error_rate")
	if hint := c["hint"].(string); strings.Contains(hint, "client gave up") {
		t.Errorf("hint = %q, want no word about client timeouts", hint)
	}
}
