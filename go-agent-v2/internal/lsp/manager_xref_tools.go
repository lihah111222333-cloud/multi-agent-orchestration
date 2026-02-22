package lsp

// Implementation 查找符号实现位置。
func (m *Manager) Implementation(filePath string, line, character int) ([]LocationResult, error) {
	var out []LocationResult
	err := m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, err := client.Implementation(m.ctx, uri, line, character)
		if err != nil {
			return err
		}
		out = result
		return nil
	})
	return out, err
}

// TypeDefinition 查找符号类型定义位置。
func (m *Manager) TypeDefinition(filePath string, line, character int) ([]LocationResult, error) {
	var out []LocationResult
	err := m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, err := client.TypeDefinition(m.ctx, uri, line, character)
		if err != nil {
			return err
		}
		out = result
		return nil
	})
	return out, err
}
