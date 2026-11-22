// Expected API (module github.com/MarcosAAlbanoJunior/rootline/hook, package callerhook):
//
//	func ModuleRelPath(modulePath, mainPkgPath, function, file string) (rel string, ok bool)
//
// modulePath is debug.ReadBuildInfo().Main.Path; mainPkgPath is debug.ReadBuildInfo().Path
// (import path of the package main). function is the runtime symbol (runtime.Frame.Function),
// file is the absolute file of the frame. rel is "<package dir relative to module>/<file base>"
// (or just "<file base>" for the module root package). ok=false for third-party code and for
// packages whose path merely shares a textual prefix with the module path. AC5.
package callerhook_test

import (
	"testing"

	callerhook "github.com/MarcosAAlbanoJunior/rootline/hook"
)

func TestAC5_ModuleRelPath(t *testing.T) {
	const mod = "github.com/x/shop"
	tests := []struct {
		name     string
		module   string
		mainPkg  string
		function string
		file     string
		want     string
		wantOK   bool
	}{
		{"plain function", mod, mod, mod + "/internal/orders.List", "/src/shop/internal/orders/service.go", "internal/orders/service.go", true},
		{"method with pointer receiver", mod, mod, mod + "/internal/orders.(*Service).List", "/src/shop/internal/orders/service.go", "internal/orders/service.go", true},
		{"closure func1", mod, mod, mod + "/internal/orders.(*Service).List.func1", "/src/shop/internal/orders/service.go", "internal/orders/service.go", true},
		{"nested closure", mod, mod, mod + "/internal/orders.(*Service).List.func1.2", "/src/shop/internal/orders/service.go", "internal/orders/service.go", true},
		{"range over func body", mod, mod, mod + "/internal/orders.List-range1", "/src/shop/internal/orders/service.go", "internal/orders/service.go", true},
		{"generic function", mod, mod, mod + "/internal/store.Get[...]", "/src/shop/internal/store/store.go", "internal/store/store.go", true},
		{"generic method", mod, mod, mod + "/internal/store.(*Repo[...]).Find", "/src/shop/internal/store/repo.go", "internal/store/repo.go", true},
		{"package main at cmd/api", mod, mod + "/cmd/api", "main.run", "/src/shop/cmd/api/main.go", "cmd/api/main.go", true},
		{"package main at module root", mod, mod, "main.run", "/src/shop/main.go", "main.go", true},
		{"package at module root", mod, mod, mod + ".Handler", "/src/shop/handler.go", "handler.go", true},
		{"module with /v2 suffix", mod + "/v2", mod + "/v2", mod + "/v2/internal/a.F", "/src/shop/internal/a/f.go", "internal/a/f.go", true},
		{"module with /v2 suffix, root package", mod + "/v2", mod + "/v2", mod + "/v2.F", "/src/shop/f.go", "f.go", true},
		{"lookalike prefix is refused", mod, mod, mod + "ping/internal/x.F", "/src/shopping/internal/x/x.go", "", false},
		{"lookalike prefix, root package", mod, mod, mod + "ping.F", "/src/shopping/x.go", "", false},
		{"third party code is refused", mod, mod, "github.com/jackc/pgx/v5.(*Conn).Query", "/go/pkg/mod/github.com/jackc/pgx/v5/conn.go", "", false},
		{"stdlib is refused", mod, mod, "net/http.HandlerFunc.ServeHTTP", "/usr/lib/go/src/net/http/server.go", "", false},
		{"main of another binary is refused", mod, mod + "/cmd/api", "runtime.main", "/usr/lib/go/src/runtime/proc.go", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := callerhook.ModuleRelPath(tc.module, tc.mainPkg, tc.function, tc.file)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("ModuleRelPath(%q,%q,%q,%q) = (%q,%v), want (%q,%v)",
					tc.module, tc.mainPkg, tc.function, tc.file, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
