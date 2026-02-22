package apiserver

import (
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

func init() {
	registerExtendedLSPDynamicToolProvider(
		"actions.tools",
		func(s *Server) {
			s.dynTools["lsp_code_action"] = s.lspCodeAction
			s.dynTools["lsp_signature_help"] = s.lspSignatureHelp
			s.dynTools["lsp_format"] = s.lspFormat
		},
		func(_ *Server) []codex.DynamicTool {
			return []codex.DynamicTool{
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

func (s *Server) lspCodeAction(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath  string   `json:"file_path"`
		Line      *int     `json:"line"`
		Column    *int     `json:"column"`
		EndLine   *int     `json:"end_line"`
		EndColumn *int     `json:"end_column"`
		Only      []string `json:"only"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	if p.Line == nil {
		return "error: line is required"
	}
	if p.Column == nil {
		return "error: column is required"
	}
	if *p.Line < 0 || *p.Column < 0 {
		return "error: line and column must be >= 0"
	}

	endLine := -1
	if p.EndLine != nil {
		endLine = *p.EndLine
	}
	endColumn := -1
	if p.EndColumn != nil {
		endColumn = *p.EndColumn
	}

	result, err := s.lsp.CodeAction(p.FilePath, *p.Line, *p.Column, endLine, endColumn, p.Only)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no code action found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func (s *Server) lspSignatureHelp(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath string `json:"file_path"`
		Line     *int   `json:"line"`
		Column   *int   `json:"column"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	if p.Line == nil {
		return "error: line is required"
	}
	if p.Column == nil {
		return "error: column is required"
	}
	if *p.Line < 0 || *p.Column < 0 {
		return "error: line and column must be >= 0"
	}

	result, err := s.lsp.SignatureHelp(p.FilePath, *p.Line, *p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	if result == nil || len(result.Signatures) == 0 {
		return "no signature help found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func (s *Server) lspFormat(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath     string `json:"file_path"`
		TabSize      *int   `json:"tab_size"`
		InsertSpaces *bool  `json:"insert_spaces"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}

	tabSize := 4
	if p.TabSize != nil {
		tabSize = *p.TabSize
	}
	insertSpaces := true
	if p.InsertSpaces != nil {
		insertSpaces = *p.InsertSpaces
	}

	result, err := s.lsp.Format(p.FilePath, tabSize, insertSpaces)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no formatting edits"
	}
	data, _ := json.Marshal(result)
	return string(data)
}
