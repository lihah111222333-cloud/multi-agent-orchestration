package apiserver

import (
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

func init() {
	registerExtendedLSPDynamicToolProvider(
		"hierarchy.tools",
		func(s *Server) {
			s.dynTools["lsp_call_hierarchy"] = s.lspCallHierarchy
			s.dynTools["lsp_type_hierarchy"] = s.lspTypeHierarchy
		},
		func(_ *Server) []codex.DynamicTool {
			return []codex.DynamicTool{
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
	)
}

func (s *Server) lspCallHierarchy(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath  string `json:"file_path"`
		Line      int    `json:"line"`
		Column    int    `json:"column"`
		Direction string `json:"direction"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}

	result, err := s.lsp.CallHierarchy(p.FilePath, p.Line, p.Column, p.Direction)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no call hierarchy found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func (s *Server) lspTypeHierarchy(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath  string `json:"file_path"`
		Line      int    `json:"line"`
		Column    int    `json:"column"`
		Direction string `json:"direction"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}

	result, err := s.lsp.TypeHierarchy(p.FilePath, p.Line, p.Column, p.Direction)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no type hierarchy found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}
