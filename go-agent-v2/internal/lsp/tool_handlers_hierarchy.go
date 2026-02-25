package lsp

import "encoding/json"

type lspHierarchyParam struct {
	FilePath  string `json:"file_path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Direction string `json:"direction"`
}

// CallHierarchy gets call hierarchy entries.
func (h *ToolHandlers) CallHierarchy(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspHierarchyParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	return runAndMarshal(
		func() ([]CallHierarchyResult, error) {
			return h.manager.CallHierarchy(filePath, params.Line, params.Column, params.Direction)
		},
		"no call hierarchy found",
		func(result []CallHierarchyResult) bool { return len(result) == 0 },
	)
}

// TypeHierarchy gets type hierarchy entries.
func (h *ToolHandlers) TypeHierarchy(args json.RawMessage) string {
	if h.managerUnavailable() {
		return "error: lsp manager unavailable"
	}
	params, err := decodeArgs[lspHierarchyParam](args)
	if err != nil {
		return toolError(err)
	}
	filePath, err := requireFilePath(params.FilePath)
	if err != nil {
		return toolError(err)
	}
	return runAndMarshalWithError(
		func() ([]TypeHierarchyResult, error) {
			return h.manager.TypeHierarchy(filePath, params.Line, params.Column, params.Direction)
		},
		func(err error) string {
			return h.contextualToolError("lsp_type_hierarchy", filePath, params.Line, params.Column, err)
		},
		"no type hierarchy found",
		func(result []TypeHierarchyResult) bool { return len(result) == 0 },
	)
}
