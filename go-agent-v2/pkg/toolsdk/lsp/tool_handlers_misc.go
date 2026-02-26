package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DiagnosticsAccessor defines the thread-safe diagnostics cache surface required by ToolHandlers.
type DiagnosticsAccessor interface {
	SetDiagnostics(uri string, diagnostics []Diagnostic)
	GetDiagnostics(uri string) []Diagnostic
	GetAllDiagnostics() map[string][]Diagnostic
}

// ToolHandlers provides dynamic-tool compatible LSP handlers backed by Manager.
type ToolHandlers struct {
	manager     *Manager
	diagnostics DiagnosticsAccessor
}

// NewToolHandlers creates a ToolHandlers set with manager + diagnostics cache access.
func NewToolHandlers(manager *Manager, diagnostics DiagnosticsAccessor) *ToolHandlers {
	return &ToolHandlers{
		manager:     manager,
		diagnostics: diagnostics,
	}
}

func (h *ToolHandlers) managerUnavailable() bool {
	return h == nil || h.manager == nil
}

func (h *ToolHandlers) diagnosticsAccessor() DiagnosticsAccessor {
	if h == nil {
		return nil
	}
	return h.diagnostics
}

// BindDynamicTool is a no-op binder on ToolHandlers.
//
// Dynamic tool ext registration is assembled by tooladapter via its own
// binding context. This method exists to satisfy tools.LSPProvider interface.
func (h *ToolHandlers) BindDynamicTool(_ string, _ func(json.RawMessage) string) {}

// AvailabilitySummary returns language server availability summary for UI/config use.
func (h *ToolHandlers) AvailabilitySummary() map[string]any {
	summary := map[string]any{
		"hasManager":           !h.managerUnavailable(),
		"hasAvailableServer":   false,
		"availableServerCount": 0,
		"servers":              []map[string]any{},
	}
	if h.managerUnavailable() {
		return summary
	}

	statuses := h.manager.Statuses()
	sort.SliceStable(statuses, func(i, j int) bool {
		return statuses[i].Language < statuses[j].Language
	})

	serverRows := make([]map[string]any, 0, len(statuses))
	availableCount := 0
	for _, st := range statuses {
		if st.Available {
			availableCount++
		}
		serverRows = append(serverRows, map[string]any{
			"language":  st.Language,
			"command":   st.Command,
			"available": st.Available,
			"running":   st.Running,
		})
	}

	summary["servers"] = serverRows
	summary["availableServerCount"] = availableCount
	summary["hasAvailableServer"] = availableCount > 0
	return summary
}

// DiagnosticsQuery returns diagnostics in JSON-RPC compatible map form.
func (h *ToolHandlers) DiagnosticsQuery(filePath string) map[string]any {
	if h.managerUnavailable() {
		return map[string]any{}
	}
	accessor := h.diagnosticsAccessor()
	if accessor == nil {
		return map[string]any{}
	}

	formatDiagnostics := func(diags []Diagnostic) []map[string]any {
		out := make([]map[string]any, 0, len(diags))
		for _, d := range diags {
			out = append(out, map[string]any{
				"message":  d.Message,
				"severity": d.Severity.String(),
				"line":     d.Range.Start.Line,
				"column":   d.Range.Start.Character,
			})
		}
		return out
	}

	result := map[string]any{}
	trimmed := strings.TrimSpace(filePath)
	if trimmed != "" {
		uri := trimmed
		if !strings.HasPrefix(uri, "file://") {
			if abs, err := filepath.Abs(uri); err == nil {
				uri = "file://" + abs
			}
		}
		diags := accessor.GetDiagnostics(uri)
		if len(diags) > 0 {
			result[uri] = formatDiagnostics(diags)
		}
		return result
	}

	for uri, diags := range accessor.GetAllDiagnostics() {
		if len(diags) == 0 {
			continue
		}
		result[uri] = formatDiagnostics(diags)
	}
	return result
}

func (h *ToolHandlers) contextualToolError(toolName, filePath string, line, column int, err error) string {
	if err == nil {
		return "error: unknown lsp error"
	}
	base := "error: " + err.Error()
	hint := lspToolCursorHint(toolName, err.Error())
	if hint == "" {
		return base
	}
	if hover := h.hoverSummaryAt(filePath, line, column); hover != "" {
		return base + "\nhint: " + hint + "\nhover: " + hover
	}
	return base + "\nhint: " + hint
}

func lspToolCursorHint(toolName, errText string) string {
	lower := strings.ToLower(strings.TrimSpace(errText))

	typeNameMismatch := strings.Contains(lower, "not a type name") ||
		strings.Contains(lower, "cannot find type name") ||
		strings.Contains(lower, "pkgname, not a type")

	notMethod := strings.Contains(lower, "not a method") ||
		strings.Contains(lower, "is a function, not a method")

	switch toolName {
	case "lsp_type_hierarchy":
		if typeNameMismatch || notMethod {
			return "place cursor on a type or interface identifier (for example Handler), not on package names or function declarations"
		}
	case "lsp_type_definition":
		if typeNameMismatch || notMethod {
			return "place cursor on an expression or identifier with a concrete type (for example a variable, field, or interface name)"
		}
	case "lsp_implementation":
		if typeNameMismatch || notMethod {
			return "place cursor on an interface type name, an interface method declaration, or a method call site"
		}
	}
	return ""
}

func (h *ToolHandlers) hoverSummaryAt(filePath string, line, column int) string {
	if h.managerUnavailable() || strings.TrimSpace(filePath) == "" || line < 0 || column < 0 {
		return ""
	}
	result, err := h.manager.Hover(filePath, line, column)
	if err != nil || result == nil {
		return ""
	}
	return firstHoverSummaryLine(result.Contents.Value)
}

func firstHoverSummaryLine(contents string) string {
	for _, raw := range strings.Split(contents, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line == "---" {
			continue
		}
		if strings.HasPrefix(line, "```") {
			continue
		}
		line = strings.Trim(line, "`")
		if line == "" {
			continue
		}
		if len(line) > 160 {
			return line[:157] + "..."
		}
		return line
	}
	return ""
}

