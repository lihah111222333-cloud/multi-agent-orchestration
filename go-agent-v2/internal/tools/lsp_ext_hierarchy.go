package tools

// LSPExtHierarchy returns the hierarchy ext registry provider.
func LSPExtHierarchy() LSPExtRegistryProvider {
	return buildLSPExtRegistryProvider("hierarchy.tools", []lspToolBinding{
		lspBinding(
			"lsp_call_hierarchy",
			"Get call hierarchy for symbol at file_path:line:column. Direction: incoming|outgoing|both.",
			lspFileLineColumnSchema(
				"",
				"",
				"",
				map[string]any{"direction": lspEnumStringProperty("Hierarchy direction (default: both)", []string{"incoming", "outgoing", "both"})},
				lspRequired("file_path", "line", "column"),
				nil,
			),
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.CallHierarchy },
		),
		lspBinding(
			"lsp_type_hierarchy",
			"Get type hierarchy for symbol at file_path:line:column. Direction: supertypes|subtypes|both.",
			lspFileLineColumnSchema(
				"",
				"",
				"",
				map[string]any{"direction": lspEnumStringProperty("Hierarchy direction (default: both)", []string{"supertypes", "subtypes", "both"})},
				lspRequired("file_path", "line", "column"),
				nil,
			),
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.TypeHierarchy },
		),
	})
}
