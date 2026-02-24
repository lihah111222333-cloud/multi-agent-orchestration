package lsp

import (
	"encoding/json"
	"strings"
)

// WorkspaceSymbol searches workspace symbols.
func (h *ToolHandlers) WorkspaceSymbol(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.WorkspaceSymbol(p.FilePath, p.Language, p.Query)
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

// Implementation finds symbol implementation locations.
func (h *ToolHandlers) Implementation(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.Implementation(p.FilePath, p.Line, p.Column)
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

// TypeDefinition finds symbol type definition locations.
func (h *ToolHandlers) TypeDefinition(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.TypeDefinition(p.FilePath, p.Line, p.Column)
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

func limitWorkspaceSymbolResults(in []WorkspaceSymbolResult) []WorkspaceSymbolResult {
	if len(in) <= XRefResultLimit {
		return in
	}
	out := make([]WorkspaceSymbolResult, XRefResultLimit)
	copy(out, in[:XRefResultLimit])
	return out
}

func limitLocationResults(in []LocationResult) []LocationResult {
	if len(in) <= XRefResultLimit {
		return in
	}
	out := make([]LocationResult, XRefResultLimit)
	copy(out, in[:XRefResultLimit])
	return out
}
