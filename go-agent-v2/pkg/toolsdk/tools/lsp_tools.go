package tools

import (
	"encoding/json"

	agentcore "github.com/multi-agent/go-agent-v2/internal/agentcore"
)

type LSPDynamicToolHandler = func(json.RawMessage) string

type LSPHandlerProvider interface {
	AvailabilitySummary() map[string]any
	DiagnosticsQuery(filePath string) map[string]any
	LSPFile(json.RawMessage) string
	LSPInspect(json.RawMessage) string
	LSPXRef(json.RawMessage) string
	LSPGrep(json.RawMessage) string
	LSPStructure(json.RawMessage) string
	LSPEdit(json.RawMessage) string
	Completion(json.RawMessage) string
}

type LSPProvider interface {
	LSPHandlerProvider
	BindDynamicTool(name string, handler LSPDynamicToolHandler)
}

type lspBaseToolSpec struct {
	schema  agentcore.DynamicTool
	handler func(LSPHandlerProvider) LSPDynamicToolHandler
}

func baseLSPToolSpecs() []lspBaseToolSpec {
	return []lspBaseToolSpec{
		lspBaseSpec(
			"lsp_file",
			"File-level LSP operations: open/sync document state and query diagnostics.",
			lspSchema(map[string]any{
				"action":          lspEnumStringProperty("File operation to execute", []string{"open_file", "did_change", "diagnostics"}),
				"file_path":       lspStringProperty(defaultFilePathDescription),
				"line":            lspNumberProperty(defaultLineDescription),
				"column":          lspNumberProperty(defaultColumnDescription),
				"new_content":     lspStringProperty("Full new document content for did_change"),
				"persist_to_disk": lspBooleanProperty("Persist did_change content to disk"),
				"version":         lspNumberProperty("Document version"),
			}, lspRequired("action"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPFile },
		),
		lspBaseSpec(
			"lsp_inspect",
			"LSP inspection operations for symbol/type/location introspection.",
			lspSchema(map[string]any{
				"action":              lspEnumStringProperty("Inspect operation", []string{"hover", "definition", "references", "implementation", "type_definition", "signature_help", "diagnostics"}),
				"file_path":           lspStringProperty(defaultFilePathDescription),
				"line":                lspNumberProperty(defaultLineDescription),
				"column":              lspNumberProperty(defaultColumnDescription),
				"include_declaration": lspBooleanProperty("Include declaration for references"),
			}, lspRequired("action", "file_path", "line", "column"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPInspect },
		),
		lspBaseSpec(
			"lsp_xref",
			"Cross-reference operations across call/type hierarchies.",
			lspSchema(map[string]any{
				"action":    lspEnumStringProperty("Cross-reference operation", []string{"call_hierarchy", "type_hierarchy", "references"}),
				"file_path": lspStringProperty(defaultFilePathDescription),
				"line":      lspNumberProperty(defaultLineDescription),
				"column":    lspNumberProperty(defaultColumnDescription),
				"direction": lspEnumStringProperty("Hierarchy direction", []string{"incoming", "outgoing", "both", "supertypes", "subtypes"}),
			}, lspRequired("action", "file_path", "line", "column"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPXRef },
		),
		lspBaseSpec(
			"lsp_grep",
			"Search source text and symbols (text search / AST search).",
			lspSchema(map[string]any{
				"action":         lspEnumStringProperty("Search operation", []string{"text_search", "ast_search"}),
				"query":          lspStringProperty("Search query"),
				"path":           lspStringProperty("Search root path"),
				"glob":           lspStringProperty("Glob filter for file paths"),
				"case_sensitive": lspBooleanProperty("Case sensitive match"),
				"max_results":    lspNumberProperty("Maximum number of matches"),
				"language":       lspStringProperty("Language selector for AST search"),
				"symbol":         lspStringProperty("Symbol query for AST search"),
			}, lspRequired("action"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPGrep },
		),
		lspBaseSpec(
			"lsp_structure",
			"Structural LSP views: document/workspace symbols, folding and semantic tokens.",
			lspSchema(map[string]any{
				"action":    lspEnumStringProperty("Structure operation", []string{"document_symbol", "workspace_symbol", "folding_range", "semantic_tokens"}),
				"file_path": lspStringProperty(defaultFilePathDescription),
				"query":     lspStringProperty("Workspace symbol query"),
				"language":  lspStringProperty("Language selector for workspace symbol"),
			}, lspRequired("action"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPStructure },
		),
		lspBaseSpec(
			"lsp_edit",
			"Editing operations: rename/code_action/format with optional disk persistence.",
			lspSchema(map[string]any{
				"action":          lspEnumStringProperty("Edit operation", []string{"rename", "code_action", "format", "did_change"}),
				"file_path":       lspStringProperty(defaultFilePathDescription),
				"line":            lspNumberProperty(defaultLineDescription),
				"column":          lspNumberProperty(defaultColumnDescription),
				"end_line":        lspNumberProperty("0-indexed end line"),
				"end_column":      lspNumberProperty("0-indexed end column"),
				"new_name":        lspStringProperty("New symbol name for rename"),
				"new_content":     lspStringProperty("Full new document content for did_change"),
				"persist_to_disk": lspBooleanProperty("Persist edits to disk when supported"),
				"version":         lspNumberProperty("Document version"),
				"insert_spaces":   lspBooleanProperty("Use spaces for formatting"),
				"tab_size":        lspNumberProperty("Tab size for formatting"),
				"only":            lspStringArrayProperty("Code action kinds filter"),
			}, lspRequired("action"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPEdit },
		),
		lspBaseSpec(
			"lsp_completion",
			"Code completion suggestions at a cursor position.",
			lspFileLineColumnSchema(defaultFilePathDescription, defaultLineDescription, defaultColumnDescription, nil, lspRequired("file_path", "line", "column"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.Completion },
		),
	}
}

func RegisterLSPHandlers(dst map[string]LSPDynamicToolHandler, provider LSPHandlerProvider) {
	if dst == nil || provider == nil {
		return
	}
	for _, spec := range baseLSPToolSpecs() {
		if spec.handler == nil {
			continue
		}
		dst[spec.schema.Name] = spec.handler(provider)
	}
}

func LSPTools() []agentcore.DynamicTool {
	specs := baseLSPToolSpecs()
	tools := make([]agentcore.DynamicTool, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, spec.schema)
	}
	return tools
}

func LSPAddonTools() []agentcore.DynamicTool {
	return nil
}