type lspHierarchyParam struct {
	FilePath  string `json:"file_path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Direction string `json:"direction"`
}

// CallHierarchy gets call hierarchy entries.
func (h *ToolHandlers) CallHierarchy(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_call_hierarchy", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspHierarchyParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]CallHierarchyResult, error) {
			return h.manager.CallHierarchy(filePath, params.Line, params.Column, params.Direction)
		},
		nil,
		"no call hierarchy found",
		func(result []CallHierarchyResult) bool { return len(result) == 0 },
		func(result []CallHierarchyResult) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		"line", params.Line,
		"column", params.Column,
		"direction", params.Direction,
	)
}

// TypeHierarchy gets type hierarchy entries.
func (h *ToolHandlers) TypeHierarchy(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_type_hierarchy", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspHierarchyParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]TypeHierarchyResult, error) {
			return h.manager.TypeHierarchy(filePath, params.Line, params.Column, params.Direction)
		},
		func(err error) string {
			return h.contextualToolError("lsp_type_hierarchy", filePath, params.Line, params.Column, err)
		},
		"no type hierarchy found",
		func(result []TypeHierarchyResult) bool { return len(result) == 0 },
		func(result []TypeHierarchyResult) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		"line", params.Line,
		"column", params.Column,
		"direction", params.Direction,
	)
}

// SemanticTokens gets document semantic tokens.
func (h *ToolHandlers) SemanticTokens(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_semantic_tokens", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}

	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() (*SemanticTokensResult, error) {
			return h.manager.SemanticTokens(filePath)
		},
		nil,
		"no semantic tokens found",
		func(result *SemanticTokensResult) bool {
			return result == nil || (len(result.Data) == 0 && len(result.Decoded) == 0)
		},
		func(result *SemanticTokensResult) []any {
			if result == nil {
				return []any{"result_count", 0}
			}
			return []any{
				"result_count", len(result.Decoded),
				"raw_token_count", len(result.Data),
			}
		},
		logger.FieldPath, resolvedPath,
	)
}

// FoldingRange gets document folding ranges.
func (h *ToolHandlers) FoldingRange(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_folding_range", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}

	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]FoldingRange, error) {
			return h.manager.FoldingRange(filePath)
		},
		nil,
		"no folding range found",
		func(result []FoldingRange) bool { return len(result) == 0 },
		func(result []FoldingRange) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
	)
}

type lspFilePathParam struct {
	FilePath string `json:"file_path"`
}

type lspFilePositionParam struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

type lspReferencesParam struct {
	FilePath    string `json:"file_path"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	IncludeDecl *bool  `json:"include_declaration"`
}

type lspRenameParam struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	NewName  string `json:"new_name"`
}

type lspDidChangeParam struct {
	FilePath      string `json:"file_path"`
	NewContent    string `json:"new_content"`
	Version       int    `json:"version"`
	PersistToDisk bool   `json:"persist_to_disk"`
}

type lspReplaceRangeParam struct {
	FilePath      string `json:"file_path"`
	Line          *int   `json:"line"`
	Column        *int   `json:"column"`
	EndLine       *int   `json:"end_line"`
	EndColumn     *int   `json:"end_column"`
	NewText       string `json:"new_text"`
	Version       int    `json:"version"`
	PersistToDisk bool   `json:"persist_to_disk"`
}

func decodeArgs[T any](args json.RawMessage) (T, error) {
	var params T
	if err := json.Unmarshal(args, &params); err != nil {
		return params, err
	}
	return params, nil
}

func requireFilePath(filePath string) (string, error) {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return "", fmt.Errorf("file_path is required")
	}
	return trimmed, nil
}

func requireIntParam(name string, value *int) (int, error) {
	if value == nil {
		return 0, fmt.Errorf("%s is required", name)
	}
	return *value, nil
}

func requireNonNegativePosition(line, column int) error {
	if line < 0 || column < 0 {
		return fmt.Errorf("line and column must be >= 0")
	}
	return nil
}

func toolError(err error) string {
	if err == nil {
		return "error: unknown error"
	}
	return "error: " + err.Error()
}

func runAndMarshal[T any](run func() (T, error), emptyMsg string, isEmpty func(T) bool) string {
	return runAndMarshalWithError(run, nil, emptyMsg, isEmpty)
}

func runAndMarshalWithError[T any](
	run func() (T, error),
	formatErr func(error) string,
	emptyMsg string,
	isEmpty func(T) bool,
) string {
	result, err := run()
	if err != nil {
		if formatErr != nil {
			return formatErr(err)
		}
		return toolError(err)
	}
	if isEmpty != nil && isEmpty(result) {
		return emptyMsg
	}
	data, err := json.Marshal(result)
	if err != nil {
		return toolError(err)
	}
	return string(data)
}

// Hover calls LSP hover.
func (h *ToolHandlers) Hover(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_hover", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePositionParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	result, err := h.manager.Hover(filePath, params.Line, params.Column)
	if err != nil {
		call.fail(err,
			logger.FieldPath, resolvedPath,
			"line", params.Line,
			"column", params.Column,
			"stage", "execute",
		)
		return toolError(err)
	}
	if result == nil {
		call.done(
			logger.FieldPath, resolvedPath,
			"line", params.Line,
			"column", params.Column,
			"result_empty", true,
		)
		return "no hover info available"
	}
	call.done(
		logger.FieldPath, resolvedPath,
		"line", params.Line,
		"column", params.Column,
		"result_empty", false,
		"content_len", len(result.Contents.Value),
	)
	return result.Contents.Value
}

// OpenFile opens file and triggers LSP analysis.
func (h *ToolHandlers) OpenFile(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_open_file", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	content, err := os.ReadFile(filePath)
	if err != nil {
		call.fail(err,
			logger.FieldPath, resolvedPath,
			"stage", "read_file",
		)
		return "error: reading file: " + err.Error()
	}
	if err := h.manager.OpenFile(filePath, string(content)); err != nil {
		call.fail(err,
			logger.FieldPath, resolvedPath,
			logger.FieldBytes, len(content),
			"stage", "execute",
		)
		return toolError(err)
	}
	call.done(
		logger.FieldPath, resolvedPath,
		logger.FieldBytes, len(content),
		"result_empty", false,
	)
	return fmt.Sprintf("opened %s (%d bytes)", filePath, len(content))
}

