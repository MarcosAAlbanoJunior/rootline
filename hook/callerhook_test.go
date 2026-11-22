// Expected API (module github.com/MarcosAAlbanoJunior/rootline/hook, package callerhook):
//
//	func New() *Hook                          // module from debug.ReadBuildInfo().Main.Path
//	func NewWithModule(modulePath string) *Hook // explicit main-module path ("" => hook is a no-op)
//	*Hook implements pgx.QueryTracer and pgx.PrepareTracer.
//
// Test trick: this file is an external test package, whose import path is
// ".../hook_test". Passing exactly that path as the "main module" makes the test functions
// count as application frames while the hook's own frames (".../hook") are not, mimicking an
// application whose module differs from the hook module.
package callerhook_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/multitracer"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	callerhook "github.com/MarcosAAlbanoJunior/rootline/hook"
)

const testModule = "github.com/MarcosAAlbanoJunior/rootline/hook_test"

func newProvider() (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	exp := tracetest.NewInMemoryExporter()
	return sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)), exp
}

func newMulti(tp *sdktrace.TracerProvider, module string) *multitracer.Tracer {
	return multitracer.New(otelpgx.NewTracer(otelpgx.WithTracerProvider(tp)), callerhook.NewWithModule(module))
}

// parentCtx returns a context holding a recording parent span that is never ended: otelpgx only
// creates a query span when the incoming context already carries a recording span, and leaving
// the parent unended keeps it out of the exporter, so the query span is the only exported one.
func parentCtx(tp *sdktrace.TracerProvider) context.Context {
	ctx, _ := tp.Tracer("test").Start(context.Background(), "parent")
	return ctx
}

//go:noinline
func runQuery(ctx context.Context, mt *multitracer.Tracer) {
	ctx = mt.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	mt.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
}

//go:noinline
func recurse(n int, f func()) {
	if n == 0 {
		f()
		return
	}
	recurse(n-1, f)
}

func attrMap(t *testing.T, exp *tracetest.InMemoryExporter) map[attribute.Key]attribute.Value {
	t.Helper()
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected exactly 1 span, got %d", len(spans))
	}
	m := map[attribute.Key]attribute.Value{}
	for _, kv := range spans[0].Attributes {
		m[kv.Key] = kv.Value
	}
	return m
}

func TestAC3_QuerySpanGetsCallerAttributes(t *testing.T) {
	tp, exp := newProvider()
	mt := newMulti(tp, testModule)

	runQuery(parentCtx(tp), mt)

	a := attrMap(t, exp)
	for _, k := range []attribute.Key{"code.function.name", "code.file.path", "code.line.number", "rootline.module.path", "rootline.app_stack", "rootline.db.phase"} {
		if _, ok := a[k]; !ok {
			t.Errorf("missing attribute %s", k)
		}
	}
	if got := a["rootline.db.phase"].AsString(); got != "query" {
		t.Errorf("rootline.db.phase = %q, want query", got)
	}
	if got := a["rootline.module.path"].AsString(); got != testModule {
		t.Errorf("rootline.module.path = %q, want %q", got, testModule)
	}
	if got := a["code.function.name"].AsString(); !strings.Contains(got, "runQuery") {
		t.Errorf("code.function.name = %q, want innermost app frame runQuery", got)
	}
	if got := a["code.line.number"].AsInt64(); got <= 0 {
		t.Errorf("code.line.number = %d, want > 0", got)
	}
	stack := a["rootline.app_stack"].AsStringSlice()
	if len(stack) < 2 || !strings.Contains(stack[0], "runQuery") || !strings.Contains(stack[1], "TestAC3_QuerySpanGetsCallerAttributes") {
		t.Errorf("app_stack must be innermost first (runQuery, then the test): %v", stack)
	}
}

func TestAC4_AppStackFormatAndLimits(t *testing.T) {
	tp, exp := newProvider()
	mt := newMulti(tp, testModule)

	recurse(20, func() { runQuery(parentCtx(tp), mt) })

	stack := attrMap(t, exp)["rootline.app_stack"].AsStringSlice()
	if len(stack) != 8 {
		t.Fatalf("app_stack has %d entries, want exactly 8 (cap) given a 20+ deep stack", len(stack))
	}
	re := regexp.MustCompile(`^\S+ callerhook_test\.go:\d+$`) // "<function> <file relative to module>:<line>"
	total := 0
	for _, e := range stack {
		total += len(e)
		if !re.MatchString(e) {
			t.Errorf("entry %q does not match <function> <module-relative file>:<line>", e)
		}
		if strings.Contains(e, "testing.") || strings.Contains(e, "otelpgx") || strings.Contains(e, "multitracer") {
			t.Errorf("entry %q is not an application frame", e)
		}
	}
	if total > 2048 {
		t.Errorf("app_stack totals %d bytes, want <= 2048", total)
	}
}

func TestAC6_PrepareSpanGetsPhasePrepare(t *testing.T) {
	tp, exp := newProvider()
	mt := newMulti(tp, testModule)

	ctx := mt.TracePrepareStart(parentCtx(tp), nil, pgx.TracePrepareStartData{Name: "stmt1", SQL: "SELECT 1"})
	mt.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})

	a := attrMap(t, exp)
	if got := a["rootline.db.phase"].AsString(); got != "prepare" {
		t.Fatalf("rootline.db.phase = %q, want prepare", got)
	}
}

func TestAC7_EmptyModulePathWritesNothingAndDoesNotPanic(t *testing.T) {
	tp, exp := newProvider()
	h := callerhook.NewWithModule("")

	ctx, span := tp.Tracer("t").Start(context.Background(), "q")
	ctx = h.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	h.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	ctx = h.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{Name: "s", SQL: "SELECT 1"})
	h.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})
	span.End()

	if a := attrMap(t, exp); len(a) != 0 {
		t.Fatalf("expected no attributes with empty Main.Path, got %v", a)
	}
}
