package otlpfile_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAAlbanoJunior/rootline/internal/adapter/otlpfile"
	"github.com/MarcosAAlbanoJunior/rootline/internal/trace"
)

func TestReadFile_ReadsSampleFile(t *testing.T) {
	spans, err := otlpfile.ReadFile("testdata/sample.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 4 {
		t.Fatalf("want 4 spans, got %d", len(spans))
	}
	byID := map[string]trace.Span{}
	for _, s := range spans {
		byID[s.SpanID] = s
	}

	srv := byID["b7ad6b7169203331"]
	if srv.TraceID != "0af7651916cd43dd8448eb211c80319c" || srv.ParentSpanID != "" ||
		srv.Name != "GET /customers/{id}/orders" || srv.Kind != 2 ||
		srv.Scope != "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp" ||
		srv.Service != "demo-app" || srv.Version != "abc1234" {
		t.Errorf("server span fields wrong: %+v", srv)
	}
	start := time.Unix(0, 1767225600000000000)
	if !srv.Start.Equal(start) || !srv.End.Equal(start.Add(1500*time.Microsecond)) {
		t.Errorf("server span times wrong: %v .. %v", srv.Start, srv.End)
	}
	if srv.Attrs["http.request.method"] != "GET" {
		t.Errorf("string attr: %v", srv.Attrs["http.request.method"])
	}
	if srv.Attrs["http.response.status_code"] != int64(200) {
		t.Errorf("int attr: %#v", srv.Attrs["http.response.status_code"])
	}
	if srv.Attrs["test.flag"] != true {
		t.Errorf("bool attr: %#v", srv.Attrs["test.flag"])
	}

	q := byID["00f067aa0ba902b7"]
	if q.ParentSpanID != "b7ad6b7169203331" || q.Kind != 3 || q.Scope != "github.com/exaring/otelpgx" {
		t.Errorf("query span fields wrong: %+v", q)
	}
	if q.Attrs["pgx.rows_affected"] != int64(3) || q.Attrs["rootline.db.phase"] != "query" {
		t.Errorf("query attrs wrong: %v", q.Attrs)
	}
	wantStack := []string{
		"example.com/demo-app/internal/orders.(*Repo).ItemsByOrder internal/orders/repository.go:30",
		"example.com/demo-app/internal/orders.(*Service).List internal/orders/service.go:42",
	}
	if got, ok := q.Attrs["rootline.app_stack"].([]string); !ok || !reflect.DeepEqual(got, wantStack) {
		t.Errorf("string list attr: %#v", q.Attrs["rootline.app_stack"])
	}

	noStack := byID["1111111111111111"]
	if _, has := noStack.Attrs["rootline.app_stack"]; has {
		t.Error("span without app_stack must not get one")
	}

	prep := byID["2222222222222222"]
	if prep.Attrs["rootline.db.phase"] != "prepare" || prep.Attrs["pgx.prepare_stmt.name"] != "stmt_1" {
		t.Errorf("prepare span attrs wrong: %v", prep.Attrs)
	}
}

func TestRead_MalformedLineReportsLineNumberAndNoPartialResult(t *testing.T) {
	good := `{"resourceSpans":[]}`
	input := good + "\n" + good + "\n" + `{"resourceSpans": [ not json` + "\n" + good + "\n"
	spans, err := otlpfile.Read(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error must name line 3, got %q", err)
	}
	if spans != nil {
		t.Errorf("no partial result expected, got %d spans", len(spans))
	}
}
