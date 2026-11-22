// Package goindex answers code-structure questions about Go sources read from git blobs: whether
// a line is inside a loop body and which line range a declared function covers (ADR-0007).
package goindex

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"sync"
)

// BlobReader reads the content of path at sha.
type BlobReader interface {
	ReadBlob(sha, path string) ([]byte, error)
}

type parsed struct {
	fset *token.FileSet
	file *ast.File
}

// Index parses blobs on demand and caches the result per (sha, path).
type Index struct {
	reader BlobReader
	mu     sync.Mutex
	cache  map[string]*parsed
	routes map[routeKey]routeResult
}

// New returns an Index over r.
func New(r BlobReader) *Index {
	return &Index{reader: r, cache: map[string]*parsed{}, routes: map[routeKey]routeResult{}}
}

func (i *Index) parse(sha, path string) (*parsed, error) {
	key := sha + "\x00" + path
	i.mu.Lock()
	defer i.mu.Unlock()
	if p, ok := i.cache[key]; ok {
		return p, nil
	}
	data, err := i.reader.ReadBlob(sha, path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, data, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	p := &parsed{fset: fset, file: f}
	i.cache[key] = p
	return p, nil
}

// InLoop reports whether line is strictly inside the body of a ForStmt or RangeStmt: after the
// line of the opening brace and before the line of the closing brace.
func (i *Index) InLoop(sha, file string, line int) (bool, error) {
	p, err := i.parse(sha, file)
	if err != nil {
		return false, err
	}
	in := false
	ast.Inspect(p.file, func(n ast.Node) bool {
		if in {
			return false
		}
		var body *ast.BlockStmt
		switch s := n.(type) {
		case *ast.ForStmt:
			body = s.Body
		case *ast.RangeStmt:
			body = s.Body
		}
		if body != nil {
			open := p.fset.Position(body.Lbrace).Line
			end := p.fset.Position(body.Rbrace).Line
			if line > open && line < end {
				in = true
				return false
			}
		}
		return true
	})
	return in, nil
}

var rangeSuffix = regexp.MustCompile(`-range\d+`)

// FuncRange returns the line range [start,end] of the FuncDecl that declares function, a runtime
// symbol with or without the import path. Closures (.funcN) and range-over-func bodies (-rangeN)
// resolve to the enclosing declared function.
func (i *Index) FuncRange(sha, file, function string) (start, end int, found bool, err error) {
	p, err := i.parse(sha, file)
	if err != nil {
		return 0, 0, false, err
	}
	decls := map[string]*ast.FuncDecl{}
	for _, d := range p.file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			decls[declKey(fd)] = fd
		}
	}
	tokens := symbolTokens(function)
	for k := len(tokens); k >= 1; k-- {
		if fd, ok := decls[strings.Join(tokens[:k], ".")]; ok {
			return p.fset.Position(fd.Pos()).Line, p.fset.Position(fd.End()).Line, true, nil
		}
	}
	return 0, 0, false, nil
}

// declKey is "Name" for functions and "Type.Name" for methods.
func declKey(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	return recvName(fd.Recv.List[0].Type) + "." + fd.Name.Name
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// symbolTokens turns "github.com/x/orders.(*Service).List.func1" into ["Service","List","func1"].
func symbolTokens(function string) []string {
	name := rangeSuffix.ReplaceAllString(function, "")
	if b := strings.IndexByte(name, '['); b >= 0 {
		if e := strings.LastIndexByte(name, ']'); e > b {
			name = name[:b] + name[e+1:]
		}
	}
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		name = name[slash+1:]
	}
	if dot := strings.IndexByte(name, '.'); dot >= 0 { // drop the package name
		name = name[dot+1:]
	}
	name = strings.NewReplacer("(*", "", "(", "", ")", "").Replace(name)
	return strings.Split(name, ".")
}
