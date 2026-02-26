package tooladapter

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoInternalImportsInToolAdapter ensures sdk package remains reusable across CLIs.
func TestNoInternalImportsInToolAdapter(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	fset := token.NewFileSet()
	violations := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse file %s: %v", name, parseErr)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, "\"")
			if strings.Contains(path, "/internal/") {
				violations = append(violations, name+": "+path)
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("tooladapter must not import internal packages: %v", violations)
	}
}
