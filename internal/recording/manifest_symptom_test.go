// The manifest carries the symptom block.
package recording_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
	"github.com/MarcosAAlbanoJunior/rootline/internal/recording"
)

func TestManifest_SymptomRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	in := sampleManifest()
	in.Symptom = fixture.Symptom{Service: "svc-x", Method: "GET", RouteSource: "span_name", Bad: "ruim", Baseline: "boa"}
	if err := recording.WriteManifest(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := recording.ReadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip lost the symptom:\n in: %+v\nout: %+v", in, out)
	}
	raw, _ := os.ReadFile(path)
	for _, want := range []string{`"symptom"`, `"service"`, `"route_source"`, `"bad"`, `"baseline"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("manifest.json lacks %s:\n%s", want, raw)
		}
	}
}
