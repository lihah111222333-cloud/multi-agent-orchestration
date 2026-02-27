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
	"reflect"
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

func (h *ToolHandlers) CallHierarchy(args json.RawMessage) string {
	return runHierarchyTool(
		h,
		"lsp_call_hierarchy",
		args,
		"no call hierarchy found",
		func(filePath string, line, column int, direction string) ([]CallHierarchyResult, error) {
			return h.manager.CallHierarchy(filePath, line, column, direction)
		},
	)
}

func (h *ToolHandlers) TypeHierarchy(args json.RawMessage) string {
	return runHierarchyTool(
		h,
		"lsp_type_hierarchy",
		args,
		"no type hierarchy found",
		func(filePath string, line, column int, direction string) ([]TypeHierarchyResult, error) {
			return h.manager.TypeHierarchy(filePath, line, column, direction)
		},
	)
}

func runHierarchyTool[T any](
	h *ToolHandlers,
	tool string,
	args json.RawMessage,
	emptyMsg string,
	run func(filePath string, line, column int, direction string) (T, error),
) string {
	call, ok := h.startManagedToolCall(tool, args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	params, err := decodeToolParams[lspHierarchyParam](call, args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return toolError(err)
	}
	doneAttrs := append(lspToolPathPositionAttrs(filePath, params.Line, params.Column), "direction", params.Direction)
	return runAndMarshalLogged(
		call,
		func() (T, error) { return run(filePath, params.Line, params.Column, params.Direction) },
		h.hierarchyToolErrorFormatter(tool, filePath, params.Line, params.Column),
		emptyMsg,
		func(result T) bool { return isHierarchyResultEmpty(result) },
		func(result T) []any { return []any{"result_count", hierarchyResultCount(result)} },
		doneAttrs...,
	)
}

func (h *ToolHandlers) hierarchyToolErrorFormatter(tool, filePath string, line, column int) func(error) string {
	if tool != "lsp_type_hierarchy" {
		return nil
	}
	return func(err error) string {
		return h.contextualToolError("lsp_type_hierarchy", filePath, line, column, err)
	}
}

func isHierarchyResultEmpty[T any](result T) bool {
	switch v := any(result).(type) {
	case []CallHierarchyResult:
		return len(v) == 0
	case []TypeHierarchyResult:
		return len(v) == 0
	default:
		return false
	}
}

func hierarchyResultCount[T any](result T) int {
	switch v := any(result).(type) {
	case []CallHierarchyResult:
		return len(v)
	case []TypeHierarchyResult:
		return len(v)
	default:
		return 0
	}
}

func (h *ToolHandlers) SemanticTokens(args json.RawMessage) string {
	return runFilePathManagerTool(
		h,
		"lsp_semantic_tokens",
		args,
		h.manager.SemanticTokens,
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
		nil,
	)
}

func (h *ToolHandlers) FoldingRange(args json.RawMessage) string {
	return runFilePathManagerTool(
		h,
		"lsp_folding_range",
		args,
		h.manager.FoldingRange,
		nil,
		"no folding range found",
		func(result []FoldingRange) bool { return len(result) == 0 },
		func(result []FoldingRange) []any { return []any{"result_count", len(result)} },
		nil,
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

func requireLineColumn(call *lspToolCallLogger, linePtr, columnPtr *int) (line, column int, err error) {
	line, err = requireIntParam("line", linePtr)
	if err != nil {
		call.fail(err, "stage", "validate")
		return 0, 0, err
	}
	column, err = requireIntParam("column", columnPtr)
	if err != nil {
		call.fail(err, "stage", "validate")
		return 0, 0, err
	}
	if err = requireNonNegativePosition(line, column); err != nil {
		call.fail(err, "stage", "validate")
		return 0, 0, err
	}
	return line, column, nil
}

func readToolFileContent(call *lspToolCallLogger, filePath, resolvedPath string) ([]byte, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		call.fail(err, logger.FieldPath, resolvedPath, "stage", "read_file")
		return nil, err
	}
	return content, nil
}

func (h *ToolHandlers) startManagedToolCall(tool string, args json.RawMessage) (*lspToolCallLogger, bool) {
	call := startLSPToolCallFromArgs(tool, args)
	if h.managerUnavailable() {
		call.fail(errLSPManagerUnavailable, "stage", "precheck")
		return call, false
	}
	return call, true
}

func decodeToolParams[T any](call *lspToolCallLogger, args json.RawMessage) (T, error) {
	params, err := decodeArgs[T](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		var zero T
		return zero, err
	}
	return params, nil
}

func requireToolFilePath(call *lspToolCallLogger, raw string) (string, error) {
	filePath, err := requireFilePath(raw)
	if err != nil {
		call.fail(err, "stage", "validate")
		return "", err
	}
	return filePath, nil
}

func lspToolPathAttrs(filePath string) []any {
	return []any{logger.FieldPath, lspToolLogPath(filePath)}
}

func lspToolPathPositionAttrs(filePath string, line, column int) []any {
	return []any{
		logger.FieldPath, lspToolLogPath(filePath),
		"line", line,
		"column", column,
	}
}

func runFilePathManagerTool[T any](
	h *ToolHandlers,
	tool string,
	args json.RawMessage,
	run func(filePath string) (T, error),
	formatErr func(filePath string, err error) string,
	emptyMsg string,
	isEmpty func(T) bool,
	resultAttrs func(T) []any,
	extraDoneAttrs func(filePath string) []any,
) string {
	call, ok := h.startManagedToolCall(tool, args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	params, err := decodeToolParams[lspFilePathParam](call, args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return toolError(err)
	}
	doneAttrs := lspToolPathAttrs(filePath)
	if extraDoneAttrs != nil {
		doneAttrs = append(doneAttrs, extraDoneAttrs(filePath)...)
	}
	return runAndMarshalLogged(
		call,
		func() (T, error) { return run(filePath) },
		func(err error) string {
			if formatErr != nil {
				return formatErr(filePath, err)
			}
			return toolError(err)
		},
		emptyMsg,
		isEmpty,
		resultAttrs,
		doneAttrs...,
	)
}

func runFilePositionManagerTool[T any](
	h *ToolHandlers,
	tool string,
	args json.RawMessage,
	run func(filePath string, line, column int) (T, error),
	formatErr func(filePath string, line, column int, err error) string,
	emptyMsg string,
	isEmpty func(T) bool,
	resultAttrs func(T) []any,
	extraDoneAttrs func(filePath string, line, column int) []any,
) string {
	call, ok := h.startManagedToolCall(tool, args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	params, err := decodeToolParams[lspFilePositionParam](call, args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return toolError(err)
	}
	doneAttrs := lspToolPathPositionAttrs(filePath, params.Line, params.Column)
	if extraDoneAttrs != nil {
		doneAttrs = append(doneAttrs, extraDoneAttrs(filePath, params.Line, params.Column)...)
	}
	return runAndMarshalLogged(
		call,
		func() (T, error) { return run(filePath, params.Line, params.Column) },
		func(err error) string {
			if formatErr != nil {
				return formatErr(filePath, params.Line, params.Column, err)
			}
			return toolError(err)
		},
		emptyMsg,
		isEmpty,
		resultAttrs,
		doneAttrs...,
	)
}

func toolError(err error) string {
	if err == nil {
		return "error: unknown error"
	}
	return "error: " + err.Error()
}

func (h *ToolHandlers) Hover(args json.RawMessage) string {
	call, ok := h.startManagedToolCall("lsp_hover", args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	params, err := decodeToolParams[lspFilePositionParam](call, args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return toolError(err)
	}
	attrs := lspToolPathPositionAttrs(filePath, params.Line, params.Column)
	result, err := h.manager.Hover(filePath, params.Line, params.Column)
	if err != nil {
		call.fail(err, append(attrs, "stage", "execute")...)
		return toolError(err)
	}
	if result == nil {
		call.done(append(attrs, "result_empty", true)...)
		return "no hover info available"
	}
	call.done(append(attrs, "result_empty", false, "content_len", len(result.Contents.Value))...)
	return result.Contents.Value
}

func (h *ToolHandlers) OpenFile(args json.RawMessage) string {
	call, filePath, resolvedPath, errText := h.decodeManagedFilePath("lsp_open_file", args)
	if errText != "" {
		return errText
	}
	content, err := readToolFileContent(call, filePath, resolvedPath)
	if err != nil {
		return "error: reading file: " + err.Error()
	}
	if err := h.manager.OpenFile(filePath, string(content)); err != nil {
		call.fail(err, logger.FieldPath, resolvedPath, logger.FieldBytes, len(content), "stage", "execute")
		return toolError(err)
	}
	call.done(logger.FieldPath, resolvedPath, logger.FieldBytes, len(content), "result_empty", false)
	return fmt.Sprintf("opened %s (%d bytes)", filePath, len(content))
}

func (h *ToolHandlers) ReadFile(args json.RawMessage) string {
	call, filePath, resolvedPath, errText := h.decodeFilePathToolCall("lsp_read_file", args)
	if errText != "" {
		return errText
	}
	content, err := readToolFileContent(call, filePath, resolvedPath)
	if err != nil {
		return "error: reading file: " + err.Error()
	}
	call.done(logger.FieldPath, resolvedPath, logger.FieldBytes, len(content), "result_empty", len(content) == 0)
	return string(content)
}

func (h *ToolHandlers) decodeManagedFilePath(
	tool string,
	args json.RawMessage,
) (call *lspToolCallLogger, filePath, resolvedPath string, errText string) {
	call, ok := h.startManagedToolCall(tool, args)
	if !ok {
		return nil, "", "", "error: lsp manager unavailable"
	}
	params, err := decodeToolParams[lspFilePathParam](call, args)
	if err != nil {
		return nil, "", "", toolError(err)
	}
	filePath, err = requireToolFilePath(call, params.FilePath)
	if err != nil {
		return nil, "", "", toolError(err)
	}
	return call, filePath, lspToolLogPath(filePath), ""
}

func (h *ToolHandlers) decodeFilePathToolCall(
	tool string,
	args json.RawMessage,
) (call *lspToolCallLogger, filePath, resolvedPath string, errText string) {
	call = startLSPToolCallFromArgs(tool, args)
	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return nil, "", "", toolError(err)
	}
	filePath, err = requireFilePath(params.FilePath)
	if err != nil {
		call.fail(err, "stage", "validate")
		return nil, "", "", toolError(err)
	}
	return call, filePath, lspToolLogPath(filePath), ""
}

func (h *ToolHandlers) Diagnostics(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_diagnostics", args)
	filePath, err := decodeDiagnosticsPath(args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return "error: unmarshal diagnostics params: " + err.Error()
	}
	resolvedPath := lspToolLogPath(filePath)
	if err := h.bootstrapDiagnosticsIfNeeded(call, filePath, resolvedPath); err != nil {
		return toolError(err)
	}

	text, count, source := h.collectDiagnostics(filePath)
	empty := text == ""
	call.done(diagnosticsDoneAttrs(filePath, resolvedPath, empty, count, source)...)
	if empty {
		return "no diagnostics"
	}
	return text
}

func decodeDiagnosticsPath(args json.RawMessage) (string, error) {
	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(params.FilePath), nil
}

func (h *ToolHandlers) bootstrapDiagnosticsIfNeeded(call *lspToolCallLogger, filePath, resolvedPath string) error {
	if filePath == "" || h.managerUnavailable() {
		return nil
	}
	if err := h.manager.BootstrapDocument(filePath); err != nil {
		call.fail(err, logger.FieldPath, resolvedPath, "stage", "bootstrap")
		return err
	}
	// Diagnostics arrive asynchronously via notifications; wait once to improve first lookup hit rate.
	time.Sleep(120 * time.Millisecond)
	return nil
}

func (h *ToolHandlers) collectDiagnostics(filePath string) (text string, count int, source string) {
	accessor := h.diagnosticsAccessor()
	if accessor == nil {
		return "", 0, "none"
	}
	if filePath != "" {
		diags := accessor.GetDiagnostics(normalizeDiagnosticsURI(filePath))
		return diagnosticsString(filePath, diags), len(diags), "file"
	}
	all := accessor.GetAllDiagnostics()
	if len(all) == 0 {
		return "", 0, "all"
	}
	total := 0
	var sb strings.Builder
	for uri, diags := range all {
		total += appendDiagnostics(&sb, uri, diags)
	}
	return sb.String(), total, "all"
}

func diagnosticsDoneAttrs(filePath, resolvedPath string, empty bool, count int, source string) []any {
	attrs := []any{
		"result_empty", empty,
		"diagnostics_count", count,
		"diagnostics_source", source,
	}
	if filePath != "" || source == "none" {
		return append([]any{logger.FieldPath, resolvedPath}, attrs...)
	}
	return attrs
}

func diagnosticsString(label string, diags []Diagnostic) string {
	if len(diags) == 0 {
		return ""
	}
	var sb strings.Builder
	_ = appendDiagnostics(&sb, label, diags)
	return sb.String()
}

func appendDiagnostics(sb *strings.Builder, label string, diags []Diagnostic) int {
	for _, diagnostic := range diags {
		fmt.Fprintf(
			sb,
			"%s:%d:%d %s\n",
			label,
			diagnostic.Range.Start.Line+1,
			diagnostic.Range.Start.Character,
			diagnostic.Message,
		)
	}
	return len(diags)
}

func (h *ToolHandlers) Definition(args json.RawMessage) string {
	return runFilePositionManagerTool(
		h,
		"lsp_definition",
		args,
		h.manager.Definition,
		nil,
		"no definition found",
		func(result []Location) bool { return len(result) == 0 },
		func(result []Location) []any { return []any{"result_count", len(result)} },
		nil,
	)
}

func (h *ToolHandlers) References(args json.RawMessage) string {
	call, ok := h.startManagedToolCall("lsp_references", args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	params, err := decodeToolParams[lspReferencesParam](call, args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return toolError(err)
	}
	includeDecl := true
	if params.IncludeDecl != nil {
		includeDecl = *params.IncludeDecl
	}
	return runAndMarshalLogged(
		call,
		func() ([]Location, error) {
			return h.manager.References(filePath, params.Line, params.Column, includeDecl)
		},
		nil,
		"no references found",
		func(result []Location) bool { return len(result) == 0 },
		func(result []Location) []any { return []any{"result_count", len(result)} },
		append(lspToolPathPositionAttrs(filePath, params.Line, params.Column), "include_declaration", includeDecl)...,
	)
}

func (h *ToolHandlers) DocumentSymbol(args json.RawMessage) string {
	return runFilePathManagerTool(
		h,
		"lsp_document_symbol",
		args,
		h.manager.DocumentSymbol,
		nil,
		"no symbols found",
		func(result []DocumentSymbol) bool { return len(result) == 0 },
		func(result []DocumentSymbol) []any { return []any{"result_count", len(result)} },
		nil,
	)
}

func (h *ToolHandlers) Rename(args json.RawMessage) string {
	call, ok := h.startManagedToolCall("lsp_rename", args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	params, err := decodeToolParams[lspRenameParam](call, args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return toolError(err)
	}
	if strings.TrimSpace(params.NewName) == "" {
		call.fail(fmt.Errorf("new_name is required"), append(lspToolPathPositionAttrs(filePath, params.Line, params.Column), "stage", "validate")...)
		return "error: new_name is required"
	}
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
		append(lspToolPathPositionAttrs(filePath, params.Line, params.Column), "new_name_len", len(params.NewName))...,
	)
}

func (h *ToolHandlers) Completion(args json.RawMessage) string {
	return runFilePositionManagerTool(
		h,
		"lsp_completion",
		args,
		func(filePath string, line, column int) ([]CompletionItem, error) {
			items, err := h.manager.Completion(filePath, line, column)
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
		nil,
	)
}

func (h *ToolHandlers) DidChange(args json.RawMessage) string {
	call, ok := h.startManagedToolCall("lsp_did_change", args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	params, filePath, baseAttrs, err := decodeDidChangeRequest(call, args)
	if err != nil {
		return toolError(err)
	}
	if err := persistDidChangeIfNeeded(call, filePath, params.NewContent, baseAttrs, params.PersistToDisk); err != nil {
		return "error: writing file: " + err.Error()
	}
	if result, done := h.syncDidChange(call, filePath, params, baseAttrs); done {
		return result
	}
	call.done(append(baseAttrs, "result_empty", false)...)
	return didChangeSuccessText(params.PersistToDisk)
}

func decodeDidChangeRequest(call *lspToolCallLogger, args json.RawMessage) (lspDidChangeParam, string, []any, error) {
	params, err := decodeToolParams[lspDidChangeParam](call, args)
	if err != nil {
		return lspDidChangeParam{}, "", nil, err
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return lspDidChangeParam{}, "", nil, err
	}
	if params.Version == 0 {
		params.Version = 2
	}
	baseAttrs := []any{
		logger.FieldPath, lspToolLogPath(filePath),
		logger.FieldVersion, params.Version,
		logger.FieldBytes, len(params.NewContent),
		"persist_to_disk", params.PersistToDisk,
	}
	return params, filePath, baseAttrs, nil
}

func persistDidChangeIfNeeded(
	call *lspToolCallLogger,
	filePath, newContent string,
	baseAttrs []any,
	persist bool,
) error {
	if !persist {
		return nil
	}
	call.step("persist_begin", append(baseAttrs, "phase", "persist")...)
	if err := writeTextFileAtomic(filePath, newContent); err != nil {
		call.fail(err, append(baseAttrs, "phase", "persist", "stage", "persist_write")...)
		return err
	}
	call.step("persist_done", append(baseAttrs, "phase", "persist")...)
	return nil
}

func (h *ToolHandlers) syncDidChange(
	call *lspToolCallLogger,
	filePath string,
	params lspDidChangeParam,
	baseAttrs []any,
) (string, bool) {
	call.step("lsp_sync_begin", append(baseAttrs, "phase", "lsp_sync")...)
	if err := h.manager.ChangeFile(filePath, params.Version, params.NewContent); err != nil {
		if params.PersistToDisk {
			call.step(
				"lsp_sync_unavailable_after_persist",
				append(baseAttrs, "phase", "lsp_sync", "stage", "execute", "warning", err.Error())...,
			)
			call.done(append(baseAttrs, "result_empty", false, "lsp_sync_warning", err.Error())...)
			return "ok: file content updated and persisted to disk (lsp sync unavailable: " + err.Error() + ")", true
		}
		call.fail(err, append(baseAttrs, "phase", "lsp_sync", "stage", "execute")...)
		return toolError(err), true
	}
	call.step("lsp_sync_done", append(baseAttrs, "phase", "lsp_sync")...)
	return "", false
}

func didChangeSuccessText(persist bool) string {
	if persist {
		return "ok: file content updated and persisted to disk"
	}
	return "ok: file content updated (lsp-only, disk not written)"
}

func (h *ToolHandlers) ReplaceRange(args json.RawMessage) string {
	call, ok := h.startManagedToolCall("lsp_replace_range", args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	req, err := decodeReplaceRangeRequest(call, args)
	if err != nil {
		return toolError(err)
	}
	baseContent, contentSource, startOffset, endOffset, err := h.resolveReplaceRangeOffsets(call, req)
	if err != nil {
		return toolError(err)
	}
	nextContent := baseContent[:startOffset] + req.NewText + baseContent[endOffset:]
	didChangeArgs, err := buildReplaceRangeDidChangeArgs(args, req, nextContent)
	if err != nil {
		call.fail(err, "stage", "marshal_did_change")
		return toolError(err)
	}
	result := h.DidChange(didChangeArgs)
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(result)), "error:") {
		call.fail(
			errors.New(strings.TrimSpace(result)),
			logger.FieldPath, lspToolLogPath(req.FilePath),
			"line", req.Line,
			"column", req.Column,
			"end_line", req.EndLine,
			"end_column", req.EndColumn,
			"stage", "did_change",
		)
		return result
	}
	call.done(
		logger.FieldPath, lspToolLogPath(req.FilePath),
		"line", req.Line,
		"column", req.Column,
		"end_line", req.EndLine,
		"end_column", req.EndColumn,
		"replacement_len", len(req.NewText),
		"replaced_len", endOffset-startOffset,
		"content_source", contentSource,
		"persist_to_disk", req.PersistToDisk,
	)
	return result
}

type lspReplaceRangeRequest struct {
	FilePath      string
	Line          int
	Column        int
	EndLine       int
	EndColumn     int
	NewText       string
	Version       int
	PersistToDisk bool
}

func decodeReplaceRangeRequest(call *lspToolCallLogger, args json.RawMessage) (lspReplaceRangeRequest, error) {
	params, err := decodeToolParams[lspReplaceRangeParam](call, args)
	if err != nil {
		return lspReplaceRangeRequest{}, err
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return lspReplaceRangeRequest{}, err
	}
	line, column, endLine, endColumn, err := validateReplaceRangePosition(call, params)
	if err != nil {
		return lspReplaceRangeRequest{}, err
	}
	return lspReplaceRangeRequest{
		FilePath:      filePath,
		Line:          line,
		Column:        column,
		EndLine:       endLine,
		EndColumn:     endColumn,
		NewText:       params.NewText,
		Version:       params.Version,
		PersistToDisk: params.PersistToDisk,
	}, nil
}

func validateReplaceRangePosition(call *lspToolCallLogger, params lspReplaceRangeParam) (int, int, int, int, error) {
	values, err := readRequiredInts(call,
		namedIntParam{name: "line", value: params.Line},
		namedIntParam{name: "column", value: params.Column},
		namedIntParam{name: "end_line", value: params.EndLine},
		namedIntParam{name: "end_column", value: params.EndColumn},
	)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	line, column, endLine, endColumn := values[0], values[1], values[2], values[3]
	if err := requireNonNegativePosition(line, column); err != nil {
		call.fail(err, "stage", "validate")
		return 0, 0, 0, 0, err
	}
	if err := requireNonNegativePosition(endLine, endColumn); err != nil {
		call.fail(err, "stage", "validate")
		return 0, 0, 0, 0, err
	}
	if endLine < line || (endLine == line && endColumn < column) {
		err := fmt.Errorf("end position must be >= start position")
		call.fail(err, "stage", "validate")
		return 0, 0, 0, 0, err
	}
	return line, column, endLine, endColumn, nil
}

type namedIntParam struct {
	name  string
	value *int
}

func readRequiredInts(call *lspToolCallLogger, params ...namedIntParam) ([]int, error) {
	out := make([]int, 0, len(params))
	for _, param := range params {
		value, err := requireIntParam(param.name, param.value)
		if err != nil {
			call.fail(err, "stage", "validate")
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func (h *ToolHandlers) resolveReplaceRangeOffsets(
	call *lspToolCallLogger,
	req lspReplaceRangeRequest,
) (content, source string, startOffset, endOffset int, err error) {
	content, source, err = h.loadReplaceRangeBaseContent(req.FilePath)
	if err != nil {
		call.fail(err, logger.FieldPath, lspToolLogPath(req.FilePath), "stage", "load_content")
		return "", "", 0, 0, err
	}
	startOffset, err = lineColumnOffset(content, req.Line, req.Column)
	if err != nil {
		call.fail(
			err,
			logger.FieldPath, lspToolLogPath(req.FilePath),
			"line", req.Line,
			"column", req.Column,
			"stage", "resolve_start",
		)
		return "", "", 0, 0, err
	}
	endOffset, err = lineColumnOffset(content, req.EndLine, req.EndColumn)
	if err != nil {
		call.fail(
			err,
			logger.FieldPath, lspToolLogPath(req.FilePath),
			"end_line", req.EndLine,
			"end_column", req.EndColumn,
			"stage", "resolve_end",
		)
		return "", "", 0, 0, err
	}
	if endOffset < startOffset {
		err = fmt.Errorf("invalid range: end offset before start offset")
		call.fail(err, "stage", "validate")
		return "", "", 0, 0, err
	}
	return content, source, startOffset, endOffset, nil
}

func buildReplaceRangeDidChangeArgs(
	args json.RawMessage,
	req lspReplaceRangeRequest,
	newContent string,
) (json.RawMessage, error) {
	payload := map[string]any{
		"file_path":       req.FilePath,
		"new_content":     newContent,
		"version":         req.Version,
		"persist_to_disk": req.PersistToDisk,
	}
	var raw map[string]any
	if err := json.Unmarshal(args, &raw); err == nil && raw != nil {
		if meta, ok := raw[lspToolCallMetaKey]; ok {
			payload[lspToolCallMetaKey] = meta
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return data, nil
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

func (h *ToolHandlers) CodeAction(args json.RawMessage) string {
	call, ok := h.startManagedToolCall("lsp_code_action", args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	params, err := decodeToolParams[lspCodeActionParam](call, args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return toolError(err)
	}
	line, column, err := requireLineColumn(call, params.Line, params.Column)
	if err != nil {
		return toolError(err)
	}
	endLine, endColumn := optionalRangeEnd(params.EndLine, params.EndColumn)
	return runAndMarshalLogged(
		call,
		func() ([]CodeActionResult, error) {
			return h.manager.CodeAction(filePath, line, column, endLine, endColumn, params.Only)
		},
		nil,
		"no code action found",
		func(result []CodeActionResult) bool { return len(result) == 0 },
		func(result []CodeActionResult) []any { return []any{"result_count", len(result)} },
		append(
			lspToolPathPositionAttrs(filePath, line, column),
			"end_line", endLine,
			"end_column", endColumn,
			"only_count", len(params.Only),
		)...,
	)
}

func optionalRangeEnd(endLine, endColumn *int) (int, int) {
	line, column := -1, -1
	if endLine != nil {
		line = *endLine
	}
	if endColumn != nil {
		column = *endColumn
	}
	return line, column
}

func (h *ToolHandlers) SignatureHelp(args json.RawMessage) string {
	call, ok := h.startManagedToolCall("lsp_signature_help", args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	params, err := decodeToolParams[lspSignatureHelpParam](call, args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireToolFilePath(call, params.FilePath)
	if err != nil {
		return toolError(err)
	}
	line, column, err := requireLineColumn(call, params.Line, params.Column)
	if err != nil {
		return toolError(err)
	}
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
		lspToolPathPositionAttrs(filePath, line, column)...,
	)
}

func (h *ToolHandlers) Format(args json.RawMessage) string {
	call, filePath, _, errText := h.decodeManagedFilePath("lsp_format", args)
	if errText != "" {
		return errText
	}
	params, err := decodeToolParams[lspFormatParam](call, args)
	if err != nil {
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
	return runAndMarshalLogged(
		call,
		func() ([]TextEdit, error) { return h.manager.Format(filePath, tabSize, insertSpaces) },
		nil,
		"no formatting edits",
		func(result []TextEdit) bool { return len(result) == 0 },
		func(result []TextEdit) []any { return []any{"result_count", len(result)} },
		append(lspToolPathAttrs(filePath), "tab_size", tabSize, "insert_spaces", insertSpaces)...,
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
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		return text, text != ""
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		return strconv.FormatInt(int64(rv.Float()), 10), true
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return "", false
	}
	return text, true
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
	cmdArgs := buildTextSearchArgs(params, target, limit)
	return runExternalSearch(
		call,
		lspExternalSearch{
			Binary:       "rg",
			MissingError: "rg not found in PATH",
			Workspace:    workspaceRoot,
			Target:       target,
			Limit:        limit,
			CommandArgs:  cmdArgs,
			Parse:        parseRipgrepVimgrepOutput,
			LogAttrs:     []any{logger.FieldPath, target, "query_len", len(params.Query)},
		},
	)
}

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
	return runExternalSearch(
		call,
		lspExternalSearch{
			Binary:       "sg",
			MissingError: "sg not found in PATH",
			Workspace:    workspaceRoot,
			Target:       target,
			Limit:        limit,
			CommandArgs:  []string{"scan", "--json=stream", "-p", params.Symbol, "-l", params.Language, target},
			Parse:        parseASTGrepOutput,
			LogAttrs: []any{
				logger.FieldPath, target,
				logger.FieldLanguage, params.Language,
				"symbol_len", len(params.Symbol),
			},
		},
	)
}

type lspExternalSearch struct {
	Binary       string
	MissingError string
	Workspace    string
	Target       string
	Limit        int
	CommandArgs  []string
	Parse        func(output []byte, workspaceRoot string, limit int) []lspSearchMatch
	LogAttrs     []any
}

func buildTextSearchArgs(params lspTextSearchParam, target string, limit int) []string {
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
	return append(cmdArgs, params.Query, target)
}

func runExternalSearch(call *lspToolCallLogger, req lspExternalSearch) string {
	binaryPath, err := lspSearchLookPath(req.Binary)
	if err != nil {
		call.fail(errors.New(req.MissingError), append(req.LogAttrs, "stage", "dependency")...)
		return toolError(errors.New(req.MissingError))
	}
	output, err := runSearchCommand(binaryPath, req.CommandArgs, req.Workspace)
	if err != nil {
		call.fail(err, append(req.LogAttrs, "stage", "execute")...)
		return toolError(err)
	}
	matches := filterAndCapSearchMatches(req.Parse(output, req.Workspace, req.Limit))
	if len(matches) == 0 {
		call.done(append(req.LogAttrs, "result_count", 0, "result_empty", true)...)
		return "no matches found"
	}
	data, err := json.Marshal(matches)
	if err != nil {
		call.fail(err, "stage", "marshal")
		return toolError(err)
	}
	call.done(append(req.LogAttrs, "result_count", len(matches), "result_empty", false)...)
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
	return parseSearchOutputLines(output, limit, func(raw string) (lspSearchMatch, bool) {
		return parseRipgrepVimgrepLine(raw, workspaceRoot)
	})
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
	return parseSearchOutputLines(output, limit, func(raw string) (lspSearchMatch, bool) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return lspSearchMatch{}, false
		}
		match, ok := parseASTGrepLine(trimmed, workspaceRoot)
		if !ok {
			match = lspSearchMatch{Path: ".", Text: truncateSearchText(trimmed)}
		}
		if isExcludedPath(match.Path) {
			return lspSearchMatch{}, false
		}
		return match, true
	})
}

func parseSearchOutputLines(
	output []byte,
	limit int,
	parse func(string) (lspSearchMatch, bool),
) []lspSearchMatch {
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
		match, ok := parse(raw)
		if !ok {
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
	if value == nil {
		return 0
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return int(rv.Float())
	}
	return 0
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
	call, ok := h.startManagedToolCall("lsp_workspace_symbol", args)
	if !ok {
		return "error: lsp manager unavailable"
	}
	req, err := decodeWorkspaceSymbolRequest(call, args)
	if err != nil {
		return toolError(err)
	}
	if err := validateWorkspaceSymbolRequest(call, req); err != nil {
		return toolError(err)
	}
	return runAndMarshalLogged(
		call,
		func() ([]WorkspaceSymbolResult, error) {
			result, err := h.manager.WorkspaceSymbol(req.Path, req.Language, req.Query)
			if err != nil {
				return nil, err
			}
			return limitWorkspaceSymbolResults(result), nil
		},
		nil,
		"no symbols found",
		func(result []WorkspaceSymbolResult) bool { return len(result) == 0 },
		func(result []WorkspaceSymbolResult) []any { return []any{"result_count", len(result)} },
		logger.FieldPath, req.ResolvedPath,
		logger.FieldLanguage, req.Language,
		"query_len", len(req.Query),
	)
}

type workspaceSymbolRequest struct {
	Path         string
	Language     string
	Query        string
	ResolvedPath string
}

func decodeWorkspaceSymbolRequest(call *lspToolCallLogger, args json.RawMessage) (workspaceSymbolRequest, error) {
	params, err := decodeToolParams[lspWorkspaceSymbolParam](call, args)
	if err != nil {
		return workspaceSymbolRequest{}, err
	}
	params.Query = strings.TrimSpace(params.Query)
	params.Path = strings.TrimSpace(params.Path)
	params.Language = strings.TrimSpace(params.Language)
	return workspaceSymbolRequest{
		Path:         params.Path,
		Language:     params.Language,
		Query:        params.Query,
		ResolvedPath: lspToolLogPath(params.Path),
	}, nil
}

func validateWorkspaceSymbolRequest(call *lspToolCallLogger, req workspaceSymbolRequest) error {
	if req.Query == "" {
		err := errors.New("query is required")
		call.fail(
			err,
			logger.FieldPath, req.ResolvedPath,
			logger.FieldLanguage, req.Language,
			"query_len", 0,
			"stage", "validate",
		)
		return err
	}
	if req.Path == "" && req.Language == "" {
		err := errors.New("exactly one of path or language is required")
		call.fail(err, "query_len", len(req.Query), "stage", "validate")
		return err
	}
	if req.Path != "" && req.Language != "" {
		err := errors.New("exactly one of path or language is required")
		call.fail(
			err,
			logger.FieldPath, req.ResolvedPath,
			logger.FieldLanguage, req.Language,
			"query_len", len(req.Query),
			"stage", "validate",
		)
		return err
	}
	if req.Path == "" {
		return nil
	}
	if info, statErr := os.Stat(req.Path); statErr == nil && info.IsDir() {
		err := errors.New("directory path is not supported for workspace_symbol; use language instead")
		call.fail(err, logger.FieldPath, req.ResolvedPath, "query_len", len(req.Query), "stage", "validate")
		return err
	}
	return nil
}

// Implementation finds symbol implementation locations.
func (h *ToolHandlers) Implementation(args json.RawMessage) string {
	return runFilePositionManagerTool(
		h,
		"lsp_implementation",
		args,
		func(filePath string, line, column int) ([]LocationResult, error) {
			result, err := h.manager.Implementation(filePath, line, column)
			if err != nil {
				return nil, err
			}
			return limitLocationResults(result), nil
		},
		func(filePath string, line, column int, err error) string {
			return h.contextualToolError("lsp_implementation", filePath, line, column, err)
		},
		"no implementation found",
		func(result []LocationResult) bool { return len(result) == 0 },
		func(result []LocationResult) []any { return []any{"result_count", len(result)} },
		nil,
	)
}

// TypeDefinition finds symbol type definition locations.
func (h *ToolHandlers) TypeDefinition(args json.RawMessage) string {
	return runFilePositionManagerTool(
		h,
		"lsp_type_definition",
		args,
		func(filePath string, line, column int) ([]LocationResult, error) {
			result, err := h.manager.TypeDefinition(filePath, line, column)
			if err != nil {
				return nil, err
			}
			return limitLocationResults(result), nil
		},
		func(filePath string, line, column int, err error) string {
			return h.contextualToolError("lsp_type_definition", filePath, line, column, err)
		},
		"no type definition found",
		func(result []LocationResult) bool { return len(result) == 0 },
		func(result []LocationResult) []any { return []any{"result_count", len(result)} },
		nil,
	)
}

func limitWorkspaceSymbolResults(in []WorkspaceSymbolResult) []WorkspaceSymbolResult {
	return limitResults(in, XRefResultLimit)
}

func limitLocationResults(in []LocationResult) []LocationResult {
	return limitResults(in, XRefResultLimit)
}

func limitResults[T any](in []T, limit int) []T {
	if len(in) <= limit {
		return in
	}
	out := make([]T, limit)
	copy(out, in[:limit])
	return out
}

type lspMergedActionParam struct {
	Action string `json:"action"`
}

type mergedActionHandler func(*ToolHandlers, json.RawMessage) string

var lspFileActionHandlers = map[string]mergedActionHandler{
	"open_file":   (*ToolHandlers).OpenFile,
	"read_file":   (*ToolHandlers).ReadFile,
	"did_change":  (*ToolHandlers).DidChange,
	"diagnostics": (*ToolHandlers).Diagnostics,
}

var lspInspectActionHandlers = map[string]mergedActionHandler{
	"hover":           (*ToolHandlers).Hover,
	"definition":      (*ToolHandlers).Definition,
	"implementation":  (*ToolHandlers).Implementation,
	"type_definition": (*ToolHandlers).TypeDefinition,
	"signature_help":  (*ToolHandlers).SignatureHelp,
}

var lspXRefActionHandlers = map[string]mergedActionHandler{
	"call_hierarchy": (*ToolHandlers).CallHierarchy,
	"type_hierarchy": (*ToolHandlers).TypeHierarchy,
	"references":     (*ToolHandlers).References,
}

var lspGrepActionHandlers = map[string]mergedActionHandler{
	"text_search": (*ToolHandlers).TextSearch,
	"ast_search":  (*ToolHandlers).AstSearch,
}

var lspStructureActionHandlers = map[string]mergedActionHandler{
	"document_symbol":  (*ToolHandlers).DocumentSymbol,
	"workspace_symbol": (*ToolHandlers).WorkspaceSymbol,
	"semantic_tokens":  (*ToolHandlers).SemanticTokens,
	"folding_range":    (*ToolHandlers).FoldingRange,
}

var lspEditActionHandlers = map[string]mergedActionHandler{
	"rename":        (*ToolHandlers).Rename,
	"code_action":   (*ToolHandlers).CodeAction,
	"format":        (*ToolHandlers).Format,
	"replace_range": (*ToolHandlers).ReplaceRange,
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

func (h *ToolHandlers) dispatchMergedAction(args json.RawMessage, scope string, handlers map[string]mergedActionHandler) string {
	action, err := decodeMergedAction(args)
	if err != nil {
		return toolError(err)
	}
	handler, ok := handlers[action]
	if !ok {
		return toolError(fmt.Errorf("unsupported %s action: %s", scope, action))
	}
	return handler(h, args)
}

// LSPFile routes file operations.
func (h *ToolHandlers) LSPFile(args json.RawMessage) string {
	return h.dispatchMergedAction(args, "lsp_file", lspFileActionHandlers)
}

// LSPInspect routes inspect operations.
func (h *ToolHandlers) LSPInspect(args json.RawMessage) string {
	return h.dispatchMergedAction(args, "lsp_inspect", lspInspectActionHandlers)
}

// LSPXRef routes cross-reference operations.
func (h *ToolHandlers) LSPXRef(args json.RawMessage) string {
	return h.dispatchMergedAction(args, "lsp_xref", lspXRefActionHandlers)
}

// LSPGrep routes text/ast search operations.
func (h *ToolHandlers) LSPGrep(args json.RawMessage) string {
	return h.dispatchMergedAction(args, "lsp_grep", lspGrepActionHandlers)
}

// LSPStructure routes structure/hierarchy operations.
func (h *ToolHandlers) LSPStructure(args json.RawMessage) string {
	return h.dispatchMergedAction(args, "lsp_structure", lspStructureActionHandlers)
}

// LSPEdit routes edit operations.
func (h *ToolHandlers) LSPEdit(args json.RawMessage) string {
	return h.dispatchMergedAction(args, "lsp_edit", lspEditActionHandlers)
}
