package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergedHandlersRouteNewActions(t *testing.T) {
	handler := &ToolHandlers{}

	tests := []struct {
		tool string
		call func(json.RawMessage) string
		args []map[string]any
	}{
		{
			tool: "lsp_file",
			call: handler.LSPFile,
			args: []map[string]any{
				{"action": "open_file", "file_path": "main.go"},
				{"action": "did_change", "file_path": "main.go", "new_content": "package main\n"},
				{"action": "diagnostics", "file_path": "main.go"},
			},
		},
		{
			tool: "lsp_inspect",
			call: handler.LSPInspect,
			args: []map[string]any{
				{"action": "hover", "file_path": "main.go", "line": 0, "column": 0},
				{"action": "definition", "file_path": "main.go", "line": 0, "column": 0},
				{"action": "references", "file_path": "main.go", "line": 0, "column": 0},
				{"action": "implementation", "file_path": "main.go", "line": 0, "column": 0},
				{"action": "type_definition", "file_path": "main.go", "line": 0, "column": 0},
				{"action": "signature_help", "file_path": "main.go", "line": 0, "column": 0},
				{"action": "diagnostics", "file_path": "main.go"},
			},
		},
		{
			tool: "lsp_xref",
			call: handler.LSPXRef,
			args: []map[string]any{
				{"action": "call_hierarchy", "file_path": "main.go", "line": 0, "column": 0},
				{"action": "type_hierarchy", "file_path": "main.go", "line": 0, "column": 0},
				{"action": "references", "file_path": "main.go", "line": 0, "column": 0},
			},
		},
		{
			tool: "lsp_structure",
			call: handler.LSPStructure,
			args: []map[string]any{
				{"action": "document_symbol", "file_path": "main.go"},
				{"action": "workspace_symbol", "language": "go", "query": "Handler"},
				{"action": "folding_range", "file_path": "main.go"},
				{"action": "semantic_tokens", "file_path": "main.go"},
			},
		},
		{
			tool: "lsp_grep",
			call: handler.LSPGrep,
			args: []map[string]any{
				{"action": "text_search", "query": "foo", "path": "."},
				{"action": "ast_search", "language": "go", "symbol": "Handler"},
			},
		},
		{
			tool: "lsp_edit",
			call: handler.LSPEdit,
			args: []map[string]any{
				{"action": "rename", "file_path": "main.go", "line": 0, "column": 0, "new_name": "x"},
				{"action": "code_action", "file_path": "main.go", "line": 0, "column": 0},
				{"action": "format", "file_path": "main.go"},
				{"action": "did_change", "file_path": "main.go", "new_content": "package main\n"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			for _, arg := range tt.args {
				raw, err := json.Marshal(arg)
				if err != nil {
					t.Fatalf("marshal args: %v", err)
				}
				result := tt.call(raw)
				unsupportedMarker := "unsupported " + tt.tool + " action"
				if strings.Contains(strings.ToLower(result), unsupportedMarker) {
					t.Fatalf("unexpected unsupported action for %s args=%v result=%q", tt.tool, arg, result)
				}
			}
		})
	}
}

func TestMergedHandlersRejectLegacyFileActions(t *testing.T) {
	handler := &ToolHandlers{}
	cases := []string{"open", "change"}

	for _, action := range cases {
		t.Run(action, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"action": action, "file_path": "main.go"})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			result := handler.LSPFile(raw)
			if !strings.Contains(strings.ToLower(result), "unsupported lsp_file action") {
				t.Fatalf("expected legacy action %q to be rejected, got %q", action, result)
			}
		})
	}
}
