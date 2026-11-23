// The symptom block of scenario.json.
package fixture_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

// xLoad declares the route "/x" used by the symptom-only tests.
const xLoad = `{"requests":1,"rate_per_second":1,"routes":[{"route":"/x","weight":1}]}`

func scenarioJSON(symptom string) string {
	return scenarioJSONWithLoad(`"breaks something"`, symptom, xLoad)
}

// scenarioJSONWithAssumption takes the raw JSON value of "assumption"; empty omits the field.
func scenarioJSONWithAssumption(assumption, symptom string) string {
	return scenarioJSONWithLoad(assumption, symptom, xLoad)
}

// scenarioJSONWithLoad takes the raw JSON values of "assumption" and "load"; empty omits the field.
func scenarioJSONWithLoad(assumption, symptom, load string) string {
	fields := ""
	if assumption != "" {
		fields += `"assumption":` + assumption + `,`
	}
	if load != "" {
		fields += `"load":` + load + `,`
	}
	return `{"name":"n",` + fields + `"commits":[{"label":"referencia"},{"label":"regressao","patch":"r.patch"}],
"answer":{"cause":"n_plus_one","function":"f","file":"a.go","marker":"// m","commit":"regressao"},
"symptom":` + symptom + `}`
}

func loadScenarioJSON(t *testing.T, body string) (fixture.Scenario, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scenario.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture.LoadScenario(dir)
}

func loadScenarioWith(t *testing.T, symptom string) (fixture.Scenario, error) {
	t.Helper()
	return loadScenarioJSON(t, scenarioJSON(symptom))
}

