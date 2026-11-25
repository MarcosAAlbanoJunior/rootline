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
	"go.opentelemetry.io/otel/trace"

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

func TestHook_QuerySpanGetsCallerAttributes(t *testing.T) {
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
	if len(stack) < 2 || !strings.Contains(stack[0], "runQuery") || !strings.Contains(stack[1], "TestHook_QuerySpanGetsCallerAttributes") {
		t.Errorf("app_stack must be innermost first (runQuery, then the test): %v", stack)
	}
}

func TestHook_AppStackFormatAndLimits(t *testing.T) {
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

func TestHook_PrepareSpanGetsPhasePrepare(t *testing.T) {
	tp, exp := newProvider()
	mt := newMulti(tp, testModule)

	ctx := mt.TracePrepareStart(parentCtx(tp), nil, pgx.TracePrepareStartData{Name: "stmt1", SQL: "SELECT 1"})
	mt.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})

	a := attrMap(t, exp)
	if got := a["rootline.db.phase"].AsString(); got != "prepare" {
		t.Fatalf("rootline.db.phase = %q, want prepare", got)
	}
}

func TestNewWithModule_EmptyModulePathWritesNothingAndDoesNotPanic(t *testing.T) {
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

// handlerCtx returns a context whose active span is a server span, as in an app whose pgx calls
// carry no database instrumentation: the hook sees the HTTP handler's span.
func handlerCtx(tp *sdktrace.TracerProvider) (context.Context, trace.Span) {
	return tp.Tracer("test").Start(context.Background(), "GET /orders", trace.WithSpanKind(trace.SpanKindServer))
}

func TestHook_LeavesANonClientSpanAloneAndCountsIt(t *testing.T) {
	tp, exp := newProvider()
	h := callerhook.NewWithModule(testModule)

	ctx, span := handlerCtx(tp)
	ctx = h.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	h.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	ctx = h.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{Name: "s", SQL: "SELECT 1"})
	h.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})
	span.End()

	if a := attrMap(t, exp); len(a) != 0 {
		t.Errorf("handler span got %v, want no attributes", a)
	}
	if got := h.Unannotated(); got != 2 {
		t.Errorf("Unannotated() = %d, want 2", got)
	}
}

func TestHook_AnnotatesAClientSpanWithItsVersion(t *testing.T) {
	tp, exp := newProvider()
	mt := newMulti(tp, testModule)

	runQuery(parentCtx(tp), mt)

	if got := attrMap(t, exp)["rootline.hook.version"].AsString(); got != callerhook.Version {
		t.Errorf("rootline.hook.version = %q, want %q", got, callerhook.Version)
	}
}

// A GORM plugin span is a client span that gets db.system only when the query ends, after the
// hook ran; it must still be annotated.
func TestHook_AnnotatesAClientSpanThatHasNoDBSystemYet(t *testing.T) {
	tp, exp := newProvider()
	h := callerhook.NewWithModule(testModule)

	ctx, span := tp.Tracer("gorm").Start(parentCtx(tp), "gorm.Query", trace.WithSpanKind(trace.SpanKindClient))
	ctx = h.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{Name: "s", SQL: "SELECT 1"})
	h.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})
	span.End()

	a := attrMap(t, exp)
	if a["rootline.db.phase"].AsString() != "prepare" || a["rootline.hook.version"].AsString() != callerhook.Version {
		t.Errorf("attributes = %v, want the prepare phase and the hook version", a)
	}
	if got := h.Unannotated(); got != 0 {
		t.Errorf("Unannotated() = %d, want 0", got)
	}
}

// Without otelpgx (otelsql, GORM), pgx prepares a statement inside the query, on the same span:
// the span stays a query span with its stack.
func TestHook_APrepareInsideAQueryOnTheSameSpanKeepsTheQueryPhase(t *testing.T) {
	tp, exp := newProvider()
	h := callerhook.NewWithModule(testModule)

	ctx, span := tp.Tracer("otelsql").Start(parentCtx(tp), "sql.conn.query", trace.WithSpanKind(trace.SpanKindClient))
	ctx = h.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	pctx := h.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{Name: "s", SQL: "SELECT 1"})
	h.TracePrepareEnd(pctx, nil, pgx.TracePrepareEndData{})
	h.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	span.End()

	a := attrMap(t, exp)
	if got := a["rootline.db.phase"].AsString(); got != "query" {
		t.Errorf("rootline.db.phase = %q, want query", got)
	}
	if _, ok := a["rootline.app_stack"]; !ok {
		t.Errorf("attributes = %v, want the app stack", a)
	}
}

// otelpgx opens its own prepare span inside the query span: that child is a prepare span.
func TestHook_APrepareSpanInsideAQueryIsStillAPrepare(t *testing.T) {
	tp, exp := newProvider()
	h := callerhook.NewWithModule(testModule)
	tr := tp.Tracer("otelpgx")

	qctx, qspan := tr.Start(parentCtx(tp), "query", trace.WithSpanKind(trace.SpanKindClient))
	qctx = h.TraceQueryStart(qctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	pctx, pspan := tr.Start(qctx, "prepare", trace.WithSpanKind(trace.SpanKindClient))
	h.TracePrepareStart(pctx, nil, pgx.TracePrepareStartData{Name: "s", SQL: "SELECT 1"})
	pspan.End()
	_ = qspan // left unended: only the prepare span is exported

	if got := attrMap(t, exp)["rootline.db.phase"].AsString(); got != "prepare" {
		t.Errorf("rootline.db.phase = %q, want prepare", got)
	}
}

// opaqueSpan hides the SDK span's kind, like a span from another OpenTelemetry implementation.
type opaqueSpan struct{ trace.Span }

func TestHook_AnnotatesARecordingSpanWhoseKindCannotBeRead(t *testing.T) {
	tp, exp := newProvider()
	h := callerhook.NewWithModule(testModule)

	ctx, span := tp.Tracer("vendor").Start(parentCtx(tp), "query", trace.WithSpanKind(trace.SpanKindClient))
	ctx = trace.ContextWithSpan(ctx, opaqueSpan{span})
	h.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	span.End()

	if got := attrMap(t, exp)["rootline.db.phase"].AsString(); got != "query" {
		t.Errorf("rootline.db.phase = %q, want query", got)
	}
	if got := h.Unannotated(); got != 0 {
		t.Errorf("Unannotated() = %d, want 0", got)
	}
}
