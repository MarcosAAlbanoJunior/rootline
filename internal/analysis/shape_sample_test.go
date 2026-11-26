package analysis_test

import "testing"

func TestTraceShapeCheck_SaysTheSampleIsTooSmallToTellARegression(t *testing.T) {
	src := append(traces("good", baseline, 10, func(int) (float64, float64) { return 5, 3 }),
		traces("bad", bad, 10, func(int) (float64, float64) { return 150, 148 })...)
	c := check(t, run(t, src, fakeGit{}, symptom), "trace_shape")

	wantStatus(t, c, "not_applicable")
	contains(t, c, "reason", "fewer than 30 traces in a version")
}