func TestLoadScenario_SymptomBlockIsLoaded(t *testing.T) {
	symptom := `{"service":"demo-app","method":"GET","route":"/customers/{id}/orders","route_source":"span_name","bad":"regressao","baseline":"referencia"}`
	sc, err := loadScenarioJSON(t, scenarioJSONWithLoad(`"breaks something"`, symptom, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := fixture.Symptom{Service: "demo-app", Method: "GET", Route: "/customers/{id}/orders", RouteSource: "span_name", Bad: "regressao", Baseline: "referencia"}
	if sc.Symptom != want {
		t.Fatalf("Symptom = %+v, want %+v", sc.Symptom, want)
	}
}

func TestLoadScenario_EmptyBaselineIsAllowed(t *testing.T) {
	if _, err := loadScenarioWith(t, `{"service":"s","method":"GET","route":"/x","bad":"regressao"}`); err != nil {
		t.Fatal(err)
	}
}

func TestLoadScenario_InvalidSymptomNamesTheField(t *testing.T) {
	cases := []struct{ name, symptom, field string }{
		{"empty service", `{"method":"GET","route":"/x","bad":"regressao"}`, "service"},
		{"empty method", `{"service":"s","route":"/x","bad":"regressao"}`, "method"},
		{"empty route", `{"service":"s","method":"GET","bad":"regressao"}`, "symptom.route"},
		{"blank route", `{"service":"s","method":"GET","route":"  ","bad":"regressao"}`, "symptom.route"},
		{"empty bad", `{"service":"s","method":"GET","route":"/x"}`, "bad"},
		{"bad is not a commit label", `{"service":"s","method":"GET","route":"/x","bad":"nope"}`, "bad"},
		{"baseline is not a commit label", `{"service":"s","method":"GET","route":"/x","bad":"regressao","baseline":"nope"}`, "baseline"},
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

func TestLoadScenario_MissingSymptomBlockIsAnError(t *testing.T) {
	dir := t.TempDir()
	body := `{"name":"n","commits":[{"label":"a"}],"answer":{"cause":"c","function":"f","file":"a.go","marker":"m","commit":"a"}}`
	if err := os.WriteFile(filepath.Join(dir, "scenario.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.LoadScenario(dir); err == nil {
		t.Fatal("a scenario without symptom must be rejected")
	}
}

func TestLoadScenario_N1BasicDeclaresTheSymptom(t *testing.T) {
	sc, err := fixture.LoadScenario(filepath.Join("..", "..", "eval", "fixtures", "scenarios", "n1-basic"))
	if err != nil {
		t.Fatal(err)
	}
	want := fixture.Symptom{Service: "demo-app", Method: "GET", Route: "/customers/{id}/orders", RouteSource: "span_name", Bad: "regressao", Baseline: "referencia"}
	if sc.Symptom != want {
		t.Fatalf("Symptom = %+v, want %+v", sc.Symptom, want)
	}
}

func TestLoadScenario_MissingOrBlankAssumptionIsAnErrorCitingAssumption(t *testing.T) {
	symptom := `{"service":"s","method":"GET","route":"/x","bad":"regressao"}`
	for name, assumption := range map[string]string{"missing": "", "empty": `""`, "blank": `"  \t "`} {
		t.Run(name, func(t *testing.T) {
			_, err := loadScenarioJSON(t, scenarioJSONWithAssumption(assumption, symptom))
			if err == nil || !strings.Contains(err.Error(), "assumption") {
				t.Fatalf("err = %v, want an error citing %q", err, "assumption")
			}
		})
	}
}

func TestLoadScenario_AssumptionIsLoaded(t *testing.T) {
	sc, err := loadScenarioJSON(t, scenarioJSONWithAssumption(`"loop two frames up"`, `{"service":"s","method":"GET","route":"/x","bad":"regressao"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Assumption != "loop two frames up" {
		t.Fatalf("Assumption = %q", sc.Assumption)
	}
}

func TestLoadScenario_N1BasicDeclaresTheAssumption(t *testing.T) {
	sc, err := fixture.LoadScenario(filepath.Join("..", "..", "eval", "fixtures", "scenarios", "n1-basic"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "caso básico: loop no service, query no repository"; sc.Assumption != want {
		t.Fatalf("Assumption = %q, want %q", sc.Assumption, want)
	}
}

// scenarioJSONWithDistractors adds the raw JSON value of "distractors" to a valid scenario.
func scenarioJSONWithDistractors(distractors string) string {
	body := scenarioJSON(`{"service":"s","method":"GET","route":"/x","bad":"regressao"}`)
	return strings.Replace(body, `{"name":"n",`, `{"name":"n","distractors":`+distractors+`,`, 1)
}

func TestLoadScenario_WithoutDistractorsBlockHasNoDistractors(t *testing.T) {
	sc, err := loadScenarioWith(t, `{"service":"s","method":"GET","route":"/x","bad":"regressao"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Distractors) != 0 {
		t.Fatalf("Distractors = %+v, want none", sc.Distractors)
	}
}

func TestLoadScenario_DistractorsAreLoaded(t *testing.T) {
	sc, err := loadScenarioJSON(t, scenarioJSONWithDistractors(`[{"function":"g","file":"b.go","marker":" // rootline:distractor"}]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []fixture.ScenarioDistractor{{Function: "g", File: "b.go", Marker: " // rootline:distractor"}}
	if !slices.Equal(sc.Distractors, want) {
		t.Fatalf("Distractors = %+v, want %+v", sc.Distractors, want)
	}
}

func TestLoadScenario_InvalidDistractorIsAnErrorCitingItsIndex(t *testing.T) {
	ok := `{"function":"g","file":"b.go","marker":"// d"}`
	cases := []struct{ name, second, field string }{
		{"no function", `{"file":"b.go","marker":"// d2"}`, "distractors[1].function"},
		{"no file", `{"function":"g","marker":"// d2"}`, "distractors[1].file"},
		{"no marker", `{"function":"g","file":"b.go"}`, "distractors[1].marker"},
		{"marker of the answer", `{"function":"g","file":"b.go","marker":"// m"}`, "distractors[1].marker"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadScenarioJSON(t, scenarioJSONWithDistractors(`[`+ok+`,`+c.second+`]`))
			if err == nil || !strings.Contains(err.Error(), c.field) {
				t.Fatalf("err = %v, want an error citing %q", err, c.field)
			}
		})
	}
}
