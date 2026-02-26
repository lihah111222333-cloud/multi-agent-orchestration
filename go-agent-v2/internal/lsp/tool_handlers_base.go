package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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
