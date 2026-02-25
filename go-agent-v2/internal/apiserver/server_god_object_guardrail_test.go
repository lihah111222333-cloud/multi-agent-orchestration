package apiserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	maxServerTopLevelFieldCount = 33
	maxServerMethodCount        = 192
	maxServerGoFileLines        = 500
)

func TestServerTopLevelFieldBudget(t *testing.T) {
	t.Parallel()

	count, err := serverTopLevelFieldCount("server.go")
	if err != nil {
		t.Fatalf("count Server top-level fields: %v", err)
	}
	if count > maxServerTopLevelFieldCount {
		t.Fatalf("Server top-level fields = %d, budget = %d (move state into embedded groups)", count, maxServerTopLevelFieldCount)
	}
}

func TestServerMethodBudget(t *testing.T) {
	t.Parallel()

	count, err := serverMethodCount(".")
	if err != nil {
		t.Fatalf("count Server methods: %v", err)
	}
	if count > maxServerMethodCount {
		t.Fatalf("Server methods = %d, budget = %d (extract domain methods from *Server)", count, maxServerMethodCount)
	}
}

func TestServerGoFileLineBudget(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	lines := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines++
	}
	if lines > maxServerGoFileLines {
		t.Fatalf("server.go non-empty lines = %d, budget = %d (split responsibilities into focused files)", lines, maxServerGoFileLines)
	}
}

func serverTopLevelFieldCount(filePath string) (int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name == nil || ts.Name.Name != "Server" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return 0, nil
			}
			return len(st.Fields.List), nil
		}
	}
	return 0, nil
}

func serverMethodCount(root string) (int, error) {
	fset := token.NewFileSet()
	count := 0

	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return 0, parseErr
		}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
				continue
			}
			if isServerReceiver(fd.Recv.List[0].Type) {
				count++
			}
		}
	}
	return count, nil
}

func isServerReceiver(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "Server"
	case *ast.StarExpr:
		ident, ok := t.X.(*ast.Ident)
		return ok && ident.Name == "Server"
	default:
		return false
	}
}
