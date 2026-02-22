package apiserver

import (
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

func init() {
	registerExtendedLSPDynamicToolProvider(
		"semantic.tools",
		func(s *Server) {
			s.dynTools["lsp_semantic_tokens"] = s.lspSemanticTokens
			s.dynTools["lsp_folding_range"] = s.lspFoldingRange
		},
		func(_ *Server) []codex.DynamicTool {
			return []codex.DynamicTool{
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

func (s *Server) lspSemanticTokens(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}

	result, err := s.lsp.SemanticTokens(p.FilePath)
	if err != nil {
		return "error: " + err.Error()
	}
	if result == nil || (len(result.Data) == 0 && len(result.Decoded) == 0) {
		return "no semantic tokens found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func (s *Server) lspFoldingRange(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}

	result, err := s.lsp.FoldingRange(p.FilePath)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no folding range found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}