// ReadFile returns full file content.
func (h *ToolHandlers) ReadFile(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_read_file", args)
	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	content, err := os.ReadFile(filePath)
	if err != nil {
		call.fail(err,
			logger.FieldPath, resolvedPath,
			"stage", "read_file",
		)
		return "error: reading file: " + err.Error()
	}
	call.done(
		logger.FieldPath, resolvedPath,
		logger.FieldBytes, len(content),
		"result_empty", len(content) == 0,
	)
	return string(content)
}

// Diagnostics returns current diagnostics from cache.
func (h *ToolHandlers) Diagnostics(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_diagnostics", args)
	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return "error: unmarshal diagnostics params: " + err.Error()
	}
	params.FilePath = strings.TrimSpace(params.FilePath)
	resolvedPath := lspToolLogPath(params.FilePath)

	if params.FilePath != "" && !h.managerUnavailable() {
		if err := h.manager.BootstrapDocument(params.FilePath); err != nil {
			call.fail(err,
				logger.FieldPath, resolvedPath,
				"stage", "bootstrap",
			)
			return toolError(err)
		}
		// Diagnostics arrive asynchronously via notifications; wait once to improve first lookup hit rate.
		time.Sleep(120 * time.Millisecond)
	}

	accessor := h.diagnosticsAccessor()
	if accessor == nil {
		call.done(
			logger.FieldPath, resolvedPath,
			"result_empty", true,
			"diagnostics_count", 0,
			"diagnostics_source", "none",
		)
		return "no diagnostics"
	}

	if params.FilePath != "" {
		uri := normalizeDiagnosticsURI(params.FilePath)
		diags := accessor.GetDiagnostics(uri)
		if len(diags) == 0 {
			call.done(
				logger.FieldPath, resolvedPath,
				"result_empty", true,
				"diagnostics_count", 0,
				"diagnostics_source", "file",
			)
			return "no diagnostics"
		}
		var sb strings.Builder
		for _, diagnostic := range diags {
			fmt.Fprintf(
				&sb,
				"%s:%d:%d %s\n",
				params.FilePath,
				diagnostic.Range.Start.Line+1,
				diagnostic.Range.Start.Character,
				diagnostic.Message,
			)
		}
		call.done(
			logger.FieldPath, resolvedPath,
			"result_empty", false,
			"diagnostics_count", len(diags),
			"diagnostics_source", "file",
		)
		return sb.String()
	}

	all := accessor.GetAllDiagnostics()
	if len(all) == 0 {
		call.done(
			"result_empty", true,
			"diagnostics_count", 0,
			"diagnostics_source", "all",
		)
		return "no diagnostics"
	}
	var sb strings.Builder
	totalCount := 0
	for uri, diags := range all {
		for _, diagnostic := range diags {
			fmt.Fprintf(
				&sb,
				"%s:%d:%d %s\n",
				uri,
				diagnostic.Range.Start.Line+1,
				diagnostic.Range.Start.Character,
				diagnostic.Message,
			)
			totalCount++
		}
	}
	if sb.Len() == 0 {
		call.done(
			"result_empty", true,
			"diagnostics_count", 0,
			"diagnostics_source", "all",
		)
		return "no diagnostics"
	}
	call.done(
		"result_empty", false,
		"diagnostics_count", totalCount,
		"diagnostics_source", "all",
	)
	return sb.String()
}

// Definition performs go-to-definition.
func (h *ToolHandlers) Definition(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_definition", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePositionParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]Location, error) { return h.manager.Definition(filePath, params.Line, params.Column) },
		nil,
		"no definition found",
		func(result []Location) bool { return len(result) == 0 },
		func(result []Location) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		"line", params.Line,
		"column", params.Column,
	)
}

// References finds symbol references.
func (h *ToolHandlers) References(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_references", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspReferencesParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	includeDecl := true
	if params.IncludeDecl != nil {
		includeDecl = *params.IncludeDecl
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]Location, error) {
			return h.manager.References(filePath, params.Line, params.Column, includeDecl)
		},
		nil,
		"no references found",
		func(result []Location) bool { return len(result) == 0 },
		func(result []Location) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		"line", params.Line,
		"column", params.Column,
		"include_declaration", includeDecl,
	)
}

// DocumentSymbol returns file symbols.
func (h *ToolHandlers) DocumentSymbol(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_document_symbol", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]DocumentSymbol, error) { return h.manager.DocumentSymbol(filePath) },
		nil,
		"no symbols found",
		func(result []DocumentSymbol) bool { return len(result) == 0 },
		func(result []DocumentSymbol) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
	)
}

// Rename renames symbol.
func (h *ToolHandlers) Rename(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_rename", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspRenameParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	if strings.TrimSpace(params.NewName) == "" {
		call.fail(fmt.Errorf("new_name is required"),
			logger.FieldPath, lspToolLogPath(filePath),
			"line", params.Line,
			"column", params.Column,
			"stage", "validate",
		)
		return "error: new_name is required"
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() (*WorkspaceEdit, error) {
			return h.manager.Rename(filePath, params.Line, params.Column, params.NewName)
		},
		nil,
		"no edits produced",
		func(result *WorkspaceEdit) bool {
			return result == nil || (len(result.Changes) == 0 && len(result.DocumentChanges) == 0)
		},
		func(result *WorkspaceEdit) []any {
			return []any{"result_count", workspaceEditCount(result)}
		},
		logger.FieldPath, resolvedPath,
		"line", params.Line,
		"column", params.Column,
		"new_name_len", len(params.NewName),
	)
}

// Completion returns code completion items.
func (h *ToolHandlers) Completion(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_completion", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePositionParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]CompletionItem, error) {
			items, err := h.manager.Completion(filePath, params.Line, params.Column)
			if err != nil {
				return nil, err
			}
			if len(items) > 50 {
				items = items[:50]
			}
			return items, nil
		},
		nil,
		"no completions",
		func(result []CompletionItem) bool { return len(result) == 0 },
		func(result []CompletionItem) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		"line", params.Line,
		"column", params.Column,
	)
}

