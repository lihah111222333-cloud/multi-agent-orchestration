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

func TestP3ProcClientUsageIsIsolated(t *testing.T) {
	t.Helper()

	root := "."
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel := filepath.Clean(path)
		if strings.HasPrefix(rel, "codexadapter"+string(filepath.Separator)) {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(data)
		clientPrefix := "Client"
		if strings.Contains(content, clientPrefix+".Submit") ||
			strings.Contains(content, clientPrefix+".SendCommand") ||
			strings.Contains(content, clientPrefix+".GetThreadID") ||
			strings.Contains(content, clientPrefix+".ResumeThread") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/apiserver: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("unexpected proc.Client usage outside codexadapter: %v", offenders)
	}
}

func TestP3CodexEntryMethodsDelegateToCodexAdapter(t *testing.T) {
	t.Helper()

	checks := []struct {
		file string
		fn   string
	}{
		{file: "methods_turn_codex.go", fn: "turnStartTyped"},
		{file: "methods_turn_codex.go", fn: "turnSteerTyped"},
		{file: "methods_turn_codex.go", fn: "turnInterrupt"},
		{file: "methods_turn_codex.go", fn: "turnForceComplete"},
		{file: "methods_turn_codex.go", fn: "reviewStartTyped"},
		{file: "methods_thread_codex.go", fn: "threadResumeTyped"},
		{file: "methods_thread_codex.go", fn: "threadNameSetTyped"},
		{file: "methods_thread_codex.go", fn: "threadRollbackTyped"},
		{file: "methods_thread_codex.go", fn: "threadMessagesTyped"},
		{file: "methods_thread_codex.go", fn: "archiveThreadArtifacts"},
		{file: "methods_helpers_codex.go", fn: "ensureThreadReadyForTurn"},
		{file: "turn_tracker_codex.go", fn: "beginTrackedTurn"},
		{file: "turn_tracker_codex.go", fn: "hasActiveTrackedTurn"},
		{file: "turn_tracker_codex.go", fn: "markTrackedTurnInterruptRequested"},
		{file: "turn_tracker_codex.go", fn: "waitTrackedTurnTerminal"},
		{file: "turn_tracker_codex.go", fn: "completeTrackedTurnByID"},
		{file: "turn_tracker_codex.go", fn: "maybeFinalizeTrackedTurn"},
		{file: "turn_tracker_codex.go", fn: "checkTurnStall"},
		{file: "turn_tracker_codex.go", fn: "executeStallAutoInterrupt"},
	}

	fset := token.NewFileSet()
	for _, item := range checks {
		file, err := parser.ParseFile(fset, item.file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", item.file, err)
		}
		if !funcContainsCodexAdapterCall(file, item.fn) {
			t.Fatalf("%s in %s must delegate via s.codexAdapter", item.fn, item.file)
		}
	}
}

func TestP3ThreadArchiveHelpersMovedToCodexAdapter(t *testing.T) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "methods_thread_codex.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse methods_thread_codex.go: %v", err)
	}

	checks := []string{
		"collectThreadArtifactCandidates",
		"pruneArchivedCodexSourceFiles",
		"restoreThreadArchiveSources",
		"inspectThreadArchiveForRestore",
		"findLatestThreadArchiveManifestPath",
		"readThreadArchiveManifest",
		"writeThreadArchiveManifest",
	}
	for _, fn := range checks {
		if !funcContainsPackageCall(file, fn, "codexadapter") {
			t.Fatalf("%s in methods_thread_codex.go must delegate to codexadapter package", fn)
		}
	}
}

func TestP3HistoryResolutionHelpersDelegateToCodexAdapter(t *testing.T) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "methods_helpers_codex.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse methods_helpers_codex.go: %v", err)
	}

	checks := []string{
		"threadExistsInHistory",
		"resolveCodexThreadCandidates",
	}
	for _, fn := range checks {
		if !funcContainsCodexAdapterCall(file, fn) {
			t.Fatalf("%s in methods_helpers_codex.go must delegate via s.codexAdapter", fn)
		}
	}
}

