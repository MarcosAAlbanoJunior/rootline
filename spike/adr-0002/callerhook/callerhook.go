// Package callerhook is the spike version of the Rootline caller hook (ADR-0002).
// It is a pgx.QueryTracer meant to run after otelpgx inside a multitracer: it
// reads the span otelpgx put in the context and attaches the application stack.
package callerhook

import (
	"context"
	"path"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	MaxFrames    = 8
	MaxAttrBytes = 2048
)

// Frame is one application frame, with the file path relative to the module root.
type Frame struct {
	Function string
	File     string
	Line     int
}

type Tracer struct {
	modulePath string // debug.ReadBuildInfo().Main.Path
	mainPkg    string // debug.ReadBuildInfo().Path (import path of package main)
	useCache   bool
	cache      sync.Map // pc -> cachedFrame
}

type cachedFrame struct {
	frame Frame
	isApp bool
}

func New(useCache bool) *Tracer {
	t := &Tracer{useCache: useCache}
	if bi, ok := debug.ReadBuildInfo(); ok {
		t.modulePath = bi.Main.Path
		t.mainPkg = bi.Path
	}
	return t
}

func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() || t.modulePath == "" {
		return ctx
	}
	frames := t.Resolve(t.Capture(3))
	if len(frames) == 0 {
		return ctx
	}
	span.SetAttributes(Attributes(t.modulePath, frames)...)
	return ctx
}

func (t *Tracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Capture returns the raw program counters of the caller's stack.
func (t *Tracer) Capture(skip int) []uintptr {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(skip, pcs)
	return pcs[:n]
}

// Resolve turns program counters into at most MaxFrames application frames,
// innermost first.
func (t *Tracer) Resolve(pcs []uintptr) []Frame {
	if t.useCache {
		return t.resolveCached(pcs)
	}
	out := make([]Frame, 0, MaxFrames)
	frames := runtime.CallersFrames(pcs)
	for {
		f, more := frames.Next()
		if fr, ok := t.appFrame(f.Function, f.File, f.Line); ok {
			out = append(out, fr)
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

// resolveCached caches per PC. Each PC is resolved on its own, which ignores
// inlining expansion; good enough for the spike measurement.
func (t *Tracer) resolveCached(pcs []uintptr) []Frame {
	out := make([]Frame, 0, MaxFrames)
	for _, pc := range pcs {
		if v, ok := t.cache.Load(pc); ok {
			c := v.(cachedFrame)
			if c.isApp {
				out = append(out, c.frame)
			}
		} else {
			f, _ := runtime.CallersFrames([]uintptr{pc}).Next()
			fr, isApp := t.appFrame(f.Function, f.File, f.Line)
			t.cache.Store(pc, cachedFrame{fr, isApp})
			if isApp {
				out = append(out, fr)
			}
		}
		if len(out) == MaxFrames {
			break
		}
	}
	return out
}

// appFrame decides whether a frame belongs to the main module and computes the
// module-relative file path from the function's package path, so it works with
// and without -trimpath.
func (t *Tracer) appFrame(function, file string, line int) (Frame, bool) {
	pkg, ok := t.packagePath(function)
	if !ok {
		return Frame{}, false
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(pkg, t.modulePath), "/")
	return Frame{Function: function, File: path.Join(rel, path.Base(file)), Line: line}, true
}

// packagePath extracts the import path of the function's package and reports
// whether it belongs to the main module.
func (t *Tracer) packagePath(function string) (string, bool) {
	name := function
	if i := strings.IndexByte(name, '['); i >= 0 { // generics: pkg.F[...]
		name = name[:i]
	}
	if strings.HasPrefix(name, "main.") {
		return t.mainPkg, t.mainPkg != "" && inModule(t.mainPkg, t.modulePath)
	}
	slash := strings.LastIndexByte(name, '/')
	dot := strings.IndexByte(name[slash+1:], '.')
	if dot < 0 {
		return "", false
	}
	pkg := name[:slash+1+dot]
	return pkg, inModule(pkg, t.modulePath)
}

func inModule(pkg, module string) bool {
	return pkg == module || strings.HasPrefix(pkg, module+"/")
}

// Attributes builds the span attributes, keeping the stack attribute under MaxAttrBytes.
func Attributes(module string, frames []Frame) []attribute.KeyValue {
	stack := make([]string, 0, len(frames))
	size := 0
	for _, f := range frames {
		s := f.Function + " " + f.File + ":" + strconv.Itoa(f.Line)
		if size+len(s) > MaxAttrBytes {
			break
		}
		size += len(s)
		stack = append(stack, s)
	}
	inner := frames[0]
	return []attribute.KeyValue{
		attribute.String("code.function.name", inner.Function),
		attribute.String("code.file.path", inner.File),
		attribute.Int("code.line.number", inner.Line),
		attribute.String("rootline.module.path", module),
		attribute.StringSlice("rootline.app_stack", stack),
	}
}