// DidChange notifies full content changes.
func (h *ToolHandlers) DidChange(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_did_change", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspDidChangeParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	if params.Version == 0 {
		params.Version = 2
	}
	resolvedPath := lspToolLogPath(filePath)
	baseAttrs := []any{
		logger.FieldPath, resolvedPath,
		logger.FieldVersion, params.Version,
		logger.FieldBytes, len(params.NewContent),
		"persist_to_disk", params.PersistToDisk,
	}

	if params.PersistToDisk {
		call.step("persist_begin",
			append(baseAttrs,
				"phase", "persist",
			)...,
		)
		if err := writeTextFileAtomic(filePath, params.NewContent); err != nil {
			call.fail(err, append(baseAttrs,
				"phase", "persist",
				"stage", "persist_write",
			)...)
			return "error: writing file: " + err.Error()
		}
		call.step("persist_done",
			append(baseAttrs,
				"phase", "persist",
			)...,
		)
	}

	call.step("lsp_sync_begin",
		append(baseAttrs,
			"phase", "lsp_sync",
		)...,
	)
	if err := h.manager.ChangeFile(filePath, params.Version, params.NewContent); err != nil {
		if params.PersistToDisk {
			call.step("lsp_sync_unavailable_after_persist",
				append(baseAttrs,
					"phase", "lsp_sync",
					"stage", "execute",
					"warning", err.Error(),
				)...,
			)
			call.done(append(baseAttrs,
				"result_empty", false,
				"lsp_sync_warning", err.Error(),
			)...)
			return "ok: file content updated and persisted to disk (lsp sync unavailable: " + err.Error() + ")"
		}
		call.fail(err, append(baseAttrs,
			"phase", "lsp_sync",
			"stage", "execute",
		)...)
		return toolError(err)
	}

	call.step("lsp_sync_done",
		append(baseAttrs,
			"phase", "lsp_sync",
		)...,
	)
	call.done(append(baseAttrs,
		"result_empty", false,
	)...)

	if params.PersistToDisk {
		return "ok: file content updated and persisted to disk"
	}
	return "ok: file content updated (lsp-only, disk not written)"
}

// ReplaceRange replaces text within [line:column, end_line:end_column) and applies the edit via did_change.
func (h *ToolHandlers) ReplaceRange(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_replace_range", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}

	params, err := decodeArgs[lspReplaceRangeParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}

	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	line, err := requireIntParam("line", params.Line)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	column, err := requireIntParam("column", params.Column)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	endLine, err := requireIntParam("end_line", params.EndLine)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	endColumn, err := requireIntParam("end_column", params.EndColumn)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	if err := requireNonNegativePosition(line, column); err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	if err := requireNonNegativePosition(endLine, endColumn); err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	if endLine < line || (endLine == line && endColumn < column) {
		err := fmt.Errorf("end position must be >= start position")
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	baseContent, contentSource, err := h.loadReplaceRangeBaseContent(filePath)
	if err != nil {
		call.fail(err,
			logger.FieldPath, lspToolLogPath(filePath),
			"stage", "load_content",
		)
		return toolError(err)
	}

	startOffset, err := lineColumnOffset(baseContent, line, column)
	if err != nil {
		call.fail(err,
			logger.FieldPath, lspToolLogPath(filePath),
			"line", line,
			"column", column,
			"stage", "resolve_start",
		)
		return toolError(err)
	}
	endOffset, err := lineColumnOffset(baseContent, endLine, endColumn)
	if err != nil {
		call.fail(err,
			logger.FieldPath, lspToolLogPath(filePath),
			"end_line", endLine,
			"end_column", endColumn,
			"stage", "resolve_end",
		)
		return toolError(err)
	}
	if endOffset < startOffset {
		err := fmt.Errorf("invalid range: end offset before start offset")
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	nextContent := baseContent[:startOffset] + params.NewText + baseContent[endOffset:]
	didChangePayload := map[string]any{
		"file_path":       filePath,
		"new_content":     nextContent,
		"version":         params.Version,
		"persist_to_disk": params.PersistToDisk,
	}
	var raw map[string]any
	if err := json.Unmarshal(args, &raw); err == nil && raw != nil {
		if meta, ok := raw[lspToolCallMetaKey]; ok {
			didChangePayload[lspToolCallMetaKey] = meta
		}
	}

	didChangeArgs, err := json.Marshal(didChangePayload)
	if err != nil {
		call.fail(err, "stage", "marshal_did_change")
		return toolError(err)
	}
	result := h.DidChange(didChangeArgs)
	normalizedResult := strings.ToLower(strings.TrimSpace(result))
	if strings.HasPrefix(normalizedResult, "error:") {
		call.fail(
			errors.New(strings.TrimSpace(result)),
			logger.FieldPath, lspToolLogPath(filePath),
			"line", line,
			"column", column,
			"end_line", endLine,
			"end_column", endColumn,
			"stage", "did_change",
		)
		return result
	}

	call.done(
		logger.FieldPath, lspToolLogPath(filePath),
		"line", line,
		"column", column,
		"end_line", endLine,
		"end_column", endColumn,
		"replacement_len", len(params.NewText),
		"replaced_len", endOffset-startOffset,
		"content_source", contentSource,
		"persist_to_disk", params.PersistToDisk,
	)
	return result
}

func (h *ToolHandlers) loadReplaceRangeBaseContent(filePath string) (string, string, error) {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return "", "", fmt.Errorf("file_path is required")
	}

	if !h.managerUnavailable() {
		uri := pathToURI(path)
		lock := h.manager.documentLock(uri)
		lock.Lock()
		state := h.manager.documentState(uri)
		if state != nil && state.Open {
			content := state.Content
			lock.Unlock()
			return content, "memory", nil
		}
		lock.Unlock()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("reading file: %w", err)
	}
	return string(data), "disk", nil
}

func lineColumnOffset(content string, targetLine, targetColumn int) (int, error) {
	if targetLine < 0 || targetColumn < 0 {
		return 0, fmt.Errorf("line and column must be >= 0")
	}

	line := 0
	column := 0
	for idx, r := range content {
		if line == targetLine && column == targetColumn {
			return idx, nil
		}
		if r == '\n' {
			line++
			column = 0
			continue
		}
		column++
	}
	if line == targetLine && column == targetColumn {
		return len(content), nil
	}
	return 0, fmt.Errorf("position out of range: line=%d column=%d", targetLine, targetColumn)
}

func workspaceEditCount(result *WorkspaceEdit) int {
	if result == nil {
		return 0
	}
	return len(result.Changes) + len(result.DocumentChanges)
}

func writeTextFileAtomic(filePath, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(filePath); err == nil {
		if perm := info.Mode().Perm(); perm != 0 {
			mode = perm
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(filePath)+".lsp-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
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

type lspCodeActionParam struct {
	FilePath  string   `json:"file_path"`
	Line      *int     `json:"line"`
	Column    *int     `json:"column"`
	EndLine   *int     `json:"end_line"`
	EndColumn *int     `json:"end_column"`
	Only      []string `json:"only"`
}

type lspSignatureHelpParam struct {
	FilePath string `json:"file_path"`
	Line     *int   `json:"line"`
	Column   *int   `json:"column"`
}

type lspFormatParam struct {
	FilePath     string `json:"file_path"`
	TabSize      *int   `json:"tab_size"`
	InsertSpaces *bool  `json:"insert_spaces"`
}

// CodeAction gets code actions at a range.
func (h *ToolHandlers) CodeAction(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_code_action", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspCodeActionParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	line, err := requireIntParam("line", params.Line)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	column, err := requireIntParam("column", params.Column)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	if err := requireNonNegativePosition(line, column); err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	endLine := -1
	if params.EndLine != nil {
		endLine = *params.EndLine
	}
	endColumn := -1
	if params.EndColumn != nil {
		endColumn = *params.EndColumn
	}

	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]CodeActionResult, error) {
			return h.manager.CodeAction(filePath, line, column, endLine, endColumn, params.Only)
		},
		nil,
		"no code action found",
		func(result []CodeActionResult) bool { return len(result) == 0 },
		func(result []CodeActionResult) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		"line", line,
		"column", column,
		"end_line", endLine,
		"end_column", endColumn,
		"only_count", len(params.Only),
	)
}

// SignatureHelp gets signature help at a position.
func (h *ToolHandlers) SignatureHelp(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_signature_help", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspSignatureHelpParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	line, err := requireIntParam("line", params.Line)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	column, err := requireIntParam("column", params.Column)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	if err := requireNonNegativePosition(line, column); err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() (*SignatureHelpResult, error) {
			return h.manager.SignatureHelp(filePath, line, column)
		},
		nil,
		"no signature help found",
		func(result *SignatureHelpResult) bool { return result == nil || len(result.Signatures) == 0 },
		func(result *SignatureHelpResult) []any {
			if result == nil {
				return []any{"result_count", 0}
			}
			return []any{"result_count", len(result.Signatures)}
		},
		logger.FieldPath, resolvedPath,
		"line", line,
		"column", column,
	)
}

