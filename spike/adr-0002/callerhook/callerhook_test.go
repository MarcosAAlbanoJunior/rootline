package callerhook

import (
	"runtime"
	"testing"
)

func TestPackagePath(t *testing.T) {
	tr := &Tracer{modulePath: "github.com/acme/shop", mainPkg: "github.com/acme/shop/cmd/api"}
	cases := []struct {
		fn, file, wantFile string
		app                bool
	}{
		{"github.com/acme/shop/repo.(*Repo).Items", "/build/x/repo/repo.go", "repo/repo.go", true},
		{"github.com/acme/shop/service.(*Svc).List.func1", "/b/service/svc.go", "service/svc.go", true},
		{"github.com/acme/shop/service.(*Svc).List-range1", "/b/service/svc.go", "service/svc.go", true},
		{"github.com/acme/shop/repo.Map[...]", "github.com/acme/shop/repo/map.go", "repo/map.go", true},
		{"main.handler.func2", "/b/cmd/api/main.go", "cmd/api/main.go", true},
		{"github.com/acme/shop.Root", "/b/root.go", "root.go", true},
		{"github.com/acme/shopping/x.F", "/b/x.go", "", false},
		{"github.com/jackc/pgx/v5.(*Conn).Query", "/m/conn.go", "", false},
		{"runtime.goexit", "/go/src/runtime/asm.s", "", false},
	}
	for _, c := range cases {
		f, ok := tr.appFrame(c.fn, c.file, 10)
		if ok != c.app || (ok && f.File != c.wantFile) {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.fn, f.File, ok, c.wantFile, c.app)
		}
	}
	v2 := &Tracer{modulePath: "github.com/acme/shop/v2"}
	if f, ok := v2.appFrame("github.com/acme/shop/v2/repo.F", "/b/repo/f.go", 1); !ok || f.File != "repo/f.go" {
		t.Errorf("v2 module: got (%q, %v)", f.File, ok)
	}
}

// deep simulates a realistic stack depth (~20 frames) before capturing.
func deep(n int, fn func()) {
	if n == 0 {
		fn()
		return
	}
	deep(n-1, fn)
}

var sink []Frame

func BenchmarkCapture(b *testing.B) {
	tr := New(false)
	deep(20, func() {
		for b.Loop() {
			_ = tr.Capture(3)
		}
	})
}

func BenchmarkResolve(b *testing.B) {
	tr := New(false)
	tr.modulePath = "github.com/MarcosAAlbanoJunior/rootline/spike/adr-0002"
	var pcs []uintptr
	deep(20, func() { pcs = tr.Capture(1) })
	for b.Loop() {
		sink = tr.Resolve(pcs)
	}
}

func BenchmarkResolveCached(b *testing.B) {
	tr := New(true)
	tr.modulePath = "github.com/MarcosAAlbanoJunior/rootline/spike/adr-0002"
	var pcs []uintptr
	deep(20, func() { pcs = tr.Capture(1) })
	for b.Loop() {
		sink = tr.Resolve(pcs)
	}
}

func BenchmarkAttributes(b *testing.B) {
	frames := make([]Frame, MaxFrames)
	for i := range frames {
		frames[i] = Frame{Function: "github.com/acme/shop/service.(*Svc).List", File: "service/svc.go", Line: 40 + i}
	}
	for b.Loop() {
		_ = Attributes("github.com/acme/shop", frames)
	}
}

var _ = runtime.Callers
