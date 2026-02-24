package tools

import "github.com/multi-agent/go-agent-v2/internal/agentcore"

// LSPExtXRef returns the xref ext registry provider.
func LSPExtXRef() LSPExtRegistryProvider {
	return LSPExtRegistryProvider{
		Name: "xref.tools",
		Register: func(provider LSPProvider) {
			if provider == nil {
				return
			}
			provider.BindDynamicTool("lsp_workspace_symbol", provider.WorkspaceSymbol)
			provider.BindDynamicTool("lsp_implementation", provider.Implementation)
			provider.BindDynamicTool("lsp_type_definition", provider.TypeDefinition)
		},
		Build: func() []agentcore.DynamicTool {
			return []agentcore.DynamicTool{
				{
					Name:        "lsp_workspace_symbol",
					Description: "Search symbols in workspace by query. Requires exactly one selector: file_path+query or language+query.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file_path": map[string]any{"type": "string", "description": "Absolute or relative path used to infer language"},
							"language":  map[string]any{"type": "string", "description": "Language name or alias: go/rust/typescript/python/c"},
							"query":     map[string]any{"type": "string", "description": "Symbol query"},
						},
						"required": []string{"query"},
						"oneOf": []map[string]any{
							{
								"required": []string{"query", "file_path"},
								"not":      map[string]any{"required": []string{"language"}},
							},
							{
								"required": []string{"query", "language"},
								"not":      map[string]any{"required": []string{"file_path"}},
							},
						},
					},
				},
				{
					Name:        "lsp_implementation",
					Description: "Find implementation locations for symbol at file_path:line:column.",
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
					Name:        "lsp_type_definition",
					Description: "Find type definition locations for symbol at file_path:line:column.",
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
			}
		},
	}
}
