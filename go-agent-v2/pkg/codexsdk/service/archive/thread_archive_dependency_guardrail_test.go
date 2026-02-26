package archive

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServiceArchiveDependencyGuardrail(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	root := filepath.Dir(thisFile)
	files := []string{
		filepath.Join(root, "thread_archive_core.go"),
		filepath.Join(root, "thread_archive_utils.go"),
	}
	for _, p := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(p), err)
		}
		for _, imp := range file.Imports {
			if imp == nil || imp.Path == nil {
				continue
			}
			path := strings.Trim(strings.TrimSpace(imp.Path.Value), "\"")
			if path == "" {
				continue
			}
			if isServiceArchiveBannedImport(path) {
				t.Fatalf("service/archive layering regression: banned import in %s: %s", filepath.Base(p), path)
			}
		}
	}
}

func isServiceArchiveBannedImport(path string) bool {
	if path == "os" || path == "io" || path == "io/fs" {
		return true
	}
	if path == "github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex" {
		return true
	}
	if strings.HasPrefix(path, "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/") {
		return true
	}
	if strings.HasPrefix(path, "github.com/multi-agent/go-agent-v2/internal/") {
		return true
	}
	return false
}
