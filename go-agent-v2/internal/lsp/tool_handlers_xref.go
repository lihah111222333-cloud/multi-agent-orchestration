package lsp

import (
	"encoding/json"
	"strings"
)

type lspWorkspaceSymbolParam struct {
	FilePath string `json:"file_path"`
	Language string `json:"language"`
	Query    string `json:"query"`
}

// WorkspaceSymbol searches workspace symbols.
func (h *ToolHandlers) WorkspaceSymbol(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspWorkspaceSymbolParam](args)
	if err != nil {
		return toolError(err)
	}

	params.Query = strings.TrimSpace(params.Query)
	params.FilePath = strings.TrimSpace(params.FilePath)
	params.Language = strings.TrimSpace(params.Language)

	if params.Query == "" {
		return "error: query is required"
	}
	if params.FilePath == "" && params.Language == "" {
		return "error: exactly one of file_path or language is required"
	}
	if params.FilePath != "" && params.Language != "" {
		return "error: file_path and language are mutually exclusive"
	}

	return runAndMarshal(
		func() ([]WorkspaceSymbolResult, error) {
			result, err := h.manager.WorkspaceSymbol(params.FilePath, params.Language, params.Query)
			if err != nil {
				return nil, err
			}
			return limitWorkspaceSymbolResults(result), nil
		},
		"no symbols found",
		func(result []WorkspaceSymbolResult) bool { return len(result) == 0 },
	)
}

// Implementation finds symbol implementation locations.
func (h *ToolHandlers) Implementation(args json.RawMessage) string {
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
	return runAndMarshalWithError(
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
	)
}

// TypeDefinition finds symbol type definition locations.
func (h *ToolHandlers) TypeDefinition(args json.RawMessage) string {
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
	return runAndMarshalWithError(
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
