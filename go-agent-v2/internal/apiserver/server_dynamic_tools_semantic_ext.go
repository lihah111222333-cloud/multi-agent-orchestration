package apiserver

import "github.com/multi-agent/go-agent-v2/internal/agentcore"

func init() {
	registerExtendedLSPDynamicToolProvider(
		"semantic.tools",
		func(s *Server) {
			s.dynTools["lsp_semantic_tokens"] = s.lspTools.SemanticTokens
			s.dynTools["lsp_folding_range"] = s.lspTools.FoldingRange
		},
		func(_ *Server) []agentcore.DynamicTool {
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
	)
}
