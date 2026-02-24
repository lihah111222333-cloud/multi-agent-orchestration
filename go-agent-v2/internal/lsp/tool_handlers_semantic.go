package lsp

import (
	"encoding/json"
	"strings"
)

// SemanticTokens gets document semantic tokens.
func (h *ToolHandlers) SemanticTokens(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.SemanticTokens(p.FilePath)
	if err != nil {
		return "error: " + err.Error()
	}
	if result == nil || (len(result.Data) == 0 && len(result.Decoded) == 0) {
		return "no semantic tokens found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// FoldingRange gets document folding ranges.
func (h *ToolHandlers) FoldingRange(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.FoldingRange(p.FilePath)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no folding range found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}
