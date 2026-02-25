package lsp

import "encoding/json"

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
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspCodeActionParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	line, err := requireIntParam("line", params.Line)
	if err != nil {
		return toolError(err)
	}
	column, err := requireIntParam("column", params.Column)
	if err != nil {
		return toolError(err)
	}
	if err := requireNonNegativePosition(line, column); err != nil {
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

	return runAndMarshal(
		func() ([]CodeActionResult, error) {
			return h.manager.CodeAction(filePath, line, column, endLine, endColumn, params.Only)
		},
		"no code action found",
		func(result []CodeActionResult) bool { return len(result) == 0 },
	)
}

// SignatureHelp gets signature help at a position.
func (h *ToolHandlers) SignatureHelp(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspSignatureHelpParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	line, err := requireIntParam("line", params.Line)
	if err != nil {
		return toolError(err)
	}
	column, err := requireIntParam("column", params.Column)
	if err != nil {
		return toolError(err)
	}
	if err := requireNonNegativePosition(line, column); err != nil {
		return toolError(err)
	}

	return runAndMarshal(
		func() (*SignatureHelpResult, error) {
			return h.manager.SignatureHelp(filePath, line, column)
		},
		"no signature help found",
		func(result *SignatureHelpResult) bool { return result == nil || len(result.Signatures) == 0 },
	)
}

// Format returns formatting edits.
func (h *ToolHandlers) Format(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspFormatParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
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
	return runAndMarshal(
		func() ([]TextEdit, error) { return h.manager.Format(filePath, tabSize, insertSpaces) },
		"no formatting edits",
		func(result []TextEdit) bool { return len(result) == 0 },
	)
}
