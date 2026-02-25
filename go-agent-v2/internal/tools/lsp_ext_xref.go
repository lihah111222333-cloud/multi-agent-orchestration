package tools

// LSPExtXRef returns the xref ext registry provider.
func LSPExtXRef() LSPExtRegistryProvider {
	workspaceSchema := lspSchema(
		map[string]any{
			"file_path": lspStringProperty("Absolute or relative path used to infer language"),
			"language":  lspStringProperty("Language name or alias: go/rust/typescript/python/c"),
			"query":     lspStringProperty("Symbol query"),
		},
		lspRequired("query"),
		map[string]any{
			"oneOf": []map[string]any{
				{"required": []string{"query", "file_path"}, "not": map[string]any{"required": []string{"language"}}},
				{"required": []string{"query", "language"}, "not": map[string]any{"required": []string{"file_path"}}},
			},
		},
	)

	lineColumnSchema := lspFileLineColumnSchema("", "", "", nil, lspRequired("file_path", "line", "column"), nil)
	return buildLSPExtRegistryProvider("xref.tools", []lspToolBinding{
		lspBinding(
			"lsp_workspace_symbol",
			"Search symbols in workspace by query. Requires exactly one selector: file_path+query or language+query.",
			workspaceSchema,
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.WorkspaceSymbol },
		),
		lspBinding(
			"lsp_implementation",
			"Find implementation locations for symbol at file_path:line:column.",
			lineColumnSchema,
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.Implementation },
		),
		lspBinding(
			"lsp_type_definition",
			"Find type definition locations for symbol at file_path:line:column.",
			lineColumnSchema,
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.TypeDefinition },
		),
	})
}
