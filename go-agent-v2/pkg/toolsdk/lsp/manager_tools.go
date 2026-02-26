package lsp

import (
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// CodeAction 查询范围内 code action，end 省略时默认与起点一致。
func (m *Manager) CodeAction(
	filePath string,
	line, character, endLine, endCharacter int,
	only []string,
) ([]CodeActionResult, error) {
	resolvedEndLine, resolvedEndCharacter, err := normalizeCodeActionRange(line, character, endLine, endCharacter)
	if err != nil {
		return nil, err
	}
	resolvedOnly := normalizeCodeActionOnlyKinds(only)

	var out []CodeActionResult
	err = m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, callErr := client.CodeAction(m.ctx, uri, line, character, resolvedEndLine, resolvedEndCharacter, resolvedOnly)
		if callErr != nil {
			return callErr
		}
		out = result
		return nil
	})
	return out, err
}

// SignatureHelp 查询指定位置签名提示。
func (m *Manager) SignatureHelp(filePath string, line, character int) (*SignatureHelpResult, error) {
	var out *SignatureHelpResult
	err := m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, callErr := client.SignatureHelp(m.ctx, uri, line, character)
		if callErr != nil {
			return callErr
		}
		out = result
		return nil
	})
	return out, err
}

// Format 获取格式化文本编辑建议，不自动应用。
func (m *Manager) Format(filePath string, tabSize int, insertSpaces bool) ([]TextEdit, error) {
	var out []TextEdit
	err := m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, callErr := client.Format(m.ctx, uri, tabSize, insertSpaces)
		if callErr != nil {
			return callErr
		}
		out = result
		return nil
	})
	return out, err
}

func normalizeCodeActionRange(line, character, endLine, endCharacter int) (int, int, error) {
	if line < 0 || character < 0 {
		return 0, 0, apperrors.Newf("LSP.CodeAction", "line and column must be >= 0")
	}

	if endLine < 0 {
		endLine = line
	}
	if endCharacter < 0 {
		endCharacter = character
	}
	if endLine < line || (endLine == line && endCharacter < character) {
		return 0, 0, apperrors.Newf("LSP.CodeAction", "range end must be >= start position")
	}

	return endLine, endCharacter, nil
}

func normalizeCodeActionOnlyKinds(only []string) []string {
	if len(only) == 0 {
		return nil
	}
	out := make([]string, 0, len(only))
	for _, item := range only {
		kind := strings.TrimSpace(item)
		if kind == "" {
			continue
		}
		out = append(out, kind)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

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

// CallHierarchy 查询调用层级，direction: incoming|outgoing|both。
func (m *Manager) CallHierarchy(filePath string, line, character int, direction string) ([]CallHierarchyResult, error) {
	dir, err := normalizeCallHierarchyDirection(direction)
	if err != nil {
		return nil, err
	}

	var out []CallHierarchyResult
	err = m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, err := client.CallHierarchy(m.ctx, uri, line, character, dir)
		if err != nil {
			return err
		}
		out = result
		return nil
	})
	return out, err
}

// TypeHierarchy 查询类型层级，direction: supertypes|subtypes|both。
func (m *Manager) TypeHierarchy(filePath string, line, character int, direction string) ([]TypeHierarchyResult, error) {
	dir, err := normalizeTypeHierarchyDirection(direction)
	if err != nil {
		return nil, err
	}

	var out []TypeHierarchyResult
	err = m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, err := client.TypeHierarchy(m.ctx, uri, line, character, dir)
		if err != nil {
			return err
		}
		out = result
		return nil
	})
	return out, err
}

func normalizeCallHierarchyDirection(direction string) (string, error) {
	dir := strings.ToLower(strings.TrimSpace(direction))
	if dir == "" {
		return "both", nil
	}
	switch dir {
	case "incoming", "outgoing", "both":
		return dir, nil
	default:
		return "", apperrors.Newf("LSP.CallHierarchy", "direction must be incoming|outgoing|both")
	}
}

func normalizeTypeHierarchyDirection(direction string) (string, error) {
	dir := strings.ToLower(strings.TrimSpace(direction))
	if dir == "" {
		return "both", nil
	}
	switch dir {
	case "supertypes", "subtypes", "both":
		return dir, nil
	default:
		return "", apperrors.Newf("LSP.TypeHierarchy", "direction must be supertypes|subtypes|both")
	}
}
