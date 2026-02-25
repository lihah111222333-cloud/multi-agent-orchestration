package apiserver

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestP2ServerGoMethodSurface(t *testing.T) {
	t.Helper()

	coreMethods, err := collectServerPointerMethods("server.go")
	if err != nil {
		t.Fatalf("collect server.go methods: %v", err)
	}
	if len(coreMethods) > 0 {
		t.Fatalf("server.go should not define Server receiver methods after lifecycle split:\n%s", strings.Join(sortedKeys(coreMethods), "\n"))
	}

	allowedLifecycle := map[string]struct{}{
		"ListenAndServe":          {},
		"cleanupRuntimeResources": {},
	}
	lifecycleMethods, err := collectServerPointerMethods("server_lifecycle.go")
	if err != nil {
		t.Fatalf("collect server_lifecycle.go methods: %v", err)
	}

	violations := make([]string, 0)
	for name := range lifecycleMethods {
		if _, ok := allowedLifecycle[name]; ok {
			continue
		}
		violations = append(violations, name)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("server_lifecycle.go must keep a minimal Server method surface:\n%s", strings.Join(violations, "\n"))
	}

	for name := range allowedLifecycle {
		if _, ok := lifecycleMethods[name]; ok {
			continue
		}
		t.Fatalf("server_lifecycle.go missing required core method %q", name)
	}
}

func TestP2BootstrapFunctionBoundaries(t *testing.T) {
	t.Helper()

	requiredByFile := map[string]map[string]struct{}{
		"server_bootstrap.go": {
			"initRuntimeWiring":     {},
			"applyStallConfig":      {},
			"initCodeRunner":        {},
			"initStores":            {},
			"ensureSkillsCacheDir":  {},
			"defaultSkillsCacheDir": {},
			"initSkills":            {},
		},
	}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	funcToFiles := map[string]map[string]struct{}{}
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
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			set := funcToFiles[fn.Name.Name]
			if set == nil {
				set = map[string]struct{}{}
				funcToFiles[fn.Name.Name] = set
			}
			set[name] = struct{}{}
		}
	}

	violations := make([]string, 0)
	for expectedFile, requiredFuncs := range requiredByFile {
		for fnName := range requiredFuncs {
			files, ok := funcToFiles[fnName]
			if !ok {
				violations = append(violations, fmt.Sprintf("%s: missing required bootstrap function %s", expectedFile, fnName))
				continue
			}
			if _, ok := files[expectedFile]; !ok {
				violations = append(violations, fmt.Sprintf("%s: %s defined in %s", expectedFile, fnName, strings.Join(sortedKeys(files), ",")))
				continue
			}
			if len(files) > 1 {
				violations = append(violations, fmt.Sprintf("%s: %s duplicated across %s", expectedFile, fnName, strings.Join(sortedKeys(files), ",")))
			}
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("bootstrap function boundaries violated:\n%s", strings.Join(violations, "\n"))
	}
}

func TestP2ServerNewInitOrder(t *testing.T) {
	t.Helper()

	callOrder, err := collectTopLevelCallOrder("server.go", "New")
	if err != nil {
		t.Fatalf("collect New call order: %v", err)
	}

	storesIdx := indexOf(callOrder, "initStores")
	runtimeIdx := indexOf(callOrder, "initRuntimeWiring")
	if storesIdx < 0 || runtimeIdx < 0 {
		t.Fatalf("New must call initStores/initRuntimeWiring, got order: %v", callOrder)
	}
	if storesIdx > runtimeIdx {
		t.Fatalf("New must initialize stores before runtime wiring (initStores idx=%d, initRuntimeWiring idx=%d, order=%v)", storesIdx, runtimeIdx, callOrder)
	}
}

func collectServerPointerMethods(path string) (map[string]struct{}, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := map[string]struct{}{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		if !isServerPointerReceiver(fn.Recv.List[0].Type) {
			continue
		}
		out[fn.Name.Name] = struct{}{}
	}
	return out, nil
}

func collectTopLevelCallOrder(path, funcName string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != funcName || fn.Body == nil {
			continue
		}
		order := make([]string, 0, len(fn.Body.List))
		for _, stmt := range fn.Body.List {
			for _, call := range topLevelStmtCalls(stmt) {
				order = append(order, call)
			}
		}
		return order, nil
	}
	return nil, fmt.Errorf("function %s not found in %s", funcName, path)
}

func topLevelStmtCalls(stmt ast.Stmt) []string {
	switch st := stmt.(type) {
	case *ast.ExprStmt:
		if call, ok := st.X.(*ast.CallExpr); ok {
			if name := callExprName(call); name != "" {
				return []string{name}
			}
		}
	case *ast.AssignStmt:
		out := make([]string, 0, len(st.Rhs))
		for _, rhs := range st.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}
			if name := callExprName(call); name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

func callExprName(call *ast.CallExpr) string {
	if call == nil {
		return ""
	}
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if fn.Sel != nil {
			return fn.Sel.Name
		}
	}
	return ""
}

func indexOf(items []string, target string) int {
	for i, item := range items {
		if item == target {
			return i
		}
	}
	return -1
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
