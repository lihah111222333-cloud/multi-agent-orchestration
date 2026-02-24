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

// RegisterLSPHandlers wires base LSP handlers into dynTools map.
func RegisterLSPHandlers(dst map[string]LSPDynamicToolHandler, provider LSPHandlerProvider) {
	if dst == nil || provider == nil {
		return
	}
	dst["lsp_hover"] = provider.Hover
	dst["lsp_open_file"] = provider.OpenFile
	dst["lsp_diagnostics"] = provider.Diagnostics
	dst["lsp_definition"] = provider.Definition
	dst["lsp_references"] = provider.References
	dst["lsp_document_symbol"] = provider.DocumentSymbol
	dst["lsp_rename"] = provider.Rename
	dst["lsp_completion"] = provider.Completion
	dst["lsp_did_change"] = provider.DidChange
}

// LSPTools returns base LSP dynamic tool schemas.
func LSPTools() []agentcore.DynamicTool {
	return []agentcore.DynamicTool{
		{
			Name:        "lsp_hover",
			Description: "Get type info and documentation for a symbol at a specific position in a file via LSP hover.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
				},
				"required": []string{"file_path", "line", "column"},
			},
		},
		{
			Name:        "lsp_open_file",
			Description: "Open a file for LSP analysis. Triggers didOpen and starts diagnostics. Call before hover/diagnostics for accurate results.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "lsp_diagnostics",
			Description: "Get current diagnostics (errors, warnings) for a file. If file_path is provided and the file was not opened, it will be auto-synchronized first.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file. Empty = all files."},
				},
			},
		},
		{
			Name:        "lsp_definition",
			Description: "Go to definition. Returns the location(s) where a symbol is defined. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
				},
				"required": []string{"file_path", "line", "column"},
			},
		},
		{
			Name:        "lsp_references",
			Description: "Find all references to a symbol. Returns locations where the symbol is used. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path":           map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":                map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":              map[string]any{"type": "number", "description": "0-indexed column number"},
					"include_declaration": map[string]any{"type": "boolean", "description": "Include the declaration in results (default: true)"},
				},
				"required": []string{"file_path", "line", "column"},
			},
		},
		{
			Name:        "lsp_document_symbol",
			Description: "Get file outline (all symbols: functions, types, methods, constants). Returns a hierarchical symbol tree. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "lsp_rename",
			Description: "Rename a symbol across all files. Returns all edits needed. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
					"new_name":  map[string]any{"type": "string", "description": "New name for the symbol"},
				},
				"required": []string{"file_path", "line", "column", "new_name"},
			},
		},
		{
			Name:        "lsp_completion",
			Description: "Get code completion suggestions at a position. Returns candidate items with labels and kinds. The document is auto-bootstrapped if not opened yet.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
				},
				"required": []string{"file_path", "line", "column"},
			},
		},
		{
			Name:        "lsp_did_change",
			Description: "Notify the language server that file content has changed. Use after editing a file to keep LSP in sync. Supports unopened files via automatic bootstrap and fail-closed sync.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path":   map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"new_content": map[string]any{"type": "string", "description": "Full new content of the file"},
					"version":     map[string]any{"type": "number", "description": "Document version (increment each change, default: 2)"},
				},
				"required": []string{"file_path", "new_content"},
			},
		},
	}
}

// LSPExtTools returns all ext LSP tool providers.
func LSPExtTools() []LSPExtRegistryProvider {
	return []LSPExtRegistryProvider{
		LSPExtActions(),
		LSPExtHierarchy(),
		LSPExtSemantic(),
		LSPExtXRef(),
	}
}
