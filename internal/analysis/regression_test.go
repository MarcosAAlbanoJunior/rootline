package analysis_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/analysis"
)

// sample has n traces of ms milliseconds each, errors of them failed.
func sample(n int, ms float64, errors int) analysis.VersionSample {
	s := analysis.VersionSample{Errors: errors}
	for i := 0; i < n; i++ {
		s.DurationsMs = append(s.DurationsMs, ms)
	}
	return s
}

// withTail replaces the slowest share of the sample (from index from on) with ms.
func withTail(s analysis.VersionSample, from int, ms float64) analysis.VersionSample {
	d := append([]float64(nil), s.DurationsMs...)
	for i := from; i < len(d); i++ {
		d[i] = ms
	}
	s.DurationsMs = d
	return s
}

func TestRegress_Rule(t *testing.T) {
	for _, tc := range []struct {
		name          string
		base, bad     analysis.VersionSample
		latency, errs bool
		enough        bool
	}{
		{"p50 exactly 1.5x with 5 ms more", sample(30, 10, 0), sample(30, 15, 0), true, false, true},
		{"p50 just below 1.5x", sample(30, 10, 0), sample(30, 14.9, 0), false, false, true},
		{"p95 exactly 2x", withTail(sample(40, 10, 0), 36, 20), withTail(sample(40, 10, 0), 36, 40), true, false, true},
		{"p95 just below 2x", withTail(sample(40, 10, 0), 36, 20), withTail(sample(40, 10, 0), 36, 39.9), false, false, true},
		{"p50 exactly 2x with 2 ms more", sample(30, 2, 0), sample(30, 4, 0), true, false, true},
		{"p50 just below 2x with 2 ms more", sample(30, 2.2, 0), sample(30, 4.39, 0), false, false, true},
		{"p50 2x but just under 2 ms more", sample(30, 1.9, 0), sample(30, 3.8, 0), false, false, true},
		{"doubled p50 reaching exactly 3 ms", sample(30, 0.9, 0), sample(30, 3, 0), true, false, true},
		{"doubled p50 that stays just under 3 ms", sample(30, 0.6, 0), sample(30, 2.99, 0), false, false, true},
		{"exactly 2 ms more despite float rounding", sample(30, 1.3, 0), sample(30, 3.3, 0), true, false, true},
		{"2.9x with 1.9 ms more does not regress", sample(30, 1, 0), sample(30, 2.9, 0), false, false, true},
		{"fast operation whose p50 quadruples with a steady p95", withTail(sample(40, 1.06, 0), 36, 4.9), withTail(sample(40, 4.01, 0), 36, 4.9), true, false, true},
		{"0.3 to 0.6 ms does not regress", sample(30, 0.3, 0), sample(30, 0.6, 0), false, false, true},
		{"29 traces in the bad version is not enough", sample(30, 10, 0), sample(29, 100, 0), false, false, false},
		{"29 traces in the baseline is not enough", sample(29, 10, 0), sample(30, 100, 0), false, false, false},
		{"error rate 0 to 7.5% regresses", sample(40, 10, 0), sample(40, 10, 3), false, true, true},
		{"error rate 1% to 1.5% does not", sample(200, 10, 2), sample(200, 10, 3), false, false, true},
		{"error rate up 1 point and doubled", sample(100, 10, 1), sample(100, 10, 2), false, true, true},
		{"error rate up 1 point but not doubled", sample(100, 10, 2), sample(100, 10, 3), false, false, true},
		{"latency and error together", sample(40, 10, 0), sample(40, 30, 4), true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := analysis.Regress(tc.base, tc.bad)
			if r.Enough != tc.enough || r.Latency != tc.latency || r.Error != tc.errs {
				t.Errorf("Regress = enough %v latency %v error %v, want %v %v %v (base %+v, bad %+v)",
					r.Enough, r.Latency, r.Error, tc.enough, tc.latency, tc.errs, r.Baseline, r.Bad)
			}
		})
	}
}

func TestRegress_TypeAndStats(t *testing.T) {
	r := analysis.Regress(sample(40, 10, 0), sample(40, 30, 4))
	if r.Type() != analysis.RegressionLatencyAndError || !r.Regressed() {
		t.Errorf("Type() = %q, want %q", r.Type(), analysis.RegressionLatencyAndError)
	}
	if r.Baseline.N != 40 || r.Bad.P50 != 30 || r.Bad.P95 != 30 || r.Bad.ErrorRate != 0.1 || r.P50Ratio() != 3 || r.NRatio() != 1 {
		t.Errorf("stats = %+v / %+v, ratios p50 %v n %v", r.Baseline, r.Bad, r.P50Ratio(), r.NRatio())
	}
	if got := analysis.Regress(sample(40, 10, 0), sample(40, 10, 0)).Type(); got != "" {
		t.Errorf("Type() without regression = %q, want empty", got)
	}
}

func TestRegress_LatencyShortfallNamesTheMissingThresholdOfEachBranch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		base, bad analysis.VersionSample
		want      string
	}{
		{"every ratio short", withTail(sample(40, 8, 0), 36, 140), withTail(sample(40, 10.4, 0), 36, 68.6),
			"p50 1.30x (+2.40 ms): under 1.5x and under 2x; p95 0.49x (-71.40 ms): under 2x"},
		{"p50 past 1.5x but under its 5 ms floor", sample(30, 5, 0), sample(30, 8.5, 0),
			"p50 1.70x (+3.50 ms): under the 5 ms floor and under 2x; p95 1.70x (+3.50 ms): under 2x"},
		{"p50 past 2x but under both floors", sample(30, 1, 0), sample(30, 2.9, 0),
			"p50 2.90x (+1.90 ms): under the 5 ms floor and under the 2 ms floor; p95 2.90x (+1.90 ms): under the 5 ms floor"},
		{"p50 doubled with 2 ms more but still under 3 ms", sample(30, 0.6, 0), sample(30, 2.7, 0),
			"p50 4.50x (+2.10 ms): under the 5 ms floor and bad p50 under 3 ms; p95 4.50x (+2.10 ms): under the 5 ms floor"},
		{"p95 past 2x but under its 5 ms floor", withTail(sample(40, 1, 0), 36, 2), withTail(sample(40, 1, 0), 36, 6),
			"p50 1.00x (+0.00 ms): under 1.5x and under 2x; p95 3.00x (+4.00 ms): under the 5 ms floor"},
		{"a ratio just under the threshold is not shown rounded up to it", sample(30, 2.2, 0), sample(30, 4.39, 0),
			"p50 1.99x (+2.19 ms): under the 5 ms floor and under 2x; p95 1.99x (+2.19 ms): under 2x"},
		{"a zero baseline has no ratio", sample(30, 0, 0), sample(30, 1.5, 0),
			"p50 from 0 ms (+1.50 ms): under the 5 ms floor and under the 2 ms floor; p95 from 0 ms (+1.50 ms): under the 5 ms floor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := analysis.Regress(tc.base, tc.bad)
			if r.Latency {
				t.Fatalf("the case must not regress (base %+v, bad %+v)", r.Baseline, r.Bad)
			}
			if got := r.LatencyShortfall(); got != tc.want {
				t.Errorf("LatencyShortfall =\n %q, want\n %q", got, tc.want)
			}
		})
	}
}
