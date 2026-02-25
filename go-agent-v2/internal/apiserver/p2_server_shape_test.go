package apiserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestP2ServerGoMethodSurface(t *testing.T) {
	t.Helper()

	allowed := map[string]struct{}{
		"ListenAndServe":          {},
		"cleanupRuntimeResources": {},
	}

	seen := make(map[string]struct{}, len(allowed))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "server.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse server.go: %v", err)
	}

	violations := make([]string, 0)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		if !isServerPointerReceiver(fn.Recv.List[0].Type) {
			continue
		}
		name := fn.Name.Name
		if _, ok := allowed[name]; !ok {
			pos := fset.Position(fn.Pos())
			violations = append(violations, pos.String()+": "+name)
			continue
		}
		seen[name] = struct{}{}
	}

	if len(violations) > 0 {
		t.Fatalf("server.go must keep a minimal Server method surface:\n%s", strings.Join(violations, "\n"))
	}

	for name := range allowed {
		if _, ok := seen[name]; ok {
			continue
		}
		t.Fatalf("server.go missing required core method %q", name)
	}
}