func TestP3PayloadAndApprovalRemainThinBoundary(t *testing.T) {
	t.Helper()

	type fileCheck struct {
		file      string
		forbidden []string
	}

	checks := []fileCheck{
		{
			file: "server_payload.go",
			forbidden: []string{
				"s.captureAndInjectTurnSummary(",
				"s.touchTrackedTurnLastEvent(",
				"s.hasActiveTrackedTurn(",
				"s.maybeFinalizeTrackedTurn(",
			},
		},
		{
			file: "server_approval.go",
			forbidden: []string{
				"s.touchTrackedTurnLastEvent(",
				"defaultStallThreshold / 6",
				"s.stallThreshold / 6",
			},
		},
	}

	for _, check := range checks {
		data, err := os.ReadFile(check.file)
		if err != nil {
			t.Fatalf("read %s: %v", check.file, err)
		}
		content := string(data)
		for _, needle := range check.forbidden {
			if strings.Contains(content, needle) {
				t.Fatalf("%s must not contain %q (strict thin boundary)", check.file, needle)
			}
		}
	}
}

func TestP3ResidualMethodsMustDelegateViaCodexAdapter(t *testing.T) {
	t.Helper()

	checks := []struct {
		file string
		fn   string
	}{
		{file: "methods_thread_codex.go", fn: "threadArchiveTyped"},
		{file: "methods_thread_codex.go", fn: "threadExistsForArchive"},
		{file: "methods_thread_codex.go", fn: "persistThreadArchivedState"},
		{file: "methods_thread_codex.go", fn: "removeThreadArchivedState"},
		{file: "methods_thread_codex.go", fn: "resolveRolloutHistorySource"},
		{file: "methods_helpers_codex.go", fn: "threadExistsInHistory"},
		{file: "methods_helpers_codex.go", fn: "resolveCodexThreadCandidates"},
		{file: "turn_tracker_codex.go", fn: "finalizeTrackedTurnEvent"},
	}

	fset := token.NewFileSet()
	for _, item := range checks {
		file, err := parser.ParseFile(fset, item.file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", item.file, err)
		}
		if !funcContainsCodexAdapterCall(file, item.fn) {
			t.Fatalf("%s in %s must delegate via s.codexAdapter", item.fn, item.file)
		}
	}
}

func TestP4BoundaryMethodsAvoidDirectClientCalls(t *testing.T) {
	t.Helper()

	checks := []struct {
		file   string
		fn     string
		method string
	}{
		{file: "methods_thread.go", fn: "threadForkTyped", method: "ForkThread"},
		{file: "methods_thread.go", fn: "threadReadTyped", method: "ListThreads"},
		{file: "server_approval.go", fn: "handleApprovalRequest", method: "Submit"},
		{file: "server_payload.go", fn: "AgentEventHandler", method: "GetThreadID"},
		{file: "server_dynamic_tools.go", fn: "handleDynamicToolCall", method: "RespondError"},
		{file: "server_dynamic_tools.go", fn: "handleDynamicToolCall", method: "SendDynamicToolResult"},
	}

	fset := token.NewFileSet()
	for _, item := range checks {
		file, err := parser.ParseFile(fset, item.file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", item.file, err)
		}
		if funcContainsClientMethodCall(file, item.fn, item.method) {
			t.Fatalf("%s in %s must not call proc.Client.%s directly", item.fn, item.file, item.method)
		}
		if !funcContainsCodexAdapterCall(file, item.fn) {
			t.Fatalf("%s in %s must delegate via s.codexAdapter", item.fn, item.file)
		}
	}
}

func TestP4NoDirectCodexPackageImportOutsideAdapter(t *testing.T) {
	t.Helper()

	root := "."
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if filepath.Clean(path) == filepath.Clean("codexadapter") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel := filepath.Clean(path)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			if imp == nil || imp.Path == nil {
				continue
			}
			importPath := strings.Trim(imp.Path.Value, "\"")
			if importPath == "github.com/multi-agent/go-agent-v2/internal/codex" {
				offenders = append(offenders, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk imports under internal/apiserver: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("unexpected internal/codex import outside codexadapter: %v", offenders)
	}
}

func funcContainsCodexAdapterCall(file *ast.File, fnName string) bool {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != fnName || fd.Body == nil {
			continue
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
	return false
}

func funcContainsPackageCall(file *ast.File, fnName, pkgName string) bool {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != fnName || fd.Body == nil {
			continue
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
	return false
}

func funcContainsClientMethodCall(file *ast.File, fnName, method string) bool {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != fnName || fd.Body == nil {
			continue
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
	return false
}
