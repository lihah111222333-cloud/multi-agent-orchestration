package tools

// LSPExtActions returns the actions ext registry provider.
func LSPExtActions() LSPExtRegistryProvider {
	return buildLSPExtRegistryProvider("actions.tools", []lspToolBinding{
		lspBinding(
			"lsp_code_action",
			"Get code actions/commands at a document range. Supports optional end_line/end_column and action kinds filter.",
			lspFileLineColumnSchema(
				"",
				"0-indexed start line number",
				"0-indexed start column number",
				map[string]any{
					"end_line":   lspNumberProperty("0-indexed end line number (default: line)"),
					"end_column": lspNumberProperty("0-indexed end column number (default: column)"),
					"only":       lspStringArrayProperty("Optional code action kinds filter"),
				},
				lspRequired("file_path", "line", "column"),
				nil,
			),
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.CodeAction },
		),
		lspBinding(
			"lsp_signature_help",
			"Get signature help at file_path:line:column.",
			lspFileLineColumnSchema("", "", "", nil, lspRequired("file_path", "line", "column"), nil),
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.SignatureHelp },
		),
		lspBinding(
			"lsp_format",
			"Get formatting text edits for a file. Returns TextEdit[] and does not apply edits automatically.",
			lspFilePathSchema(
				"",
				true,
				map[string]any{
					"tab_size":      lspNumberProperty("Tab size (default: 4)"),
					"insert_spaces": lspBooleanProperty("Use spaces for indentation (default: true)"),
				},
				nil,
			),
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.Format },
		),
	})
}
