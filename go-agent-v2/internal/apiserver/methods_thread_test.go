// methods_thread_test.go — 重构护栏: thread 操作相关纯函数的行为基线测试。
package apiserver

import (
	"encoding/json"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
)

// ========================================
// sanitizeArchiveName / sanitizeArchiveNameStrict
// ========================================

func TestSanitizeArchiveName(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"alphanumeric", "hello123", "hello123"},
		{"special_chars", "hello world!@#$%", "hello_world"},
		{"dots_dashes", "my-archive.v2", "my-archive.v2"},
		{"empty", "", ""},
		{"only_special", "!@#", ""},
		{"leading_dot", ".hidden", "hidden"},
		{"trailing_underscore", "name_", "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codexadapter.SanitizeArchiveName(tt.raw)
			if got != tt.want {
				t.Errorf("sanitizeArchiveName(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSanitizeArchiveNameStrict(t *testing.T) {
	// valid
	if name, err := codexadapter.SanitizeArchiveNameStrict("valid-name"); err != nil || name != "valid-name" {
		t.Errorf("valid: got %q, %v", name, err)
	}
	// empty → error
	if _, err := codexadapter.SanitizeArchiveNameStrict(""); err == nil {
		t.Error("empty: expected error, got nil")
	}
	// only special → error
	if _, err := codexadapter.SanitizeArchiveNameStrict("!@#"); err == nil {
		t.Error("special: expected error, got nil")
	}
}

// ========================================
// inferThreadArtifactKind
// ========================================

func TestInferThreadArtifactKind(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"rollout", "rollout-123.jsonl", "rollout"},
		{"breakpoint", "bp-snapshot.dat", "breakpoint"},
		{"shell", "setup.sh", "shell_snapshot"},
		{"jsonl_other", "events.jsonl", "jsonl"},
		{"unknown", "readme.md", "artifact"},
		{"empty", "", "artifact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codexadapter.InferThreadArtifactKind(tt.filename)
			if got != tt.want {
				t.Errorf("inferThreadArtifactKind(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

// ========================================
// pathWithinRoot
// ========================================

func TestPathWithinRoot(t *testing.T) {
	// Create temp dir for deterministic paths
	tmpDir := t.TempDir()
	child := filepath.Join(tmpDir, "sub", "file.txt")
	_ = os.MkdirAll(filepath.Dir(child), 0o755)

	within, err := codexadapter.PathWithinRoot(tmpDir, child)
	if err != nil || !within {
		t.Errorf("child: got %v, %v; want true, nil", within, err)
	}

	// root itself
	within, err = codexadapter.PathWithinRoot(tmpDir, tmpDir)
	if err != nil || !within {
		t.Errorf("self: got %v, %v; want true, nil", within, err)
	}

	// outside
	within, err = codexadapter.PathWithinRoot(tmpDir, "/tmp")
	if err != nil {
		t.Fatalf("outside: error=%v", err)
	}
	if within {
		t.Error("outside: got true, want false")
	}
}

// ========================================
// normalizeThreadArchiveMap
// ========================================

func TestNormalizeThreadArchiveMap(t *testing.T) {
	// nil → empty map
	got := codexadapter.NormalizeThreadArchiveMap(nil)
	if len(got) != 0 {
		t.Errorf("nil: got %v", got)
	}

	// map[string]int64
	got = codexadapter.NormalizeThreadArchiveMap(map[string]int64{"a": 123, "": 456})
	if len(got) != 1 || got["a"] != 123 {
		t.Errorf("int64 map: got %v", got)
	}

	// map[string]any with float64
	got = codexadapter.NormalizeThreadArchiveMap(map[string]any{"b": float64(789)})
	if got["b"] != 789 {
		t.Errorf("any map: got %v", got)
	}

	// JSON string
	got = codexadapter.NormalizeThreadArchiveMap(`{"c": 1000}`)
	if got["c"] != 1000 {
		t.Errorf("json string: got %v", got)
	}

	// json.RawMessage
	raw := json.RawMessage(`{"d": 2000}`)
	got = codexadapter.NormalizeThreadArchiveMap(raw)
	if got["d"] != 2000 {
		t.Errorf("raw message: got %v", got)
	}

	// zero value filtered
	got = codexadapter.NormalizeThreadArchiveMap(map[string]any{"e": float64(0)})
	if len(got) != 0 {
		t.Errorf("zero: got %v", got)
	}
}

func TestP4ThreadTurnRegisteredRoutesDelegateToCodexAdapter(t *testing.T) {
	t.Helper()

	checks := []string{
		"threadStartTyped",
		"threadResumeTyped",
		"threadRecoverTyped",
		"threadForkTyped",
		"threadArchiveTyped",
		"threadUnarchiveTyped",
		"threadNameSetTyped",
		"threadCompact",
		"threadRollbackTyped",
		"threadList",
		"threadLoadedList",
		"threadReadTyped",
		"threadResolveTyped",
		"threadMessagesTyped",
		"threadBgTerminalsClean",
		"turnStartTyped",
		"turnSteerTyped",
		"turnInterrupt",
		"turnForceComplete",
		"reviewStartTyped",
		"threadUndo",
		"threadModelSet",
		"threadPersonality",
		"threadApprovals",
		"threadMCPList",
		"threadSkillsList",
		"threadDebugMemory",
	}

	files := parseAPIServerNonTestFiles(t)
	for _, fn := range checks {
		fd, fileName, ok := findFuncDecl(files, fn)
		if !ok {
			t.Fatalf("function %s not found", fn)
		}
		if funcDeclContainsCodexAdapterCall(fd) {
			continue
		}
		if funcDeclContainsCallName(fd, "sendSlashCommand") || funcDeclContainsCallName(fd, "sendSlashCommandWithArgs") {
			continue
		}
		t.Fatalf("%s in %s must delegate via codexadapter or thin slash helper", fn, fileName)
	}
}

func funcDeclContainsCallName(fd *ast.FuncDecl, fnName string) bool {
	if fd == nil || fd.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fd.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			if fun.Name == fnName {
				found = true
				return false
			}
		case *ast.SelectorExpr:
			if fun.Sel != nil && fun.Sel.Name == fnName {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func TestP4ThreadTurnRouteSetRemainsStable(t *testing.T) {
	t.Helper()

	gotHandlers := parseThreadTurnReviewRouteHandlers(t)
	got := make([]string, 0, len(gotHandlers))
	for method := range gotHandlers {
		got = append(got, method)
	}
	sort.Strings(got)

	want := []string{
		"review/start",
		"thread/approvals/set",
		"thread/archive",
		"thread/backgroundTerminals/clean",
		"thread/compact/start",
		"thread/debugMemory",
		"thread/fork",
		"thread/list",
		"thread/loaded/list",
		"thread/mcp/list",
		"thread/messages",
		"thread/model/set",
		"thread/name/set",
		"thread/personality/set",
		"thread/read",
		"thread/recover",
		"thread/resolve",
		"thread/resume",
		"thread/rollback",
		"thread/skills/list",
		"thread/start",
		"thread/unarchive",
		"thread/undo",
		"turn/forceComplete",
		"turn/interrupt",
		"turn/start",
		"turn/steer",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("thread/turn/review registered route set drifted\nwant: %v\ngot:  %v", want, got)
	}
}

func TestP4ThreadTurnRouteBindingsRemainDelegates(t *testing.T) {
	t.Helper()

	got := parseThreadTurnReviewRouteHandlers(t)
	want := map[string]string{
		"thread/start":                     "threadStartTyped",
		"thread/resume":                    "threadResumeTyped",
		"thread/recover":                   "threadRecoverTyped",
		"thread/fork":                      "threadForkTyped",
		"thread/archive":                   "threadArchiveTyped",
		"thread/unarchive":                 "threadUnarchiveTyped",
		"thread/name/set":                  "threadNameSetTyped",
		"thread/compact/start":             "threadCompact",
		"thread/rollback":                  "threadRollbackTyped",
		"thread/list":                      "threadList",
		"thread/loaded/list":               "threadLoadedList",
		"thread/read":                      "threadReadTyped",
		"thread/resolve":                   "threadResolveTyped",
		"thread/messages":                  "threadMessagesTyped",
		"thread/backgroundTerminals/clean": "threadBgTerminalsClean",
		"turn/start":                       "turnStartTyped",
		"turn/steer":                       "turnSteerTyped",
		"turn/interrupt":                   "turnInterrupt",
		"turn/forceComplete":               "turnForceComplete",
		"review/start":                     "reviewStartTyped",
		"thread/undo":                      "threadUndo",
		"thread/model/set":                 "threadModelSet",
		"thread/personality/set":           "threadPersonality",
		"thread/approvals/set":             "threadApprovals",
		"thread/mcp/list":                  "threadMCPList",
		"thread/skills/list":               "threadSkillsList",
		"thread/debugMemory":               "threadDebugMemory",
	}

	if len(got) != len(want) {
		t.Fatalf("unexpected thread/turn/review route binding count: want=%d got=%d", len(want), len(got))
	}
	for method, wantHandler := range want {
		gotHandler, ok := got[method]
		if !ok {
			t.Fatalf("missing route binding: %s", method)
		}
		if gotHandler != wantHandler {
			t.Fatalf("route %s must bind to %s, got %s", method, wantHandler, gotHandler)
		}
	}
}

func TestP4ThreadTurnBoundHandlersStayThin(t *testing.T) {
	t.Helper()

	bindings := parseThreadTurnReviewRouteHandlers(t)
	files := parseAPIServerNonTestFiles(t)

	seen := map[string]struct{}{}
	for _, handler := range bindings {
		if _, ok := seen[handler]; ok {
			continue
		}
		seen[handler] = struct{}{}

		fd, fileName, ok := findFuncDecl(files, handler)
		if !ok {
			t.Fatalf("handler %s not found", handler)
		}

		// 入口函数必须保持薄委派（参数处理 + adapter/slash helper 调用 + 返回）。
		if len(fd.Body.List) > 5 {
			t.Fatalf("%s in %s has too many top-level statements: %d", handler, fileName, len(fd.Body.List))
		}
		if !funcDeclContainsCodexAdapterCall(fd) &&
			!funcDeclContainsCallName(fd, "sendSlashCommand") &&
			!funcDeclContainsCallName(fd, "sendSlashCommandWithArgs") {
			t.Fatalf("%s in %s must delegate via codexadapter or slash command helper", handler, fileName)
		}

		var forCount, rangeCount, goCount, deferCount, selectCount int
		ast.Inspect(fd.Body, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.ForStmt:
				forCount++
			case *ast.RangeStmt:
				rangeCount++
			case *ast.GoStmt:
				goCount++
			case *ast.DeferStmt:
				deferCount++
			case *ast.SelectStmt:
				selectCount++
			}
			return true
		})
		if forCount > 0 {
			t.Fatalf("%s in %s must not contain for-loops", handler, fileName)
		}
		if rangeCount > 0 && handler != "threadSkillsList" {
			t.Fatalf("%s in %s must not contain range-loops", handler, fileName)
		}
		if goCount > 0 || deferCount > 0 || selectCount > 0 {
			t.Fatalf("%s in %s must stay synchronous thin wrapper (go=%d defer=%d select=%d)", handler, fileName, goCount, deferCount, selectCount)
		}
	}
}

func parseThreadTurnReviewRouteHandlers(t *testing.T) map[string]string {
	t.Helper()

	files := parseAPIServerNonTestFiles(t)
	fd, fileName, ok := findFuncDecl(files, "registerMethods")
	if !ok {
		t.Fatalf("registerMethods not found")
	}

	out := make(map[string]string)
	ast.Inspect(fd.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		methodName, ok := extractRegisteredMethodName(assign.Lhs[0])
		if !ok {
			return true
		}
		if !strings.HasPrefix(methodName, "thread/") &&
			!strings.HasPrefix(methodName, "turn/") &&
			methodName != "review/start" {
			return true
		}
		handlerName, ok := extractServerHandlerName(assign.Rhs[0])
		if !ok {
			t.Fatalf("%s in %s must bind to server method handler", methodName, fileName)
		}
		out[methodName] = handlerName
		return true
	})
	return out
}

func extractRegisteredMethodName(expr ast.Expr) (string, bool) {
	indexExpr, ok := expr.(*ast.IndexExpr)
	if !ok {
		return "", false
	}
	selector, ok := indexExpr.X.(*ast.SelectorExpr)
	if !ok || selector.Sel == nil || selector.Sel.Name != "methods" {
		return "", false
	}
	recv, ok := selector.X.(*ast.Ident)
	if !ok || recv.Name != "s" {
		return "", false
	}
	key, ok := indexExpr.Index.(*ast.BasicLit)
	if !ok || key.Kind != token.STRING {
		return "", false
	}
	return strings.Trim(key.Value, "\""), true
}

func extractServerHandlerName(expr ast.Expr) (string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if ok {
		recv, recvOK := selector.X.(*ast.Ident)
		if recvOK && recv.Name == "s" && selector.Sel != nil {
			return selector.Sel.Name, true
		}
		return "", false
	}

	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != "typedHandler" || len(call.Args) != 1 {
		return "", false
	}
	handlerSel, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok || handlerSel.Sel == nil {
		return "", false
	}
	recv, ok := handlerSel.X.(*ast.Ident)
	if !ok || recv.Name != "s" {
		return "", false
	}
	return handlerSel.Sel.Name, true
}
