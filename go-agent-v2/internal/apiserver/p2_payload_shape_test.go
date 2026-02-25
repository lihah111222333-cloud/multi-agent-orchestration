package apiserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestP2ServerPayloadSplitGuard(t *testing.T) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "server_payload.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse server_payload.go: %v", err)
	}

	bannedFuncs := map[string]struct{}{
		"AgentEventHandler":            {},
		"handleHTTPRPC":                {},
		"writeJSONRPCError":            {},
		"recoveryMiddleware":           {},
		"corsMiddleware":               {},
		"handleSSE":                    {},
		"toolResultSuccess":            {},
		"rememberFileChanges":          {},
		"consumeRememberedFileChanges": {},
		"enrichFileChangePayload":      {},
	}
	bannedVars := map[string]struct{}{}

	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			if _, disallow := bannedFuncs[typed.Name.Name]; !disallow {
				continue
			}
			pos := fset.Position(typed.Pos())
			t.Fatalf("server_payload.go should stay payload-focused; moved function found: %s at %s", typed.Name.Name, pos.String())
		case *ast.GenDecl:
			if typed.Tok != token.VAR {
				continue
			}
			for _, spec := range typed.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if _, disallow := bannedVars[name.Name]; !disallow {
						continue
					}
					pos := fset.Position(name.Pos())
					t.Fatalf("server_payload.go should stay payload-focused; moved var found: %s at %s", name.Name, pos.String())
				}
			}
		}
	}
}
