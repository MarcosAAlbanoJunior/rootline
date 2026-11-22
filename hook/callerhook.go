// Package callerhook is the Rootline caller hook (ADR-0002). It is a pgx.QueryTracer and
// pgx.PrepareTracer meant to run after otelpgx inside a multitracer: it reads the span otelpgx
// put in the context and annotates it with the application stack.
package callerhook

import (
	"context"
	"runtime"
	"runtime/debug"
	"strconv"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

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
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return ctx
	}
	frames := h.appFrames(captureStack(3))
	attrs := []attribute.KeyValue{attribute.String("rootline.db.phase", "query")}
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

// TraceQueryEnd implements pgx.QueryTracer.
func (h *Hook) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TracePrepareStart implements pgx.PrepareTracer.
func (h *Hook) TracePrepareStart(ctx context.Context, _ *pgx.Conn, _ pgx.TracePrepareStartData) context.Context {
	if h.modulePath == "" {
		return ctx
	}
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.SetAttributes(attribute.String("rootline.db.phase", "prepare"))
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
