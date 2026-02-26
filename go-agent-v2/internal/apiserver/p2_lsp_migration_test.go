package apiserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var p2BannedLSPServerMethods = map[string]struct{}{
	"lspHover":                 {},
	"lspOpenFile":              {},
	"lspDiagnostics":           {},
	"lspDefinition":            {},
	"lspReferences":            {},
	"lspDocumentSymbol":        {},
	"lspRename":                {},
	"lspCompletion":            {},
	"lspDidChange":             {},
	"lspCodeAction":            {},
	"lspSignatureHelp":         {},
	"lspFormat":                {},
	"lspCallHierarchy":         {},
	"lspTypeHierarchy":         {},
	"lspSemanticTokens":        {},
	"lspFoldingRange":          {},
	"lspWorkspaceSymbol":       {},
	"lspImplementation":        {},
	"lspTypeDefinition":        {},
	"lspTextSearch":            {},
	"lspAstSearch":             {},
	"lspAvailabilitySummary":   {},
	"lspDiagnosticsQueryTyped": {},
}

func TestP2NoAPIServerDynamicLSPHandlers(t *testing.T) {
	t.Helper()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	violations := make([]string, 0)
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

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			if !isServerPointerReceiver(fn.Recv.List[0].Type) {
				continue
			}
			if _, banned := p2BannedLSPServerMethods[fn.Name.Name]; !banned {
				continue
			}
			pos := fset.Position(fn.Pos())
			violations = append(violations, pos.String()+": "+fn.Name.Name)
		}
	}

	if len(violations) > 0 {
		t.Fatalf("apiserver still contains dynamic lsp handlers:\n%s", strings.Join(violations, "\n"))
	}
}

func TestP2LSPDynToolsBindViaLSPTools(t *testing.T) {
	t.Helper()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	violations := make([]string, 0)
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

		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			toolName, ok := lspDynToolKey(assign.Lhs[0])
			if !ok || !strings.HasPrefix(toolName, "lsp_") {
				return true
			}
			if isLSPToolsSelector(assign.Rhs[0]) {
				return true
			}

			pos := fset.Position(assign.Pos())
			violations = append(violations, pos.String()+": "+toolName)
			return true
		})
	}

	if len(violations) > 0 {
		t.Fatalf("lsp dynTools bindings in apiserver must route through s.lspTools:\n%s", strings.Join(violations, "\n"))
	}
}

func TestP2DiagCacheNotAccessedOutsideServerAccessors(t *testing.T) {
	t.Helper()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	violations := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".go" || isTestGoFile(name) || name == "server_state_groups.go" {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse file %s: %v", name, parseErr)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "diagCache" {
				return true
			}
			if !isServerStateOwner(sel.X) {
				return true
			}
			pos := fset.Position(sel.Pos())
			violations = append(violations, pos.String())
			return true
		})
	}

	if len(violations) > 0 {
		t.Fatalf("diagCache direct access must stay inside grouped state internals (server_state_groups.go):\n%s", strings.Join(violations, "\n"))
	}
}

func isServerPointerReceiver(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == "Server"
}

func lspDynToolKey(lhs ast.Expr) (string, bool) {
	idx, ok := lhs.(*ast.IndexExpr)
	if !ok {
		return "", false
	}
	sel, ok := idx.X.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "dynTools" {
		return "", false
	}
	if !isServerStateOwner(sel.X) {
		return "", false
	}
	lit, ok := idx.Index.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	unquoted, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(unquoted), true
}

func isLSPToolsSelector(rhs ast.Expr) bool {
	sel, ok := rhs.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	base, ok := sel.X.(*ast.SelectorExpr)
	if !ok || base.Sel == nil || base.Sel.Name != "lspTools" {
		return false
	}
	return isServerStateOwner(base.X)
}
