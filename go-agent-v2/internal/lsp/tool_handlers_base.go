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
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePositionParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	result, err := h.manager.Hover(filePath, params.Line, params.Column)
	if err != nil {
		return toolError(err)
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
	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "error: reading file: " + err.Error()
	}
	if err := h.manager.OpenFile(filePath, string(content)); err != nil {
		return toolError(err)
	}
	return fmt.Sprintf("opened %s (%d bytes)", filePath, len(content))
}

// Diagnostics returns current diagnostics from cache.
func (h *ToolHandlers) Diagnostics(args json.RawMessage) string {
	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		return "error: unmarshal diagnostics params: " + err.Error()
	}
	params.FilePath = strings.TrimSpace(params.FilePath)

	if params.FilePath != "" && !h.managerUnavailable() {
		if err := h.manager.BootstrapDocument(params.FilePath); err != nil {
			return toolError(err)
		}
		// Diagnostics arrive asynchronously via notifications; wait once to improve first lookup hit rate.
		time.Sleep(120 * time.Millisecond)
	}

	accessor := h.diagnosticsAccessor()
	if accessor == nil {
		return "no diagnostics"
	}

	if params.FilePath != "" {
		uri := normalizeDiagnosticsURI(params.FilePath)
		diags := accessor.GetDiagnostics(uri)
		if len(diags) == 0 {
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
		return sb.String()
	}

	all := accessor.GetAllDiagnostics()
	if len(all) == 0 {
		return "no diagnostics"
	}
	var sb strings.Builder
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
	params, err := decodeArgs[lspFilePositionParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	return runAndMarshal(
		func() ([]Location, error) { return h.manager.Definition(filePath, params.Line, params.Column) },
		"no definition found",
		func(result []Location) bool { return len(result) == 0 },
	)
}

// References finds symbol references.
func (h *ToolHandlers) References(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspReferencesParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	includeDecl := true
	if params.IncludeDecl != nil {
		includeDecl = *params.IncludeDecl
	}
	return runAndMarshal(
		func() ([]Location, error) {
			return h.manager.References(filePath, params.Line, params.Column, includeDecl)
		},
		"no references found",
		func(result []Location) bool { return len(result) == 0 },
	)
}

// DocumentSymbol returns file symbols.
func (h *ToolHandlers) DocumentSymbol(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePathParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	return runAndMarshal(
		func() ([]DocumentSymbol, error) { return h.manager.DocumentSymbol(filePath) },
		"no symbols found",
		func(result []DocumentSymbol) bool { return len(result) == 0 },
	)
}

// Rename renames symbol.
func (h *ToolHandlers) Rename(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspRenameParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	if strings.TrimSpace(params.NewName) == "" {
		return "error: new_name is required"
	}
	return runAndMarshal(
		func() (*WorkspaceEdit, error) {
			return h.manager.Rename(filePath, params.Line, params.Column, params.NewName)
		},
		"no edits produced",
		func(result *WorkspaceEdit) bool {
			return result == nil || (len(result.Changes) == 0 && len(result.DocumentChanges) == 0)
		},
	)
}

// Completion returns code completion items.
func (h *ToolHandlers) Completion(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFilePositionParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	return runAndMarshal(
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
		"no completions",
		func(result []CompletionItem) bool { return len(result) == 0 },
	)
}

// DidChange notifies full content changes.
func (h *ToolHandlers) DidChange(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspDidChangeParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	if params.Version == 0 {
		params.Version = 2
	}
	resolvedPath := filePath
	if absPath, absErr := filepath.Abs(filePath); absErr == nil {
		resolvedPath = absPath
	}

	if params.PersistToDisk {
		logger.Info("lsp_did_change: persist begin",
			logger.FieldToolName, "lsp_did_change",
			logger.FieldPath, resolvedPath,
			logger.FieldVersion, params.Version,
			logger.FieldBytes, len(params.NewContent),
			"persist_to_disk", true,
			"phase", "persist",
		)
		if err := writeTextFileAtomic(filePath, params.NewContent); err != nil {
			logger.Warn("lsp_did_change: persist failed",
				logger.FieldToolName, "lsp_did_change",
				logger.FieldPath, resolvedPath,
				logger.FieldVersion, params.Version,
				logger.FieldBytes, len(params.NewContent),
				"persist_to_disk", true,
				"phase", "persist",
				logger.FieldError, err,
			)
			return "error: writing file: " + err.Error()
		}
		logger.Info("lsp_did_change: persist done",
			logger.FieldToolName, "lsp_did_change",
			logger.FieldPath, resolvedPath,
			logger.FieldVersion, params.Version,
			logger.FieldBytes, len(params.NewContent),
			"persist_to_disk", true,
			"phase", "persist",
		)
	}

	if err := h.manager.ChangeFile(filePath, params.Version, params.NewContent); err != nil {
		logger.Warn("lsp_did_change: lsp sync failed",
			logger.FieldToolName, "lsp_did_change",
			logger.FieldPath, resolvedPath,
			logger.FieldVersion, params.Version,
			logger.FieldBytes, len(params.NewContent),
			"persist_to_disk", params.PersistToDisk,
			"phase", "lsp_sync",
			logger.FieldError, err,
		)
		return toolError(err)
	}

	logger.Info("lsp_did_change: lsp sync done",
		logger.FieldToolName, "lsp_did_change",
		logger.FieldPath, resolvedPath,
		logger.FieldVersion, params.Version,
		logger.FieldBytes, len(params.NewContent),
		"persist_to_disk", params.PersistToDisk,
		"phase", "lsp_sync",
	)

	if params.PersistToDisk {
		return "ok: file content updated and persisted to disk"
	}
	return "ok: file content updated (lsp-only, disk not written)"
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
