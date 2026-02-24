package tools

import "github.com/multi-agent/go-agent-v2/internal/agentcore"

// LSPExtHierarchy returns the hierarchy ext registry provider.
func LSPExtHierarchy() LSPExtRegistryProvider {
	return LSPExtRegistryProvider{
		Name: "hierarchy.tools",
		Register: func(provider LSPProvider) {
			if provider == nil {
				return
			}
			provider.BindDynamicTool("lsp_call_hierarchy", provider.CallHierarchy)
			provider.BindDynamicTool("lsp_type_hierarchy", provider.TypeHierarchy)
		},
		Build: func() []agentcore.DynamicTool {
			return []agentcore.DynamicTool{
				{
					Name:        "lsp_call_hierarchy",
					Description: "Get call hierarchy for symbol at file_path:line:column. Direction: incoming|outgoing|both.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
							"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
							"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
							"direction": map[string]any{"type": "string", "enum": []string{"incoming", "outgoing", "both"}, "description": "Hierarchy direction (default: both)"},
						},
						"required": []string{"file_path", "line", "column"},
					},
				},
				{
					Name:        "lsp_type_hierarchy",
					Description: "Get type hierarchy for symbol at file_path:line:column. Direction: supertypes|subtypes|both.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
							"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
							"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
							"direction": map[string]any{"type": "string", "enum": []string{"supertypes", "subtypes", "both"}, "description": "Hierarchy direction (default: both)"},
						},
						"required": []string{"file_path", "line", "column"},
					},
				},
			}
		},
	}
}
