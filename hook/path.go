package callerhook

import (
	"path"
	"strings"
)

// ModuleRelPath derives the module-relative file path of a frame from the function's package
// path, so it works with and without -trimpath. modulePath is the main module path
// (debug.ReadBuildInfo().Main.Path) and mainPkgPath is the import path of package main
// (debug.ReadBuildInfo().Path). It reports ok=false for third-party code, for the standard
// library and for packages whose path only shares a textual prefix with the module path.
func ModuleRelPath(modulePath, mainPkgPath, function, file string) (rel string, ok bool) {
	if modulePath == "" {
		return "", false
	}
	pkg, ok := packagePath(mainPkgPath, function)
	if !ok || !inModule(pkg, modulePath) {
		return "", false
	}
	dir := strings.TrimPrefix(strings.TrimPrefix(pkg, modulePath), "/")
	return path.Join(dir, path.Base(strings.ReplaceAll(file, `\`, "/"))), true
}

// packagePath extracts the import path of the package that declares function.
func packagePath(mainPkgPath, function string) (string, bool) {
	name := function
	if i := strings.IndexByte(name, '['); i >= 0 { // generics: pkg.F[...] or pkg.(*T[...]).M
		name = name[:i]
	}
	if strings.HasPrefix(name, "main.") {
		return mainPkgPath, mainPkgPath != ""
	}
	slash := strings.LastIndexByte(name, '/')
	dot := strings.IndexByte(name[slash+1:], '.')
	if dot < 0 {
		return "", false
	}
	return name[:slash+1+dot], true
}

func inModule(pkg, modulePath string) bool {
	return pkg == modulePath || strings.HasPrefix(pkg, modulePath+"/")
}
