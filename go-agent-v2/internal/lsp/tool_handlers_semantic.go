package lsp

import "encoding/json"

// SemanticTokens gets document semantic tokens.
func (h *ToolHandlers) SemanticTokens(args json.RawMessage) string {
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
		func() (*SemanticTokensResult, error) {
			return h.manager.SemanticTokens(filePath)
		},
		"no semantic tokens found",
		func(result *SemanticTokensResult) bool {
			return result == nil || (len(result.Data) == 0 && len(result.Decoded) == 0)
		},
	)
}

// FoldingRange gets document folding ranges.
func (h *ToolHandlers) FoldingRange(args json.RawMessage) string {
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
		func() ([]FoldingRange, error) {
			return h.manager.FoldingRange(filePath)
		},
		"no folding range found",
		func(result []FoldingRange) bool { return len(result) == 0 },
	)
}
