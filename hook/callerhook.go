// Package callerhook is the Rootline caller hook (ADR-0002). It is a pgx.QueryTracer and
// pgx.PrepareTracer meant to run after a database instrumentation (otelpgx, otelsql, the GORM
// plugin) inside a multitracer: it reads the span that instrumentation put in the context and
// annotates it with the application stack.
//
// It only annotates a client span (ADR-0029). Without a database instrumentation the active span
// is the HTTP handler's or an internal one; the hook then writes nothing and counts the call in
// Unannotated. A span whose kind cannot be read (an OpenTelemetry implementation other than the
// SDK) is annotated, as before the guard.
//
// The attributes below are the contract with rootline analyze. Before 1.0, an incompatible change
// to them bumps the minor of Version.
//
//   - rootline.db.phase: "query" or "prepare".
//   - rootline.hook.version: Version.
//   - rootline.app_stack: the application frames, innermost first, each
//     "<function> <module-relative file>:<line>", at most MaxFrames entries and MaxStackBytes.
//   - rootline.module.path: the main module path the files are relative to.
//   - code.function.name, code.file.path, code.line.number: the innermost application frame.
//
// The stack attributes are only written on query spans that have at least one application frame.
package callerhook

import (
	"context"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Version is the hook version written in rootline.hook.version. It matches the hook/vX.Y.Z tag
// the module is published under (ADR-0029).
const Version = "0.1.0"

const (
	// MaxFrames is the maximum number of entries in rootline.app_stack.
	MaxFrames = 8
	// MaxStackBytes is the maximum summed size of the rootline.app_stack entries.
	MaxStackBytes = 2048

	maxPCs = 64
)

// Hook annotates database spans with caller information.
type Hook struct {
	modulePath string // debug.ReadBuildInfo().Main.Path
	mainPkg    string // debug.ReadBuildInfo().Path (import path of package main)

	unannotated atomic.Uint64
}

var (
	_ pgx.QueryTracer   = (*Hook)(nil)
	_ pgx.PrepareTracer = (*Hook)(nil)
)

// New returns a Hook for the running binary's main module.
func New() *Hook {
	h := &Hook{}
	if bi, ok := debug.ReadBuildInfo(); ok {
		h.modulePath = bi.Main.Path
		h.mainPkg = bi.Path
	}
	return h
}

// NewWithModule returns a Hook for an explicit main-module path. An empty path makes the hook a
// no-op.
func NewWithModule(modulePath string) *Hook {
	return &Hook{modulePath: modulePath, mainPkg: modulePath}
}

// TraceQueryStart implements pgx.QueryTracer.
func (h *Hook) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if h.modulePath == "" {
		return ctx
	}
	span, ok := h.clientSpan(ctx)
	if !ok {
		return ctx
	}
	ctx = context.WithValue(ctx, queryKey{}, span)
	frames := h.appFrames(captureStack(3))
	attrs := []attribute.KeyValue{attribute.String("rootline.db.phase", "query"), versionAttr}
	if len(frames) > 0 {
		inner := frames[0]
		attrs = append(attrs,
			attribute.String("code.function.name", inner.function),
			attribute.String("code.file.path", inner.file),
			attribute.Int("code.line.number", inner.line),
			attribute.String("rootline.module.path", h.modulePath),
			attribute.StringSlice("rootline.app_stack", stackEntries(frames)),
		)
	}
	span.SetAttributes(attrs...)
	return ctx
}

var versionAttr = attribute.String("rootline.hook.version", Version)

// Unannotated returns how many pgx calls found a recording span that is not a client span, and so
// were left unannotated.

// queryKey marks the context of a query the hook annotated. pgx prepares the statement inside the
// query with that context; without otelpgx (otelsql, GORM) no prepare span is opened, and the
// prepare must not relabel the query span.
type queryKey struct{}

func isQuerySpan(ctx context.Context, span trace.Span) bool {
	q, ok := ctx.Value(queryKey{}).(trace.Span)
	// Compared by span id: == on two spans of a non-comparable type would panic.
	return ok && q.SpanContext().SpanID() == span.SpanContext().SpanID()
}
func (h *Hook) Unannotated() uint64 { return h.unannotated.Load() }

// kinded is implemented by the OpenTelemetry SDK spans; trace.Span does not expose the kind.
type kinded interface{ SpanKind() trace.SpanKind }

// clientSpan returns the recording span in ctx when it is a client span. The guard is the kind
// alone, not db.system: the GORM plugin sets db.system only after the query ran (ADR-0029). A span
// whose kind cannot be read is accepted: the guard only rules out spans known not to be clients.
func (h *Hook) clientSpan(ctx context.Context) (trace.Span, bool) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return nil, false
	}
	if k, ok := span.(kinded); ok && k.SpanKind() != trace.SpanKindClient {
		h.unannotated.Add(1)
		return nil, false
	}
	return span, true
}

// TraceQueryEnd implements pgx.QueryTracer.
func (h *Hook) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TracePrepareStart implements pgx.PrepareTracer.
func (h *Hook) TracePrepareStart(ctx context.Context, _ *pgx.Conn, _ pgx.TracePrepareStartData) context.Context {
	if h.modulePath == "" {
		return ctx
	}
	if span, ok := h.clientSpan(ctx); ok && !isQuerySpan(ctx, span) {
		span.SetAttributes(attribute.String("rootline.db.phase", "prepare"), versionAttr)
	}
	return ctx
}

// TracePrepareEnd implements pgx.PrepareTracer.
func (h *Hook) TracePrepareEnd(context.Context, *pgx.Conn, pgx.TracePrepareEndData) {}

type frame struct {
	function string
	file     string
	line     int
}

func captureStack(skip int) []uintptr {
	pcs := make([]uintptr, maxPCs)
	n := runtime.Callers(skip, pcs)
	return pcs[:n]
}

// appFrames keeps the application frames, innermost first, up to MaxFrames.
func (h *Hook) appFrames(pcs []uintptr) []frame {
	out := make([]frame, 0, MaxFrames)
	frames := runtime.CallersFrames(pcs)
	for {
		f, more := frames.Next()
		if rel, ok := ModuleRelPath(h.modulePath, h.mainPkg, f.Function, f.File); ok {
			out = append(out, frame{function: f.Function, file: rel, line: f.Line})
			if len(out) == MaxFrames {
				break
			}
		}
		if !more {
			break
		}
	}
	return out
}

// stackEntries formats frames as "<function> <file>:<line>", stopping before the summed size
// would pass MaxStackBytes.
func stackEntries(frames []frame) []string {
	out := make([]string, 0, len(frames))
	total := 0
	for _, f := range frames {
		e := f.function + " " + f.file + ":" + strconv.Itoa(f.line)
		if total+len(e) > MaxStackBytes {
			break
		}
		total += len(e)
		out = append(out, e)
	}
	return out
}
