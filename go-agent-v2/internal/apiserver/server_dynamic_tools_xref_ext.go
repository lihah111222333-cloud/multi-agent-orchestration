package apiserver

import (
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
)

func init() {
	registerExtendedLSPDynamicToolProvider(
		"xref.tools",
		func(s *Server) {
			s.dynTools["lsp_workspace_symbol"] = s.lspWorkspaceSymbol
			s.dynTools["lsp_implementation"] = s.lspImplementation
			s.dynTools["lsp_type_definition"] = s.lspTypeDefinition
		},
		func(_ *Server) []codex.DynamicTool {
			return []codex.DynamicTool{
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
	)
}

func (s *Server) lspWorkspaceSymbol(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath string `json:"file_path"`
		Language string `json:"language"`
		Query    string `json:"query"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}

	p.Query = strings.TrimSpace(p.Query)
	p.FilePath = strings.TrimSpace(p.FilePath)
	p.Language = strings.TrimSpace(p.Language)

	if p.Query == "" {
		return "error: query is required"
	}
	if p.FilePath == "" && p.Language == "" {
		return "error: exactly one of file_path or language is required"
	}
	if p.FilePath != "" && p.Language != "" {
		return "error: file_path and language are mutually exclusive"
	}

	result, err := s.lsp.WorkspaceSymbol(p.FilePath, p.Language, p.Query)
	if err != nil {
		return "error: " + err.Error()
	}
	result = limitWorkspaceSymbolResults(result)
	if len(result) == 0 {
		return "no symbols found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func (s *Server) lspImplementation(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}

	result, err := s.lsp.Implementation(p.FilePath, p.Line, p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	result = limitLocationResults(result)
	if len(result) == 0 {
		return "no implementation found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func (s *Server) lspTypeDefinition(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}

	var p struct {
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}

	result, err := s.lsp.TypeDefinition(p.FilePath, p.Line, p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	result = limitLocationResults(result)
	if len(result) == 0 {
		return "no type definition found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func limitWorkspaceSymbolResults(in []lsp.WorkspaceSymbolResult) []lsp.WorkspaceSymbolResult {
	if len(in) <= lsp.XRefResultLimit {
		return in
	}
	out := make([]lsp.WorkspaceSymbolResult, lsp.XRefResultLimit)
	copy(out, in[:lsp.XRefResultLimit])
	return out
}

func limitLocationResults(in []lsp.LocationResult) []lsp.LocationResult {
	if len(in) <= lsp.XRefResultLimit {
		return in
	}
	out := make([]lsp.LocationResult, lsp.XRefResultLimit)
	copy(out, in[:lsp.XRefResultLimit])
	return out
}
