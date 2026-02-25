package tools

// LSPExtSemantic returns the semantic ext registry provider.
func LSPExtSemantic() LSPExtRegistryProvider {
	return buildLSPExtRegistryProvider("semantic.tools", []lspToolBinding{
		lspBinding(
			"lsp_semantic_tokens",
			"Get semantic tokens for a file. Decoded token output is limited to 200 items.",
			lspFilePathSchema("", true, nil, nil),
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.SemanticTokens },
		),
		lspBinding(
			"lsp_folding_range",
			"Get folding ranges for a file with boundary filtering.",
			lspFilePathSchema("", true, nil, nil),
			func(provider LSPProvider) LSPDynamicToolHandler { return provider.FoldingRange },
		),
	})
}
