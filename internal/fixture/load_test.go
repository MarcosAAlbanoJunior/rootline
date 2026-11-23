// The load block of scenario.json.
package fixture_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/fixture"
)

const ordersSymptom = `{"service":"s","method":"GET","route":"/customers/{id}/orders","bad":"regressao"}`

func TestLoadScenario_WithoutLoadBlockUsesTheDefaultLoad(t *testing.T) {
	sc, err := loadScenarioJSON(t, scenarioJSONWithLoad(`"a"`, ordersSymptom, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := fixture.Load{Requests: 200, RatePerSecond: 50, Routes: []fixture.LoadRoute{{Route: "/customers/{id}/orders", Weight: 1}}}
	if !reflect.DeepEqual(sc.Load, want) {
		t.Fatalf("Load = %+v, want %+v", sc.Load, want)
	}
	if !reflect.DeepEqual(fixture.DefaultLoad(), want) {
		t.Fatalf("DefaultLoad() = %+v, want %+v", fixture.DefaultLoad(), want)
	}
}

func TestLoadScenario_DeclaredLoadIsLoaded(t *testing.T) {
	load := `{"requests":8,"rate_per_second":4,"routes":[{"route":"/customers/{id}/orders","weight":3},{"route":"/b","weight":1}]}`
	sc, err := loadScenarioJSON(t, scenarioJSONWithLoad(`"a"`, ordersSymptom, load))
	if err != nil {
		t.Fatal(err)
	}
	want := fixture.Load{Requests: 8, RatePerSecond: 4, Routes: []fixture.LoadRoute{{Route: "/customers/{id}/orders", Weight: 3}, {Route: "/b", Weight: 1}}}
	if !reflect.DeepEqual(sc.Load, want) {
		t.Fatalf("Load = %+v, want %+v", sc.Load, want)
	}
}

func TestLoadScenario_InvalidLoadNamesTheField(t *testing.T) {
	route := func(r string, w int) string {
		return `{"route":"` + r + `","weight":` + strconv.Itoa(w) + `}`
	}
	ok := route("/customers/{id}/orders", 1)
	load := func(requests, rate string, routes ...string) string {
		return `{"requests":` + requests + `,"rate_per_second":` + rate + `,"routes":[` + strings.Join(routes, ",") + `]}`
	}
	cases := []struct{ name, load, field string }{
		{"zero requests", load("0", "1", ok), "load.requests"},
		{"negative requests", load("-3", "1", ok), "load.requests"},
		{"zero rate", load("1", "0", ok), "load.rate_per_second"},
		{"negative rate", load("1", "-1", ok), "load.rate_per_second"},
		{"no routes", load("1", "1"), "load.routes"},
		{"routes missing", `{"requests":1,"rate_per_second":1}`, "load.routes"},
		{"zero weight", load("1", "1", ok, route("/b", 0)), "load.routes[1].weight"},
		{"negative weight", load("1", "1", route("/customers/{id}/orders", -2)), "load.routes[0].weight"},
		{"route without leading slash", load("1", "1", ok, route("b/{id}", 1)), "load.routes[1].route"},
		{"empty route", load("1", "1", ok, route("", 1)), "load.routes[1].route"},
		{"repeated route", load("1", "1", ok, route("/b", 1), ok), "load.routes[2].route"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadScenarioJSON(t, scenarioJSONWithLoad(`"a"`, ordersSymptom, c.load))
			if err == nil || !strings.Contains(err.Error(), c.field) {
				t.Fatalf("err = %v, want an error citing %q", err, c.field)
			}
		})
	}
}

func TestLoadScenario_SymptomRouteOutsideTheLoadIsAnErrorCitingSymptomRoute(t *testing.T) {
	cases := map[string]string{
		"declared load": `{"requests":1,"rate_per_second":1,"routes":[{"route":"/a/{id}","weight":1}]}`,
		"default load":  "",
	}
	for name, load := range cases {
		t.Run(name, func(t *testing.T) {
			symptom := `{"service":"s","method":"GET","route":"/customers/{id}/receipts","bad":"regressao"}`
			_, err := loadScenarioJSON(t, scenarioJSONWithLoad(`"a"`, symptom, load))
			if err == nil || !strings.Contains(err.Error(), "symptom.route") {
				t.Fatalf("err = %v, want an error citing symptom.route", err)
			}
		})
	}
}