// Format returns formatting edits.
func (h *ToolHandlers) Format(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_format", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFormatParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	tabSize := 4
	if params.TabSize != nil {
		tabSize = *params.TabSize
	}
	insertSpaces := true
	if params.InsertSpaces != nil {
		insertSpaces = *params.InsertSpaces
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]TextEdit, error) { return h.manager.Format(filePath, tabSize, insertSpaces) },
		nil,
		"no formatting edits",
		func(result []TextEdit) bool { return len(result) == 0 },
		func(result []TextEdit) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		"tab_size", tabSize,
		"insert_spaces", insertSpaces,
	)
}

const lspToolLogSchema = "lsp_tool_v1"
const lspToolCallMetaKey = "__tool_call_meta"

var errLSPManagerUnavailable = errors.New("lsp manager unavailable")

type lspToolCallLogger struct {
	tool      string
	startedAt time.Time
	baseAttrs []any
}

func startLSPToolCallFromArgs(tool string, args json.RawMessage, attrs ...any) *lspToolCallLogger {
	baseAttrs := make([]any, 0, len(attrs)+8)
	baseAttrs = append(baseAttrs, "raw_args_len", len(args))
	baseAttrs = append(baseAttrs, attrs...)
	baseAttrs = append(baseAttrs, lspToolCallMetaAttrs(args)...)
	return startLSPToolCall(tool, baseAttrs...)
}

func startLSPToolCall(tool string, attrs ...any) *lspToolCallLogger {
	call := &lspToolCallLogger{
		tool:      strings.TrimSpace(tool),
		startedAt: time.Now(),
		baseAttrs: append([]any(nil), attrs...),
	}
	call.step("begin")
	return call
}

func (c *lspToolCallLogger) done(attrs ...any) {
	c.emit("done", nil, true, attrs...)
}

func (c *lspToolCallLogger) fail(err error, attrs ...any) {
	c.emit("failed", err, true, attrs...)
}

func (c *lspToolCallLogger) step(event string, attrs ...any) {
	c.emit(event, nil, false, attrs...)
}

func (c *lspToolCallLogger) emit(event string, err error, withDuration bool, attrs ...any) {
	if c == nil {
		return
	}
	fields := make([]any, 0, len(c.baseAttrs)+len(attrs)+12)
	fields = append(
		fields,
		"log_schema", lspToolLogSchema,
		"ai_readable", true,
		logger.FieldToolName, c.tool,
		"event", strings.TrimSpace(event),
	)
	if withDuration {
		fields = append(fields, logger.FieldDurationMS, time.Since(c.startedAt).Milliseconds())
	}
	fields = append(fields, c.baseAttrs...)
	fields = append(fields, attrs...)
	if err != nil {
		fields = append(fields, logger.FieldError, err)
		logger.Warn("lsp_tool_event", fields...)
		return
	}
	logger.Info("lsp_tool_event", fields...)
}

func lspToolLogPath(filePath string) string {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "file://") {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func lspToolCallMetaAttrs(args json.RawMessage) []any {
	if len(args) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(args, &payload); err != nil || payload == nil {
		return nil
	}
	meta, ok := payload[lspToolCallMetaKey].(map[string]any)
	if !ok || meta == nil {
		return nil
	}

	attrs := make([]any, 0, 8)
	if value := strings.TrimSpace(fmt.Sprint(meta["agent_id"])); value != "" && value != "<nil>" {
		attrs = append(attrs, logger.FieldAgentID, value)
	}
	if value := strings.TrimSpace(fmt.Sprint(meta["call_id"])); value != "" && value != "<nil>" {
		attrs = append(attrs, logger.FieldCallID, value)
	}
	if value := strings.TrimSpace(fmt.Sprint(meta["thread_id"])); value != "" && value != "<nil>" {
		attrs = append(attrs, logger.FieldThreadID, value)
	}
	if reqID, ok := normalizeLSPRequestID(meta["request_id"]); ok {
		attrs = append(attrs, logger.FieldReqID, reqID)
	}
	return attrs
}

func normalizeLSPRequestID(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case int:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float64:
		if v == 0 {
			return "0", true
		}
		return strconv.FormatInt(int64(v), 10), true
	case float32:
		if v == 0 {
			return "0", true
		}
		return strconv.FormatInt(int64(v), 10), true
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return "", false
		}
		return text, true
	}
}

