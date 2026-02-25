package tools

import (
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
)

type LSPDynamicToolHandler = func(json.RawMessage) string

// LSPHandlerProvider defines the LSP dynamic-tool handler surface.
type LSPHandlerProvider interface {
	AvailabilitySummary() map[string]any
	DiagnosticsQuery(filePath string) map[string]any
	Hover(json.RawMessage) string
	OpenFile(json.RawMessage) string
	Diagnostics(json.RawMessage) string
	Definition(json.RawMessage) string
	References(json.RawMessage) string
	DocumentSymbol(json.RawMessage) string
	Rename(json.RawMessage) string
	Completion(json.RawMessage) string
	DidChange(json.RawMessage) string
	CodeAction(json.RawMessage) string
	SignatureHelp(json.RawMessage) string
	Format(json.RawMessage) string
	CallHierarchy(json.RawMessage) string
	TypeHierarchy(json.RawMessage) string
	SemanticTokens(json.RawMessage) string
	FoldingRange(json.RawMessage) string
	WorkspaceSymbol(json.RawMessage) string
	Implementation(json.RawMessage) string
	TypeDefinition(json.RawMessage) string
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
	return []lspBaseToolSpec{
		lspBaseSpec(
			"lsp_hover",
			"Get type info and documentation for a symbol at a specific position in a file via LSP hover.",
			lspFileLineColumnSchema("", "", "", nil, lspRequired("file_path", "line", "column"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.Hover },
		),
		lspBaseSpec(
			"lsp_open_file",
			"Open a file for LSP analysis. Triggers didOpen and starts diagnostics. Call before hover/diagnostics for accurate results.",
			lspFilePathSchema("", true, nil, nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.OpenFile },
		),
		lspBaseSpec(
			"lsp_diagnostics",
			"Get current diagnostics (errors, warnings) for a file. If file_path is provided and the file was not opened, it will be auto-synchronized first.",
			lspFilePathSchema("Absolute or relative path to the file. Empty = all files.", false, nil, nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.Diagnostics },
		),
		lspBaseSpec(
			"lsp_definition",
			"Go to definition. Returns the location(s) where a symbol is defined. The document is auto-bootstrapped if not opened yet.",
			lspFileLineColumnSchema("", "", "", nil, lspRequired("file_path", "line", "column"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.Definition },
		),
		lspBaseSpec(
			"lsp_references",
			"Find all references to a symbol. Returns locations where the symbol is used. The document is auto-bootstrapped if not opened yet.",
			lspFileLineColumnSchema(
				"",
				"",
				"",
				map[string]any{"include_declaration": lspBooleanProperty("Include the declaration in results (default: true)")},
				lspRequired("file_path", "line", "column"),
				nil,
			),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.References },
		),
		lspBaseSpec(
			"lsp_document_symbol",
			"Get file outline (all symbols: functions, types, methods, constants). Returns a hierarchical symbol tree. The document is auto-bootstrapped if not opened yet.",
			lspFilePathSchema("", true, nil, nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.DocumentSymbol },
		),
		lspBaseSpec(
			"lsp_rename",
			"Rename a symbol across all files. Returns all edits needed. The document is auto-bootstrapped if not opened yet.",
			lspFileLineColumnSchema(
				"",
				"",
				"",
				map[string]any{"new_name": lspStringProperty("New name for the symbol")},
				lspRequired("file_path", "line", "column", "new_name"),
				nil,
			),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.Rename },
		),
		lspBaseSpec(
			"lsp_completion",
			"Get code completion suggestions at a position. Returns candidate items with labels and kinds. The document is auto-bootstrapped if not opened yet.",
			lspFileLineColumnSchema("", "", "", nil, lspRequired("file_path", "line", "column"), nil),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.Completion },
		),
		lspBaseSpec(
			"lsp_did_change",
			"Notify the language server that file content has changed. Use after editing a file to keep LSP in sync. By default this does not write to disk; set persist_to_disk=true to atomically persist before syncing LSP.",
			lspSchema(
				map[string]any{
					"file_path":       lspStringProperty(defaultFilePathDescription),
					"new_content":     lspStringProperty("Full new content of the file"),
					"version":         lspNumberProperty("Document version (increment each change, default: 2)"),
					"persist_to_disk": lspBooleanProperty("When true, atomically write new_content to file_path before notifying LSP (default: false)"),
				},
				lspRequired("file_path", "new_content"),
				nil,
			),
			func(provider LSPHandlerProvider) LSPDynamicToolHandler { return provider.DidChange },
		),
	}
}

// RegisterLSPHandlers wires base LSP handlers into dynTools map.
func RegisterLSPHandlers(dst map[string]LSPDynamicToolHandler, provider LSPHandlerProvider) {
	if dst == nil || provider == nil {
		return
	}
	for _, spec := range baseLSPToolSpecs() {
		if spec.handler == nil {
			continue
		}
		handler := spec.handler(provider)
		if handler == nil {
			continue
		}
		dst[spec.schema.Name] = handler
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

// LSPExtTools returns all ext LSP tool providers.
func LSPExtTools() []LSPExtRegistryProvider {
	return []LSPExtRegistryProvider{LSPExtActions(), LSPExtHierarchy(), LSPExtSemantic(), LSPExtXRef()}
}
