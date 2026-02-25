package lsp

import (
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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
