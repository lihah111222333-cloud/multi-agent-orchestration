package lsp

import (
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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
