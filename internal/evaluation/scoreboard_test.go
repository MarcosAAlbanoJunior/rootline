package evaluation_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/evaluation"
)

func sb(c, l, m, s evaluation.Metric) evaluation.Scoreboard {
	return evaluation.Scoreboard{"detectors_only": {Cause: c, Line: l, Commit: m, Strict: s}}
}

var one = evaluation.Metric{Hits: 1, Total: 1}

func TestScoreboard_JSONShapeAndRoundTrip(t *testing.T) {
	in := sb(one, one, one, one)
	var buf bytes.Buffer
	if err := evaluation.WriteScoreboard(&buf, in); err != nil {
		t.Fatal(err)
	}
	var raw map[string]map[string]map[string]int
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("not valid JSON of the documented shape: %v\n%s", err, buf.String())
	}
	if raw["detectors_only"]["cause"]["hits"] != 1 || raw["detectors_only"]["strict"]["total"] != 1 {
		t.Fatalf("unexpected shape: %s", buf.String())
	}
	out, err := evaluation.ReadScoreboard(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip lost data: %+v vs %+v", in, out)
	}
}

func TestRegressions_FlagsDroppedHitsWithTheSameTotal(t *testing.T) {
	zero1 := evaluation.Metric{Hits: 0, Total: 1}
	tests := []struct {
		name      string
		base, cur evaluation.Scoreboard
		want      []string
	}{
		{"nothing dropped", sb(one, one, one, one), sb(one, one, one, one), nil},
		{"hit dropped with same total", sb(one, one, one, one), sb(zero1, one, one, one), []string{"detectors_only.cause"}},
		{"strict dropped", sb(one, one, one, one), sb(one, one, one, zero1), []string{"detectors_only.strict"}},
		{"improvement is fine", sb(zero1, zero1, zero1, zero1), sb(one, one, one, one), nil},
		{"different denominator is not compared", sb(one, one, one, one),
			sb(evaluation.Metric{Hits: 1, Total: 2}, one, one, one), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluation.Regressions(tc.base, tc.cur)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
