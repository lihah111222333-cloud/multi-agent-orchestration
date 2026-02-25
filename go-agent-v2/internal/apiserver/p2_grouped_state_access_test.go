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

func TestP2GroupedStateFieldAccessBoundaries(t *testing.T) {
	t.Helper()

	trackedStateFields := map[string]struct{}{
		"mu":                          {},
		"conns":                       {},
		"nextID":                      {},
		"pendingMu":                   {},
		"pending":                     {},
		"nextReqID":                   {},
		"diagMu":                      {},
		"diagCache":                   {},
		"codeRunMu":                   {},
		"activeCodeRuns":              {},
		"codeRunSeq":                  {},
		"agentWorkDirMu":              {},
		"agentWorkDirs":               {},
		"threadSeq":                   {},
		"fileChangeMu":                {},
		"fileChangeByThread":          {},
		"orchestrationReportMu":       {},
		"orchestrationPendingReports": {},
		"orchestrationReportTTL":      {},
		"uiThrottleMu":                {},
		"uiThrottleEntries":           {},
		"toolCallMu":                  {},
		"toolCallCount":               {},
		"sseMu":                       {},
		"sseClients":                  {},
		"notifyHookMu":                {},
		"notifyHook":                  {},
		"approvalInFlight":            {},
		"cleanupOnce":                 {},
		"threadAliasMu":               {},
	}

	allowedOutsideStateGroups := map[string]map[string]struct{}{
		"diagMu":    allowFileSet("server_diagnostics.go"),
		"diagCache": allowFileSet("server_diagnostics.go"),
	}

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
		if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, parseErr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse file %s: %v", name, parseErr)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil {
				return true
			}
			owner, ok := sel.X.(*ast.Ident)
			if !ok || owner.Name != "s" {
				return true
			}

			field := sel.Sel.Name
			if _, tracked := trackedStateFields[field]; !tracked {
				return true
			}
			if name == "server_state_groups.go" {
				return true
			}
			if allowed, ok := allowedOutsideStateGroups[field]; ok {
				if _, pass := allowed[name]; pass {
					return true
				}
			}

			pos := fset.Position(sel.Pos())
			violations = append(violations, pos.String()+": s."+field)
			return true
		})
	}

	if len(violations) > 0 {
		t.Fatalf("grouped state fields must be accessed via state accessors (except explicit boundary files):\n%s", strings.Join(violations, "\n"))
	}
}

func allowFileSet(names ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		out[trimmed] = struct{}{}
	}
	return out
}
