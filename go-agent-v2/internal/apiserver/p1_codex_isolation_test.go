package apiserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestP1CodexSymbolsAreIsolated enforces codex boundary isolation without file-name coupling.
func TestP1CodexSymbolsAreIsolated(t *testing.T) {
	t.Helper()

	offenders, err := collectImportOffenders("codexadapter", "github.com/multi-agent/go-agent-v2/internal/apiserver")
	if err != nil {
		t.Fatalf("collect codexadapter imports: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("codexadapter must not import apiserver: %v", offenders)
	}
}

func parseAPIServerNonTestFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	files := make(map[string]*ast.File)
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".go" || isTestGoFile(name) {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse file %s: %v", name, parseErr)
		}
		files[name] = file
	}
	return files
}

func findFuncDecl(files map[string]*ast.File, fnName string) (*ast.FuncDecl, string, bool) {
	for fileName, file := range files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Name.Name != fnName || fd.Body == nil {
				continue
			}
			return fd, fileName, true
		}
	}
	return nil, "", false
}

func funcDeclContainsCodexAdapterCall(fd *ast.FuncDecl) bool {
	if fd == nil || fd.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fd.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		inner, ok := sel.X.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		root, ok := inner.X.(*ast.Ident)
		if !ok {
			return true
		}
		if root.Name == "s" && inner.Sel != nil && inner.Sel.Name == "codexAdapter" {
			found = true
			return false
		}
		return true
	})
	return found
}

func funcDeclContainsPackageCall(fd *ast.FuncDecl, pkgName string) bool {
	if fd == nil || fd.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fd.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		root, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if root.Name == pkgName {
			found = true
			return false
		}
		return true
	})
	return found
}

func funcDeclContainsClientMethodCall(fd *ast.FuncDecl, method string) bool {
	if fd == nil || fd.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fd.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != method {
			return true
		}
		inner, ok := sel.X.(*ast.SelectorExpr)
		if !ok || inner.Sel == nil || inner.Sel.Name != "Client" {
			return true
		}
		found = true
		return false
	})
	return found
}

func collectImportOffenders(rootDir, bannedImport string) ([]string, error) {
	offenders := make([]string, 0)
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			if imp == nil || imp.Path == nil {
				continue
			}
			importPath := strings.Trim(imp.Path.Value, "\"")
			if importPath == bannedImport {
				offenders = append(offenders, filepath.Clean(path))
				break
			}
		}
		return nil
	})
	sort.Strings(offenders)
	return offenders, err
}

func isTestGoFile(name string) bool {
	if len(name) < len("_test.go") {
		return false
	}
	return name[len(name)-len("_test.go"):] == "_test.go"
}
