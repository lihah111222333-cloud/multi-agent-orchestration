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
			"File-level LSP operations: open/read/sync document state and query diagnostics.",
			lspSchema(map[string]any{
				"action":          lspEnumStringProperty("File operation to execute", []string{"open_file", "read_file", "did_change", "diagnostics"}),
				"file_path":       lspStringProperty(defaultFilePathDescription),
				"new_content":     lspStringProperty("Full new document content for did_change"),
				"persist_to_disk": lspBooleanProperty("Persist did_change content to disk"),
				"version":         lspNumberProperty("Document version"),
			}, lspRequired("action"), map[string]any{
				"additionalProperties": false,
				"oneOf": []any{
					lspActionCase("open_file", "file_path"),
					lspActionCase("read_file", "file_path"),
					lspActionCase("did_change", "file_path", "new_content"),
					lspActionCase("diagnostics", "file_path"),
				},
			}),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPFile },
		),
		lspBaseSpec(
			"lsp_inspect",
			"LSP inspection operations for symbol/type/location introspection.",
			lspSchema(map[string]any{
				"action":    lspEnumStringProperty("Inspect operation", []string{"hover", "definition", "implementation", "type_definition", "signature_help"}),
				"file_path": lspStringProperty(defaultFilePathDescription),
				"line":      lspNumberProperty(defaultLineDescription),
				"column":    lspNumberProperty(defaultColumnDescription),
			}, lspRequired("action"), map[string]any{
				"additionalProperties": false,
				"oneOf": []any{
					lspActionCase("hover", "file_path", "line", "column"),
					lspActionCase("definition", "file_path", "line", "column"),
					lspActionCase("implementation", "file_path", "line", "column"),
					lspActionCase("type_definition", "file_path", "line", "column"),
					lspActionCase("signature_help", "file_path", "line", "column"),
				},
			}),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPInspect },
		),
		lspBaseSpec(
			"lsp_xref",
			"Cross-reference operations across call/type hierarchies.",
			lspSchema(map[string]any{
				"action":              lspEnumStringProperty("Cross-reference operation", []string{"call_hierarchy", "type_hierarchy", "references"}),
				"file_path":           lspStringProperty(defaultFilePathDescription),
				"line":                lspNumberProperty(defaultLineDescription),
				"column":              lspNumberProperty(defaultColumnDescription),
				"direction":           lspEnumStringProperty("Hierarchy direction", []string{"incoming", "outgoing", "both", "supertypes", "subtypes"}),
				"include_declaration": lspBooleanProperty("Include declaration for references"),
			}, lspRequired("action"), map[string]any{
				"additionalProperties": false,
				"oneOf": []any{
					lspActionCase("references", "file_path", "line", "column"),
					map[string]any{
						"properties": map[string]any{
							"action":    map[string]any{"const": "call_hierarchy"},
							"direction": map[string]any{"enum": []string{"incoming", "outgoing", "both"}},
						},
						"required": []string{"action", "file_path", "line", "column"},
					},
					map[string]any{
						"properties": map[string]any{
							"action":    map[string]any{"const": "type_hierarchy"},
							"direction": map[string]any{"enum": []string{"supertypes", "subtypes", "both"}},
						},
						"required": []string{"action", "file_path", "line", "column"},
					},
				},
			}),
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
			}, lspRequired("action"), map[string]any{
				"additionalProperties": false,
				"oneOf": []any{
					lspActionCase("text_search", "query"),
					lspActionCase("ast_search", "language", "symbol"),
				},
			}),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPGrep },
		),
		lspBaseSpec(
			"lsp_structure",
			"Structural LSP views: document/workspace symbols, folding and semantic tokens.",
			lspSchema(map[string]any{
				"action":    lspEnumStringProperty("Structure operation", []string{"document_symbol", "workspace_symbol", "folding_range", "semantic_tokens"}),
				"file_path": lspStringProperty(defaultFilePathDescription),
				"path":      lspStringProperty("Workspace path selector"),
				"query":     lspStringProperty("Workspace symbol query"),
				"language":  lspStringProperty("Language selector for workspace symbol"),
			}, lspRequired("action"), map[string]any{
				"additionalProperties": false,
				"oneOf": []any{
					lspActionCase("document_symbol", "file_path"),
					lspActionCase("folding_range", "file_path"),
					lspActionCase("semantic_tokens", "file_path"),
					map[string]any{
						"properties": map[string]any{
							"action": map[string]any{"const": "workspace_symbol"},
						},
						"required": []string{"action", "query"},
						"oneOf": []any{
							map[string]any{
								"required": []string{"path"},
								"not":      map[string]any{"required": []string{"language"}},
							},
							map[string]any{
								"required": []string{"language"},
								"not":      map[string]any{"required": []string{"path"}},
							},
						},
					},
				},
			}),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPStructure },
		),
		lspBaseSpec(
			"lsp_edit",
			"Editing operations: rename/code_action/format/replace_range.",
			lspSchema(map[string]any{
				"action":          lspEnumStringProperty("Edit operation", []string{"rename", "code_action", "format", "replace_range"}),
				"file_path":       lspStringProperty(defaultFilePathDescription),
				"line":            lspNumberProperty(defaultLineDescription),
				"column":          lspNumberProperty(defaultColumnDescription),
				"end_line":        lspNumberProperty("0-indexed end line"),
				"end_column":      lspNumberProperty("0-indexed end column"),
				"new_name":        lspStringProperty("New symbol name for rename"),
				"new_text":        lspStringProperty("Replacement text for replace_range"),
				"insert_spaces":   lspBooleanProperty("Use spaces for formatting"),
				"tab_size":        lspNumberProperty("Tab size for formatting"),
				"only":            lspStringArrayProperty("Code action kinds filter"),
				"version":         lspNumberProperty("Document version"),
				"persist_to_disk": lspBooleanProperty("Persist replace_range result to disk"),
			}, lspRequired("action"), map[string]any{
				"additionalProperties": false,
				"oneOf": []any{
					lspActionCase("rename", "file_path", "line", "column", "new_name"),
					lspActionCase("code_action", "file_path", "line", "column"),
					lspActionCase("format", "file_path"),
					lspActionCase("replace_range", "file_path", "line", "column", "end_line", "end_column", "new_text"),
				},
			}),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.LSPEdit },
		),
		lspBaseSpec(
			"lsp_completion",
			"Code completion suggestions at a cursor position.",
			lspFileLineColumnSchema(defaultFilePathDescription, defaultLineDescription, defaultColumnDescription, nil, lspRequired("file_path", "line", "column"), map[string]any{
				"additionalProperties": false,
			}),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.Completion },
		),
	}
}

func lspActionCase(action string, required ...string) map[string]any {
	req := make([]string, 0, len(required)+1)
	req = append(req, "action")
	req = append(req, required...)
	return map[string]any{
		"properties": map[string]any{
			"action": map[string]any{"const": action},
		},
		"required": req,
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
