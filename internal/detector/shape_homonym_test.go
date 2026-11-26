package detector_test

import (
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
)

func TestTraceShapeCheck_DescribesThePromotedDependencyNotAHomonymOfAnotherKind(t *testing.T) {
	homonym := detector.DependencyShare{
		Dependency: detector.Dependency{Kind: detector.KindSpanName, Name: "postgresql"}, Explained: 0.1,
		Baseline: detector.Share{Presence: 1}, Bad: detector.Share{Presence: 1},
	}
	b := grew(100, homonym)
	b.Rows = append(b.Rows, addedWork(0.5, 1, 1))
	c, ok := detector.PromoteShape(fakeEnv{}, "abc1234", b, nil)
	if !ok {
		t.Fatal("nothing promoted")
	}

	got := detector.TraceShapeCheck(b, &c)

	if want := "promoted added_work on db:postgresql: presence 50% → 100%"; !strings.Contains(got.Reason, want) {
		t.Errorf("reason = %q, want it to contain %q", got.Reason, want)
	}
}
