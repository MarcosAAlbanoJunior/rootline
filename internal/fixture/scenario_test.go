// Slice 2, AC16 and AC17: the symptom block of scenario.json.
//
// Expected API (package fixture):
//
//	type Symptom struct { Service, Method, RouteSource, Bad, Baseline string } // json: service, method, route_source, bad, baseline
//	Scenario gains: Symptom Symptom `json:"symptom"`
package fixture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

func scenarioJSON(symptom string) string {
	return `{"name":"n","commits":[{"label":"referencia"},{"label":"regressao","patch":"r.patch"}],
"answer":{"cause":"n_plus_one","function":"f","file":"a.go","marker":"// m","commit":"regressao"},
"symptom":` + symptom + `}`
}

func loadScenarioWith(t *testing.T, symptom string) (fixture.Scenario, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scenario.json"), []byte(scenarioJSON(symptom)), 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture.LoadScenario(dir)
}

func TestAC16_SymptomBlockIsLoaded(t *testing.T) {
	sc, err := loadScenarioWith(t, `{"service":"demo-app","method":"GET","route_source":"span_name","bad":"regressao","baseline":"referencia"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := fixture.Symptom{Service: "demo-app", Method: "GET", RouteSource: "span_name", Bad: "regressao", Baseline: "referencia"}
	if sc.Symptom != want {
		t.Fatalf("Symptom = %+v, want %+v", sc.Symptom, want)
	}
}

func TestAC16_EmptyBaselineIsAllowed(t *testing.T) {
	if _, err := loadScenarioWith(t, `{"service":"s","method":"GET","bad":"regressao"}`); err != nil {
		t.Fatal(err)
	}
}

func TestAC16_InvalidSymptomNamesTheField(t *testing.T) {
	cases := []struct{ name, symptom, field string }{
		{"empty service", `{"method":"GET","bad":"regressao"}`, "service"},
		{"empty method", `{"service":"s","bad":"regressao"}`, "method"},
		{"empty bad", `{"service":"s","method":"GET"}`, "bad"},
		{"bad is not a commit label", `{"service":"s","method":"GET","bad":"nope"}`, "bad"},
		{"baseline is not a commit label", `{"service":"s","method":"GET","bad":"regressao","baseline":"nope"}`, "baseline"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadScenarioWith(t, c.symptom)
			if err == nil || !strings.Contains(err.Error(), c.field) {
				t.Fatalf("err = %v, want an error citing %q", err, c.field)
			}
		})
	}
}

func TestAC16_MissingSymptomBlockIsAnError(t *testing.T) {
	dir := t.TempDir()
	body := `{"name":"n","commits":[{"label":"a"}],"answer":{"cause":"c","function":"f","file":"a.go","marker":"m","commit":"a"}}`
	if err := os.WriteFile(filepath.Join(dir, "scenario.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.LoadScenario(dir); err == nil {
		t.Fatal("a scenario without symptom must be rejected")
	}
}

func TestAC17_N1BasicDeclaresTheSymptom(t *testing.T) {
	sc, err := fixture.LoadScenario(filepath.Join("..", "..", "eval", "fixtures", "scenarios", "n1-basic"))
	if err != nil {
		t.Fatal(err)
	}
	want := fixture.Symptom{Service: "demo-app", Method: "GET", RouteSource: "span_name", Bad: "regressao", Baseline: "referencia"}
	if sc.Symptom != want {
		t.Fatalf("Symptom = %+v, want %+v", sc.Symptom, want)
	}
}
