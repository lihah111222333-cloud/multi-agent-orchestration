package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Hover calls LSP hover.
func (h *ToolHandlers) Hover(args json.RawMessage) string {
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
	result, err := h.manager.Hover(p.FilePath, p.Line, p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	if result == nil {
		return "no hover info available"
	}
	return result.Contents.Value
}

// OpenFile opens file and triggers LSP analysis.
func (h *ToolHandlers) OpenFile(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	p.FilePath = strings.TrimSpace(p.FilePath)
	if p.FilePath == "" {
		return "error: file_path is required"
	}
	content, err := os.ReadFile(p.FilePath)
	if err != nil {
		return "error: reading file: " + err.Error()
	}
	if err := h.manager.OpenFile(p.FilePath, string(content)); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("opened %s (%d bytes)", p.FilePath, len(content))
}

// Diagnostics returns current diagnostics from cache.
func (h *ToolHandlers) Diagnostics(args json.RawMessage) string {
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: unmarshal diagnostics params: " + err.Error()
	}
	p.FilePath = strings.TrimSpace(p.FilePath)

	if p.FilePath != "" && !h.managerUnavailable() {
		if err := h.manager.BootstrapDocument(p.FilePath); err != nil {
			return "error: " + err.Error()
		}
		// Diagnostics arrive asynchronously via notifications; wait once to improve first lookup hit rate.
		time.Sleep(120 * time.Millisecond)
	}

	accessor := h.diagnosticsAccessor()
	if accessor == nil {
		return "no diagnostics"
	}

	if p.FilePath != "" {
		uri := normalizeDiagnosticsURI(p.FilePath)
		diags := accessor.GetDiagnostics(uri)
		if len(diags) == 0 {
			return "no diagnostics"
		}
		var sb strings.Builder
		for _, d := range diags {
			fmt.Fprintf(&sb, "%s:%d:%d %s\n", p.FilePath, d.Range.Start.Line+1, d.Range.Start.Character, d.Message)
		}
		return sb.String()
	}

	all := accessor.GetAllDiagnostics()
	if len(all) == 0 {
		return "no diagnostics"
	}
	var sb strings.Builder
	for uri, diags := range all {
		for _, d := range diags {
			fmt.Fprintf(&sb, "%s:%d:%d %s\n", uri, d.Range.Start.Line+1, d.Range.Start.Character, d.Message)
		}
	}
	if sb.Len() == 0 {
		return "no diagnostics"
	}
	return sb.String()
}

// Definition performs go-to-definition.
func (h *ToolHandlers) Definition(args json.RawMessage) string {
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
	locs, err := h.manager.Definition(p.FilePath, p.Line, p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(locs) == 0 {
		return "no definition found"
	}
	data, _ := json.Marshal(locs)
	return string(data)
}

// References finds symbol references.
func (h *ToolHandlers) References(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath    string `json:"file_path"`
		Line        int    `json:"line"`
		Column      int    `json:"column"`
		IncludeDecl *bool  `json:"include_declaration"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	includeDecl := true
	if p.IncludeDecl != nil {
		includeDecl = *p.IncludeDecl
	}
	locs, err := h.manager.References(p.FilePath, p.Line, p.Column, includeDecl)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(locs) == 0 {
		return "no references found"
	}
	data, _ := json.Marshal(locs)
	return string(data)
}

// DocumentSymbol returns file symbols.
func (h *ToolHandlers) DocumentSymbol(args json.RawMessage) string {
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
	symbols, err := h.manager.DocumentSymbol(p.FilePath)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(symbols) == 0 {
		return "no symbols found"
	}
	data, _ := json.Marshal(symbols)
	return string(data)
}

// Rename renames symbol.
func (h *ToolHandlers) Rename(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
		NewName  string `json:"new_name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	if strings.TrimSpace(p.NewName) == "" {
		return "error: new_name is required"
	}
	edit, err := h.manager.Rename(p.FilePath, p.Line, p.Column, p.NewName)
	if err != nil {
		return "error: " + err.Error()
	}
	if edit == nil || (len(edit.Changes) == 0 && len(edit.DocumentChanges) == 0) {
		return "no edits produced"
	}
	data, _ := json.Marshal(edit)
	return string(data)
}

// Completion returns code completion items.
func (h *ToolHandlers) Completion(args json.RawMessage) string {
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
	items, err := h.manager.Completion(p.FilePath, p.Line, p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(items) == 0 {
		return "no completions"
	}
	if len(items) > 50 {
		items = items[:50]
	}
	data, _ := json.Marshal(items)
	return string(data)
}

// DidChange notifies full content changes.
func (h *ToolHandlers) DidChange(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath   string `json:"file_path"`
		NewContent string `json:"new_content"`
		Version    int    `json:"version"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
	}
	if p.Version == 0 {
		p.Version = 2
	}
	if err := h.manager.ChangeFile(p.FilePath, p.Version, p.NewContent); err != nil {
		return "error: " + err.Error()
	}
	return "ok: file content updated"
}

func normalizeDiagnosticsURI(filePath string) string {
	uri := strings.TrimSpace(filePath)
	if uri == "" {
		return ""
	}
	if strings.HasPrefix(uri, "file://") {
		return uri
	}
	abs, _ := filepath.Abs(uri)
	return "file://" + abs
}
