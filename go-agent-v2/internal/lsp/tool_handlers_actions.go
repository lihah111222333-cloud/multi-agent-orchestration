package lsp

import (
	"encoding/json"
	"strings"
)

// CodeAction gets code actions at a range.
func (h *ToolHandlers) CodeAction(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.CodeAction(p.FilePath, *p.Line, *p.Column, endLine, endColumn, p.Only)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no code action found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// SignatureHelp gets signature help at a position.
func (h *ToolHandlers) SignatureHelp(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.SignatureHelp(p.FilePath, *p.Line, *p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	if result == nil || len(result.Signatures) == 0 {
		return "no signature help found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// Format returns formatting edits.
func (h *ToolHandlers) Format(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.Format(p.FilePath, tabSize, insertSpaces)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no formatting edits"
	}
	data, _ := json.Marshal(result)
	return string(data)
}
