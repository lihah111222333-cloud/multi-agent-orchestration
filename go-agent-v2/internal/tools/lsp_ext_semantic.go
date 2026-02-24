package tools

import "github.com/multi-agent/go-agent-v2/internal/agentcore"

// LSPExtSemantic returns the semantic ext registry provider.
func LSPExtSemantic() LSPExtRegistryProvider {
	return LSPExtRegistryProvider{
		Name: "semantic.tools",
		Register: func(provider LSPProvider) {
			if provider == nil {
				return
			}
			provider.BindDynamicTool("lsp_semantic_tokens", provider.SemanticTokens)
			provider.BindDynamicTool("lsp_folding_range", provider.FoldingRange)
		},
		Build: func() []agentcore.DynamicTool {
			return []agentcore.DynamicTool{
				{
					Name:        "lsp_semantic_tokens",
					Description: "Get semantic tokens for a file. Decoded token output is limited to 200 items.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
						},
						"required": []string{"file_path"},
					},
				},
				{
					Name:        "lsp_folding_range",
					Description: "Get folding ranges for a file with boundary filtering.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
						},
						"required": []string{"file_path"},
					},
				},
			}
		},
	}
}