func runAndMarshalLogged[T any](
	call *lspToolCallLogger,
	run func() (T, error),
	formatErr func(error) string,
	emptyMsg string,
	isEmpty func(T) bool,
	resultAttrs func(T) []any,
	doneAttrs ...any,
) string {
	result, err := run()
	if err != nil {
		call.fail(err, append(doneAttrs, "stage", "execute")...)
		if formatErr != nil {
			return formatErr(err)
		}
		return toolError(err)
	}

	empty := false
	if isEmpty != nil {
		empty = isEmpty(result)
	}

	finalAttrs := append([]any(nil), doneAttrs...)
	if resultAttrs != nil {
		finalAttrs = append(finalAttrs, resultAttrs(result)...)
	}
	finalAttrs = append(finalAttrs, "result_empty", empty)
	call.done(finalAttrs...)

	if empty {
		return emptyMsg
	}

	data, err := json.Marshal(result)
	if err != nil {
		call.fail(err, append(doneAttrs, "stage", "marshal")...)
		return toolError(err)
	}
	return string(data)
}

const (
	lspSearchTimeout     = 15 * time.Second
	lspSearchResultLimit = 50
	lspSearchSnippetMax  = 500
	lspSearchPayloadMax  = 16 * 1024
)

var (
	lspSearchLookPath       = exec.LookPath
	lspSearchCommandContext = exec.CommandContext
	lspSearchGetwd          = os.Getwd
)

var lspSearchExcludeDirs = []string{".git", "node_modules", "vendor", "dist"}

type lspTextSearchParam struct {
	Query         string `json:"query"`
	Path          string `json:"path"`
	Glob          string `json:"glob"`
	CaseSensitive bool   `json:"case_sensitive"`
	MaxResults    int    `json:"max_results"`
}

type lspASTSearchParam struct {
	Symbol     string `json:"symbol"`
	Language   string `json:"language"`
	Path       string `json:"path"`
	MaxResults int    `json:"max_results"`
}

type lspSearchMatch struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
	Text   string `json:"text"`
}

// TextSearch performs text search via ripgrep.
func (h *ToolHandlers) TextSearch(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_text_search", args)
	params, err := decodeArgs[lspTextSearchParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		err := errors.New("query is required")
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	workspaceRoot, target, err := resolveSearchTarget(params.Path)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	limit := normalizeSearchLimit(params.MaxResults)

	binaryPath, err := lspSearchLookPath("rg")
	if err != nil {
		err = errors.New("rg not found in PATH")
		call.fail(err,
			logger.FieldPath, target,
			"query_len", len(params.Query),
			"stage", "dependency",
		)
		return toolError(err)
	}

	cmdArgs := []string{"--vimgrep", "--no-heading", "--color", "never", "--max-count", strconv.Itoa(limit)}
	if params.CaseSensitive {
		cmdArgs = append(cmdArgs, "--case-sensitive")
	} else {
		cmdArgs = append(cmdArgs, "--ignore-case")
	}
	for _, excluded := range lspSearchExcludeDirs {
		cmdArgs = append(cmdArgs, "--glob", "!"+excluded+"/**")
	}
	if glob := strings.TrimSpace(params.Glob); glob != "" {
		cmdArgs = append(cmdArgs, "--glob", glob)
	}
	cmdArgs = append(cmdArgs, params.Query, target)

	output, err := runSearchCommand(binaryPath, cmdArgs, workspaceRoot)
	if err != nil {
		call.fail(err,
			logger.FieldPath, target,
			"query_len", len(params.Query),
			"stage", "execute",
		)
		return toolError(err)
	}

	matches := parseRipgrepVimgrepOutput(output, workspaceRoot, limit)
	matches = filterAndCapSearchMatches(matches)
	if len(matches) == 0 {
		call.done(
			logger.FieldPath, target,
			"query_len", len(params.Query),
			"result_count", 0,
			"result_empty", true,
		)
		return "no matches found"
	}

	data, err := json.Marshal(matches)
	if err != nil {
		call.fail(err, "stage", "marshal")
		return toolError(err)
	}
	call.done(
		logger.FieldPath, target,
		"query_len", len(params.Query),
		"result_count", len(matches),
		"result_empty", false,
	)
	return string(data)
}

// AstSearch performs AST pattern search via ast-grep.
func (h *ToolHandlers) AstSearch(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_ast_search", args)
	params, err := decodeArgs[lspASTSearchParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	params.Symbol = strings.TrimSpace(params.Symbol)
	params.Language = strings.TrimSpace(params.Language)
	if params.Symbol == "" {
		err := errors.New("symbol is required")
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	if params.Language == "" {
		err := errors.New("language is required")
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	workspaceRoot, target, err := resolveSearchTarget(params.Path)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	limit := normalizeSearchLimit(params.MaxResults)

	binaryPath, err := lspSearchLookPath("sg")
	if err != nil {
		err = errors.New("sg not found in PATH")
		call.fail(err,
			logger.FieldPath, target,
			logger.FieldLanguage, params.Language,
			"symbol_len", len(params.Symbol),
			"stage", "dependency",
		)
		return toolError(err)
	}

	cmdArgs := []string{"scan", "--json=stream", "-p", params.Symbol, "-l", params.Language, target}
	output, err := runSearchCommand(binaryPath, cmdArgs, workspaceRoot)
	if err != nil {
		call.fail(err,
			logger.FieldPath, target,
			logger.FieldLanguage, params.Language,
			"symbol_len", len(params.Symbol),
			"stage", "execute",
		)
		return toolError(err)
	}

	matches := parseASTGrepOutput(output, workspaceRoot, limit)
	matches = filterAndCapSearchMatches(matches)
	if len(matches) == 0 {
		call.done(
			logger.FieldPath, target,
			logger.FieldLanguage, params.Language,
			"symbol_len", len(params.Symbol),
			"result_count", 0,
			"result_empty", true,
		)
		return "no matches found"
	}

	data, err := json.Marshal(matches)
	if err != nil {
		call.fail(err, "stage", "marshal")
		return toolError(err)
	}
	call.done(
		logger.FieldPath, target,
		logger.FieldLanguage, params.Language,
		"symbol_len", len(params.Symbol),
		"result_count", len(matches),
		"result_empty", false,
	)
	return string(data)
}

func normalizeSearchLimit(v int) int {
	if v <= 0 {
		return lspSearchResultLimit
	}
	if v > lspSearchResultLimit {
		return lspSearchResultLimit
	}
	return v
}

func resolveSearchTarget(pathArg string) (workspaceRoot string, target string, err error) {
	workspaceRoot, err = searchWorkspaceRoot()
	if err != nil {
		return "", "", err
	}
	target = workspaceRoot
	trimmed := strings.TrimSpace(pathArg)
	if trimmed == "" {
		return workspaceRoot, target, nil
	}

	candidate := trimmed
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workspaceRoot, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", fmt.Errorf("resolve search path: %w", err)
	}
	resolvedCandidate, err := filepath.EvalSymlinks(absCandidate)
	if err != nil {
		return "", "", fmt.Errorf("path not found: %s", trimmed)
	}
	if !isWithinRoot(workspaceRoot, resolvedCandidate) {
		return "", "", fmt.Errorf("path out of workspace root: %s", trimmed)
	}
	return workspaceRoot, resolvedCandidate, nil
}

func searchWorkspaceRoot() (string, error) {
	wd, err := lspSearchGetwd()
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	absWD, err := filepath.Abs(wd)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absWD)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	return filepath.Clean(absWD), nil
}

func isWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func runSearchCommand(binaryPath string, args []string, workDir string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), lspSearchTimeout)
	defer cancel()

	cmd := lspSearchCommandContext(ctx, binaryPath, args...)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("search timed out after %s", lspSearchTimeout)
	}
	if err == nil {
		return output, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil, nil
	}

	msg := strings.TrimSpace(string(output))
	if msg == "" {
		msg = err.Error()
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return nil, errors.New(msg)
}

