package lsp

import (
	"encoding/json"
	"strings"
)

// CallHierarchy gets call hierarchy entries.
func (h *ToolHandlers) CallHierarchy(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.CallHierarchy(p.FilePath, p.Line, p.Column, p.Direction)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no call hierarchy found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// TypeHierarchy gets type hierarchy entries.
func (h *ToolHandlers) TypeHierarchy(args json.RawMessage) string {
	if h.managerUnavailable() {
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

	result, err := h.manager.TypeHierarchy(p.FilePath, p.Line, p.Column, p.Direction)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(result) == 0 {
		return "no type hierarchy found"
	}
	data, _ := json.Marshal(result)
	return string(data)
}
