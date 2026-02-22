package lsp

// SemanticTokens 获取文档语义 tokens（含 decoded）。
func (m *Manager) SemanticTokens(filePath string) (*SemanticTokensResult, error) {
	var out *SemanticTokensResult
	err := m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, callErr := client.SemanticTokens(m.ctx, uri)
		if callErr != nil {
			return callErr
		}
		out = result
		return nil
	})
	return out, err
}

// FoldingRange 获取文档折叠区间。
func (m *Manager) FoldingRange(filePath string) ([]FoldingRange, error) {
	var out []FoldingRange
	err := m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, callErr := client.FoldingRange(m.ctx, uri)
		if callErr != nil {
			return callErr
		}
		out = result
		return nil
	})
	return out, err
}