func parseRipgrepVimgrepOutput(output []byte, workspaceRoot string, limit int) []lspSearchMatch {
	if len(output) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return nil
	}
	matches := make([]lspSearchMatch, 0, lspSearchMinInt(limit, len(lines)))
	for _, raw := range lines {
		if len(matches) >= limit {
			break
		}
		match, ok := parseRipgrepVimgrepLine(raw, workspaceRoot)
		if !ok {
			continue
		}
		matches = append(matches, match)
	}
	return matches
}

func parseRipgrepVimgrepLine(raw, workspaceRoot string) (lspSearchMatch, bool) {
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) < 4 {
		return lspSearchMatch{}, false
	}
	line, err := strconv.Atoi(parts[1])
	if err != nil {
		return lspSearchMatch{}, false
	}
	column, err := strconv.Atoi(parts[2])
	if err != nil {
		return lspSearchMatch{}, false
	}
	path := strings.TrimSpace(parts[0])
	if path == "" {
		return lspSearchMatch{}, false
	}
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(workspaceRoot, path)
	}
	return lspSearchMatch{
		Path:   displayPath(workspaceRoot, absPath),
		Line:   line,
		Column: column,
		Text:   truncateSearchText(parts[3]),
	}, true
}

func parseASTGrepOutput(output []byte, workspaceRoot string, limit int) []lspSearchMatch {
	if len(output) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return nil
	}
	matches := make([]lspSearchMatch, 0, lspSearchMinInt(limit, len(lines)))
	for _, raw := range lines {
		if len(matches) >= limit {
			break
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		match, ok := parseASTGrepLine(trimmed, workspaceRoot)
		if !ok {
			match = lspSearchMatch{Path: ".", Text: truncateSearchText(trimmed)}
		}
		if isExcludedPath(match.Path) {
			continue
		}
		matches = append(matches, match)
	}
	return matches
}

func parseASTGrepLine(raw, workspaceRoot string) (lspSearchMatch, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return lspSearchMatch{}, false
	}
	path := firstString(payload, "file", "path", "filename")
	if path == "" {
		return lspSearchMatch{}, false
	}
	line := 0
	column := 0
	if rangeValue, ok := payload["range"].(map[string]any); ok {
		if startValue, ok := rangeValue["start"].(map[string]any); ok {
			line = toInt(startValue["line"]) + 1
			column = toInt(startValue["column"]) + 1
		}
	}
	text := firstString(payload, "text", "match", "matched", "snippet")
	if text == "" {
		text = raw
	}

	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(workspaceRoot, absPath)
	}
	return lspSearchMatch{
		Path:   displayPath(workspaceRoot, absPath),
		Line:   line,
		Column: column,
		Text:   truncateSearchText(text),
	}, true
}

func filterAndCapSearchMatches(matches []lspSearchMatch) []lspSearchMatch {
	if len(matches) == 0 {
		return nil
	}
	filtered := make([]lspSearchMatch, 0, len(matches))
	for _, match := range matches {
		if isExcludedPath(match.Path) {
			continue
		}
		filtered = append(filtered, match)
	}
	for len(filtered) > 0 {
		data, err := json.Marshal(filtered)
		if err == nil && len(data) <= lspSearchPayloadMax {
			return filtered
		}
		filtered = filtered[:len(filtered)-1]
	}
	return nil
}

func isExcludedPath(path string) bool {
	slashPath := filepath.ToSlash(strings.ToLower(strings.TrimSpace(path)))
	if slashPath == "" {
		return false
	}
	for _, excluded := range lspSearchExcludeDirs {
		token := "/" + strings.ToLower(excluded) + "/"
		if strings.Contains("/"+slashPath+"/", token) {
			return true
		}
	}
	return false
}

