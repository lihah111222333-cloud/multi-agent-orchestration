package tools

import (
	"encoding/json"

	agentcore "github.com/multi-agent/go-agent-v2/internal/agentcore"
)

// LSPDynamicToolHandler is the runtime callback for one LSP dynamic tool.
type LSPDynamicToolHandler = func(json.RawMessage) string

// LSPHandlerProvider defines the merged P2 LSP dynamic-tool handler surface.
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

// LSPProvider extends handler capabilities with dynamic tool binding.
type LSPProvider interface {
	LSPHandlerProvider
	BindDynamicTool(name string, handler LSPDynamicToolHandler)
}

// LSPExtRegistryProvider defines one ext registry entry.
type LSPExtRegistryProvider struct {
	Name     string
	Register func(LSPProvider)
	Build    func() []agentcore.DynamicTool
}

type lspBaseToolSpec struct {
	schema  agentcore.DynamicTool
	handler func(LSPHandlerProvider) LSPDynamicToolHandler
}

func baseLSPToolSpecs() []lspBaseToolSpec {
	fileSchema := lspSchema(
		map[string]any{
			"action":          lspEnumStringProperty("File action.", []string{"open", "change"}),
			"file_path":       lspStringProperty(defaultFilePathDescription),
			"line":            lspNumberProperty(defaultLineDescription),
			"column":          lspNumberProperty(defaultColumnDescription),
			"new_content":     lspStringProperty("Full new content for action=change."),
			"version":         lspNumberProperty("Optional document version."),
			"persist_to_disk": lspBooleanProperty("Persist to disk when action=change."),
		},
		lspRequired("action", "file_path"),
		map[string]any{
			"oneOf": []map[string]any{
				{
					"properties": map[string]any{"action": map[string]any{"const": "open"}},
					"required":   []string{"action", "file_path"},
				},
				{
					"properties": map[string]any{"action": map[string]any{"const": "change"}},
					"required":   []string{"action", "file_path", "new_content"},
				},
			},
		},
	)

	inspectSchema := lspSchema(
		map[string]any{
			"action":    lspEnumStringProperty("Inspect action.", []string{"hover", "diagnostics", "signature_help"}),
			"file_path": lspStringProperty(defaultFilePathDescription),
			"line":      lspNumberProperty(defaultLineDescription),
			"column":    lspNumberProperty(defaultColumnDescription),
		},
		lspRequired("action", "file_path"),
		map[string]any{
			"oneOf": []map[string]any{
				{
					"properties": map[string]any{"action": map[string]any{"const": "diagnostics"}},
					"required":   []string{"action", "file_path"},
				},
				{
					"properties": map[string]any{"action": map[string]any{"enum": []string{"hover", "signature_help"}}},
					"required":   []string{"action", "file_path", "line", "column"},
				},
			},
		},
	)

	xrefSchema := lspSchema(
		map[string]any{
			"action":              lspEnumStringProperty("XRef action.", []string{"definition", "references", "implementation", "type_definition", "workspace_symbol"}),
			"file_path":           lspStringProperty(defaultFilePathDescription),
			"line":                lspNumberProperty(defaultLineDescription),
			"column":              lspNumberProperty(defaultColumnDescription),
			"include_declaration": lspBooleanProperty("Include declaration for references action."),
			"query":               lspStringProperty("Workspace symbol query for action=workspace_symbol."),
		},
		lspRequired("action"),
		map[string]any{
			"oneOf": []map[string]any{
				{
					"properties": map[string]any{"action": map[string]any{"const": "workspace_symbol"}},
					"required":   []string{"action", "query"},
				},
				{
					"properties": map[string]any{"action": map[string]any{"enum": []string{"definition", "references", "implementation", "type_definition"}}},
					"required":   []string{"action", "file_path", "line", "column"},
				},
			},
		},
	)

	grepSchema := lspSchema(
		map[string]any{
			"action": lspEnumStringProperty("Grep action.", []string{"text_search", "ast_search"}),
			"query":  lspStringProperty("Search query or AST pattern."),
			"path":   lspStringProperty("Optional path constraint."),
			"limit":  lspNumberProperty("Optional result limit."),
			"lang":   lspStringProperty("Optional language hint for ast_search."),
		},
		lspRequired("action", "query"),
		nil,
	)

	structureSchema := lspSchema(
		map[string]any{
			"action":    lspEnumStringProperty("Structure action.", []string{"document_symbol", "call_hierarchy", "type_hierarchy", "semantic_tokens", "folding_range"}),
			"file_path": lspStringProperty(defaultFilePathDescription),
			"line":      lspNumberProperty(defaultLineDescription),
			"column":    lspNumberProperty(defaultColumnDescription),
			"direction": lspEnumStringProperty("Hierarchy direction.", []string{"incoming", "outgoing", "both", "supertypes", "subtypes"}),
		},
		lspRequired("action", "file_path"),
		map[string]any{
			"oneOf": []map[string]any{
				{
					"properties": map[string]any{"action": map[string]any{"enum": []string{"document_symbol", "semantic_tokens", "folding_range"}}},
					"required":   []string{"action", "file_path"},
				},
				{
					"properties": map[string]any{"action": map[string]any{"enum": []string{"call_hierarchy", "type_hierarchy"}}},
					"required":   []string{"action", "file_path", "line", "column"},
				},
			},
		},
	)

	editSchema := lspSchema(
		map[string]any{
			"action":        lspEnumStringProperty("Edit action.", []string{"rename", "code_action", "format"}),
			"file_path":     lspStringProperty(defaultFilePathDescription),
			"line":          lspNumberProperty(defaultLineDescription),
			"column":        lspNumberProperty(defaultColumnDescription),
			"end_line":      lspNumberProperty("Optional end line for code_action."),
			"end_column":    lspNumberProperty("Optional end column for code_action."),
			"only":          lspStringArrayProperty("Optional code action kinds filter."),
			"new_name":      lspStringProperty("New symbol name for rename."),
			"insert_spaces": lspBooleanProperty("Use spaces for format."),
			"tab_size":      lspNumberProperty("Tab size for format."),
		},
		lspRequired("action", "file_path"),
		map[string]any{
			"oneOf": []map[string]any{
				{
					"properties": map[string]any{"action": map[string]any{"const": "format"}},
					"required":   []string{"action", "file_path"},
				},
				{
					"properties": map[string]any{"action": map[string]any{"const": "code_action"}},
					"required":   []string{"action", "file_path", "line", "column"},
				},
				{
					"properties": map[string]any{"action": map[string]any{"const": "rename"}},
					"required":   []string{"action", "file_path", "line", "column", "new_name"},
				},
			},
		},
	)

	completionSchema := lspFileLineColumnSchema(
		defaultFilePathDescription,
		defaultLineDescription,
		defaultColumnDescription,
		nil,
		lspRequired("file_path", "line", "column"),
		nil,
	)

	return []lspBaseToolSpec{
		lspBaseSpec("lsp_file", "LSP file operations (open/change).", fileSchema, func(p LSPHandlerProvider) LSPDynamicToolHandler { return p.LSPFile }),
		lspBaseSpec("lsp_inspect", "LSP inspect operations (hover/diagnostics/signature_help).", inspectSchema, func(p LSPHandlerProvider) LSPDynamicToolHandler { return p.LSPInspect }),
		lspBaseSpec("lsp_xref", "LSP cross-reference operations.", xrefSchema, func(p LSPHandlerProvider) LSPDynamicToolHandler { return p.LSPXRef }),
		lspBaseSpec("lsp_grep", "Workspace grep operations (text_search/ast_search).", grepSchema, func(p LSPHandlerProvider) LSPDynamicToolHandler { return p.LSPGrep }),
		lspBaseSpec("lsp_structure", "LSP structure operations (symbols/hierarchies/semantic/folding).", structureSchema, func(p LSPHandlerProvider) LSPDynamicToolHandler { return p.LSPStructure }),
		lspBaseSpec("lsp_edit", "LSP edit operations (rename/code_action/format).", editSchema, func(p LSPHandlerProvider) LSPDynamicToolHandler { return p.LSPEdit }),
		lspBaseSpec("lsp_completion", "LSP completion suggestions.", completionSchema, func(p LSPHandlerProvider) LSPDynamicToolHandler { return p.Completion }),
	}
}

// RegisterLSPHandlers wires base LSP handlers into dynTools map.
func RegisterLSPHandlers(dst map[string]LSPDynamicToolHandler, provider LSPHandlerProvider) {
	for _, spec := range baseLSPToolSpecs() {
		dst[spec.schema.Name] = spec.handler(provider)
	}
}

// LSPTools returns base LSP dynamic tool schemas.
func LSPTools() []agentcore.DynamicTool {
	specs := baseLSPToolSpecs()
	out := make([]agentcore.DynamicTool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.schema)
	}
	return out
}

// LSPExtTools is intentionally empty in P2 to remove legacy extension surface.
func LSPExtTools() []LSPExtRegistryProvider {
	return nil
}
