package codexadapter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestThreadArchiveUtilsExportSurfaceGuardrail(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	utilsPath := filepath.Join(filepath.Dir(thisFile), "thread_archive_utils.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, utilsPath, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse thread_archive_utils.go: %v", err)
	}

	allowedExports := map[string]struct{}{
		"NormalizeThreadArchiveMap": {},
		"SanitizeArchiveName":       {},
		"SanitizeArchiveNameStrict": {},
		"PathWithinRoot":            {},
	}
	foundAllowed := map[string]struct{}{}
	unexpected := make([]string, 0)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil {
			continue
		}
		name := strings.TrimSpace(fn.Name.Name)
		if name == "" || !ast.IsExported(name) {
			continue
		}
		if _, ok := allowedExports[name]; ok {
			foundAllowed[name] = struct{}{}
			continue
		}
		unexpected = append(unexpected, name)
	}

	if len(unexpected) > 0 {
		t.Fatalf("thread_archive_utils export surface regression: unexpected exports: %s", strings.Join(unexpected, ", "))
	}
	for name := range allowedExports {
		if _, ok := foundAllowed[name]; !ok {
			t.Fatalf("thread_archive_utils export surface regression: missing expected export: %s", name)
		}
	}
}