func displayPath(workspaceRoot, absPath string) string {
	clean := filepath.Clean(absPath)
	if rel, err := filepath.Rel(workspaceRoot, clean); err == nil {
		if rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(clean)
}

func truncateSearchText(text string) string {
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	if len(runes) <= lspSearchSnippetMax {
		return trimmed
	}
	return string(runes[:lspSearchSnippetMax])
}

func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func toInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func lspSearchMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type lspWorkspaceSymbolParam struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Query    string `json:"query"`
}

// WorkspaceSymbol searches workspace symbols.
func (h *ToolHandlers) WorkspaceSymbol(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_workspace_symbol", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspWorkspaceSymbolParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}

	params.Query = strings.TrimSpace(params.Query)
	params.Path = strings.TrimSpace(params.Path)
	params.Language = strings.TrimSpace(params.Language)
	resolvedPath := lspToolLogPath(params.Path)

	if params.Query == "" {
		call.fail(
			errors.New("query is required"),
			logger.FieldPath, resolvedPath,
			logger.FieldLanguage, params.Language,
			"query_len", 0,
			"stage", "validate",
		)
		return "error: query is required"
	}
	if params.Path == "" && params.Language == "" {
		call.fail(
			errors.New("exactly one of path or language is required"),
			"query_len", len(params.Query),
			"stage", "validate",
		)
		return "error: exactly one of path or language is required"
	}
	if params.Path != "" && params.Language != "" {
		call.fail(
			errors.New("exactly one of path or language is required"),
			logger.FieldPath, resolvedPath,
			logger.FieldLanguage, params.Language,
			"query_len", len(params.Query),
			"stage", "validate",
		)
		return "error: exactly one of path or language is required"
	}
	if params.Path != "" {
		if info, statErr := os.Stat(params.Path); statErr == nil && info.IsDir() {
			err := errors.New("directory path is not supported for workspace_symbol; use language instead")
			call.fail(
				err,
				logger.FieldPath, resolvedPath,
				"query_len", len(params.Query),
				"stage", "validate",
			)
			return "error: " + err.Error()
		}
	}

	return runAndMarshalLogged(
		call,
		func() ([]WorkspaceSymbolResult, error) {
			result, err := h.manager.WorkspaceSymbol(params.Path, params.Language, params.Query)
			if err != nil {
				return nil, err
			}
			return limitWorkspaceSymbolResults(result), nil
		},
		nil,
		"no symbols found",
		func(result []WorkspaceSymbolResult) bool { return len(result) == 0 },
		func(result []WorkspaceSymbolResult) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		logger.FieldLanguage, params.Language,
		"query_len", len(params.Query),
	)
}

// Implementation finds symbol implementation locations.
func (h *ToolHandlers) Implementation(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_implementation", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePositionParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]LocationResult, error) {
			result, err := h.manager.Implementation(filePath, params.Line, params.Column)
			if err != nil {
				return nil, err
			}
			return limitLocationResults(result), nil
		},
		func(err error) string {
			return h.contextualToolError("lsp_implementation", filePath, params.Line, params.Column, err)
		},
		"no implementation found",
		func(result []LocationResult) bool { return len(result) == 0 },
		func(result []LocationResult) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		"line", params.Line,
		"column", params.Column,
	)
}

// TypeDefinition finds symbol type definition locations.
func (h *ToolHandlers) TypeDefinition(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_type_definition", args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePositionParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	resolvedPath := lspToolLogPath(filePath)
	return runAndMarshalLogged(
		call,
		func() ([]LocationResult, error) {
			result, err := h.manager.TypeDefinition(filePath, params.Line, params.Column)
			if err != nil {
				return nil, err
			}
			return limitLocationResults(result), nil
		},
		func(err error) string {
			return h.contextualToolError("lsp_type_definition", filePath, params.Line, params.Column, err)
		},
		"no type definition found",
		func(result []LocationResult) bool { return len(result) == 0 },
		func(result []LocationResult) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, resolvedPath,
		"line", params.Line,
		"column", params.Column,
	)
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

type lspMergedActionParam struct {
	Action string `json:"action"`
}

func decodeMergedAction(args json.RawMessage) (string, error) {
	p, err := decodeArgs[lspMergedActionParam](args)
	if err != nil {
		return "", err
	}
	if p.Action == "" {
		return "", fmt.Errorf("missing required param: action")
	}
	return p.Action, nil
}

// LSPFile routes file operations.
func (h *ToolHandlers) LSPFile(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "open_file":
		return h.OpenFile(args)
	case "read_file":
		return h.ReadFile(args)
	case "did_change":
		return h.DidChange(args)
	case "diagnostics":
		return h.Diagnostics(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_file action: %s", action))
	}
}

// LSPInspect routes inspect operations.
func (h *ToolHandlers) LSPInspect(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "hover":
		return h.Hover(args)
	case "definition":
		return h.Definition(args)
	case "implementation":
		return h.Implementation(args)
	case "type_definition":
		return h.TypeDefinition(args)
	case "signature_help":
		return h.SignatureHelp(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_inspect action: %s", action))
	}
}

// LSPXRef routes cross-reference operations.
func (h *ToolHandlers) LSPXRef(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "call_hierarchy":
		return h.CallHierarchy(args)
	case "type_hierarchy":
		return h.TypeHierarchy(args)
	case "references":
		return h.References(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_xref action: %s", action))
	}
}

// LSPGrep routes text/ast search operations.
func (h *ToolHandlers) LSPGrep(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "text_search":
		return h.TextSearch(args)
	case "ast_search":
		return h.AstSearch(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_grep action: %s", action))
	}
}

// LSPStructure routes structure/hierarchy operations.
func (h *ToolHandlers) LSPStructure(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "document_symbol":
		return h.DocumentSymbol(args)
	case "workspace_symbol":
		return h.WorkspaceSymbol(args)
	case "semantic_tokens":
		return h.SemanticTokens(args)
	case "folding_range":
		return h.FoldingRange(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_structure action: %s", action))
	}
}

// LSPEdit routes edit operations.
func (h *ToolHandlers) LSPEdit(args json.RawMessage) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	switch action {
	case "rename":
		return h.Rename(args)
	case "code_action":
		return h.CodeAction(args)
	case "format":
		return h.Format(args)
	case "replace_range":
		return h.ReplaceRange(args)
	default:
		return toolError(fmt.Errorf("unsupported lsp_edit action: %s", action))
	}
}
