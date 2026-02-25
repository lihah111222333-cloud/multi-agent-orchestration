package lsp

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type lspWorkspaceSymbolParam struct {
	FilePath string `json:"file_path"`
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
	params.FilePath = strings.TrimSpace(params.FilePath)
	params.Language = strings.TrimSpace(params.Language)
	resolvedPath := lspToolLogPath(params.FilePath)

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
	if params.FilePath == "" && params.Language == "" {
		call.fail(
			errors.New("exactly one of file_path or language is required"),
			"query_len", len(params.Query),
			"stage", "validate",
		)
		return "error: exactly one of file_path or language is required"
	}
	if params.FilePath != "" && params.Language != "" {
		call.fail(
			errors.New("file_path and language are mutually exclusive"),
			logger.FieldPath, resolvedPath,
			logger.FieldLanguage, params.Language,
			"query_len", len(params.Query),
			"stage", "validate",
		)
		return "error: file_path and language are mutually exclusive"
	}

	return runAndMarshalLogged(
		call,
		func() ([]WorkspaceSymbolResult, error) {
			result, err := h.manager.WorkspaceSymbol(params.FilePath, params.Language, params.Query)
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
