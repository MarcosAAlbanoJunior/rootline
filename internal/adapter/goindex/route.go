package goindex

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// GoFileLister lists the Go source files of a commit. It is optional: only route lookup needs it.
type GoFileLister interface {
	ListGoFiles(sha string) ([]string, error)
}

// FileGrepper is an optional capability of the blob reader: files of a commit that contain a fixed
// string, found with one process. When present, route lookup reads only those files.
type FileGrepper interface {
	GrepGoFiles(sha, needle string) ([]string, error)
}

type routeKey struct{ sha, method, route string }

type routeResult struct {
	function, file string
	found          bool
	err            error
}

// FindRouteHandler implements the level-2 location (ADR-0002): it finds the registration of the
// route (a call such as mux.HandleFunc("GET /customers/{id}/orders", h.list)) and returns the
// declared handler function as a runtime symbol plus its repo-relative file. The result is
// approximate by nature: a function, with no line.
//
// A registration matches when its first argument is the string literal "METHOD route" or just
// route, and the second argument names the handler (an identifier, a selector such as h.list, or
// either wrapped in a conversion like http.HandlerFunc(...)). The handler is looked up among the
// declarations of the same directory. found is false when no registration exists; an error is
// returned when the lookup is ambiguous or a file cannot be read.
//
// Cost: with a FileGrepper reader, one grep process finds the candidate files and only those (plus
// the handler's directory) are read. Without one, every Go file is read. Results are memoized per
// (sha, method, route), so each candidate does not pay again.
func (i *Index) FindRouteHandler(sha, method, route string) (function, file string, found bool, err error) {
	key := routeKey{sha, method, route}
	i.mu.Lock()
	if r, ok := i.routes[key]; ok {
		i.mu.Unlock()
		return r.function, r.file, r.found, r.err
	}
	i.mu.Unlock()
	function, file, found, err = i.findRouteHandler(sha, method, route)
	i.mu.Lock()
	i.routes[key] = routeResult{function, file, found, err}
	i.mu.Unlock()
	return function, file, found, err
}

func (i *Index) findRouteHandler(sha, method, route string) (function, file string, found bool, err error) {
	lister, ok := i.reader.(GoFileLister)
	if !ok {
		return "", "", false, errors.New("goindex: the blob reader cannot list Go files")
	}
	files, err := lister.ListGoFiles(sha)
	if err != nil {
		return "", "", false, err
	}
	candidates := files
	if g, ok := i.reader.(FileGrepper); ok {
		if candidates, err = g.GrepGoFiles(sha, route); err != nil {
			return "", "", false, err
		}
	}
	patterns := map[string]bool{route: true, method + " " + route: true}

	type hit struct{ dir, name string }
	hits := map[hit]bool{}
	for _, f := range candidates {
		data, err := i.reader.ReadBlob(sha, f)
		if err != nil {
			return "", "", false, err
		}
		if !bytes.Contains(data, []byte(route)) {
			continue
		}
		p, err := i.parse(sha, f)
		if err != nil {
			return "", "", false, err
		}
		ast.Inspect(p.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if pat, err := strconv.Unquote(lit.Value); err != nil || !patterns[pat] {
				return true
			}
			if name := handlerName(call.Args[1]); name != "" {
				hits[hit{dir: path.Dir(f), name: name}] = true
			}
			return true
		})
	}
	if len(hits) == 0 {
		return "", "", false, nil
	}

	var resolved []string
	files2 := map[string]string{}
	for h := range hits {
		sym, declFile, err := i.declare(sha, files, h.dir, h.name)
		if err != nil {
			return "", "", false, err
		}
		if sym != "" {
			resolved = append(resolved, sym)
			files2[sym] = declFile
		}
	}
	sort.Strings(resolved)
	resolved = dedupe(resolved)
	switch len(resolved) {
	case 0:
		return "", "", false, nil
	case 1:
		return resolved[0], files2[resolved[0]], true, nil
	}
	return "", "", false, fmt.Errorf("goindex: route %s %s is registered with several handlers: %s", method, route, strings.Join(resolved, ", "))
}

// handlerName extracts the handler's declared name from the registration argument.
func handlerName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.CallExpr: // http.HandlerFunc(h.list)
		if len(x.Args) == 1 {
			return handlerName(x.Args[0])
		}
	case *ast.ParenExpr:
		return handlerName(x.X)
	}
	return ""
}

// declare finds the single FuncDecl called name among the files of dir and returns its runtime
// symbol and file. An unknown name returns "" with no error; two declarations are ambiguous.
func (i *Index) declare(sha string, files []string, dir, name string) (symbol, file string, err error) {
	type decl struct{ sym, file string }
	var found []decl
	for _, f := range files {
		if path.Dir(f) != dir {
			continue
		}
		p, err := i.parse(sha, f)
		if err != nil {
			return "", "", err
		}
		for _, d := range p.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != name {
				continue
			}
			pkgPath, err := i.packagePath(sha, dir)
			if err != nil {
				return "", "", err
			}
			found = append(found, decl{sym: symbolOf(pkgPath, fd), file: f})
		}
	}
	switch len(found) {
	case 0:
		return "", "", nil
	case 1:
		return found[0].sym, found[0].file, nil
	}
	return "", "", fmt.Errorf("goindex: %s is declared %d times in %s", name, len(found), dir)
}

// symbolOf builds the runtime symbol: pkg.Func, pkg.Type.Method or pkg.(*Type).Method.
func symbolOf(pkgPath string, fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return pkgPath + "." + fd.Name.Name
	}
	t := fd.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		return pkgPath + ".(*" + recvName(star.X) + ")." + fd.Name.Name
	}
	return pkgPath + "." + recvName(t) + "." + fd.Name.Name
}

// packagePath returns the import path of the package in dir: the module path of the nearest
// ancestor go.mod plus the directory relative to it.
func (i *Index) packagePath(sha, dir string) (string, error) {
	for d := dir; ; d = path.Dir(d) {
		gomod := "go.mod"
		if d != "." {
			gomod = path.Join(d, "go.mod")
		}
		if data, err := i.reader.ReadBlob(sha, gomod); err == nil {
			mod := moduleDirective(string(data))
			if mod == "" {
				return "", fmt.Errorf("goindex: %s has no module directive", gomod)
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(dir, d), "/")
			if d == "." {
				rel = dir
			}
			if rel == "" || rel == "." {
				return mod, nil
			}
			return mod + "/" + rel, nil
		}
		if d == "." || d == "/" {
			return "", fmt.Errorf("goindex: no go.mod above %s at %s", dir, sha)
		}
	}
}

func moduleDirective(gomod string) string {
	for _, line := range strings.Split(gomod, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
			return strings.Trim(f[1], "\"`")
		}
	}
	return ""
}

func dedupe(sorted []string) []string {
	out := sorted[:0]
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}
