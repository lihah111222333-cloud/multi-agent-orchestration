package lsp

import (
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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
