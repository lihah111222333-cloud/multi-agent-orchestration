package apiserver

import "github.com/multi-agent/go-agent-v2/internal/agentcore"

func init() {
	registerExtendedLSPDynamicToolProvider(
		"actions.tools",
		func(s *Server) {
			s.dynTools["lsp_code_action"] = s.lspTools.CodeAction
			s.dynTools["lsp_signature_help"] = s.lspTools.SignatureHelp
			s.dynTools["lsp_format"] = s.lspTools.Format
		},
		func(_ *Server) []agentcore.DynamicTool {
			return []agentcore.DynamicTool{
				{
					Name:        "lsp_code_action",
					Description: "Get code actions/commands at a document range. Supports optional end_line/end_column and action kinds filter.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file_path":  map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
							"line":       map[string]any{"type": "number", "description": "0-indexed start line number"},
							"column":     map[string]any{"type": "number", "description": "0-indexed start column number"},
							"end_line":   map[string]any{"type": "number", "description": "0-indexed end line number (default: line)"},
							"end_column": map[string]any{"type": "number", "description": "0-indexed end column number (default: column)"},
							"only":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional code action kinds filter"},
						},
						"required": []string{"file_path", "line", "column"},
					},
				},
				{
					Name:        "lsp_signature_help",
					Description: "Get signature help at file_path:line:column.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
							"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
							"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
						},
						"required": []string{"file_path", "line", "column"},
					},
				},
				{
					Name:        "lsp_format",
					Description: "Get formatting text edits for a file. Returns TextEdit[] and does not apply edits automatically.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file_path":     map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
							"tab_size":      map[string]any{"type": "number", "description": "Tab size (default: 4)"},
							"insert_spaces": map[string]any{"type": "boolean", "description": "Use spaces for indentation (default: true)"},
						},
						"required": []string{"file_path"},
					},
				},
			}
		},
	)
}
