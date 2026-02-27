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
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]CodeActionResult, error) {
		return client.CodeAction(m.ctx, uri, line, character, resolvedEndLine, resolvedEndCharacter, resolvedOnly)
	})
}

// SignatureHelp 查询指定位置签名提示。
func (m *Manager) SignatureHelp(filePath string, line, character int) (*SignatureHelpResult, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) (*SignatureHelpResult, error) {
		return client.SignatureHelp(m.ctx, uri, line, character)
	})
}

// Format 获取格式化文本编辑建议，不自动应用。
func (m *Manager) Format(filePath string, tabSize int, insertSpaces bool) ([]TextEdit, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]TextEdit, error) {
		return client.Format(m.ctx, uri, tabSize, insertSpaces)
	})
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
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) (*SemanticTokensResult, error) {
		return client.SemanticTokens(m.ctx, uri)
	})
}

// FoldingRange 获取文档折叠区间。
func (m *Manager) FoldingRange(filePath string) ([]FoldingRange, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]FoldingRange, error) {
		return client.FoldingRange(m.ctx, uri)
	})
}

// Implementation 查找符号实现位置。
func (m *Manager) Implementation(filePath string, line, character int) ([]LocationResult, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]LocationResult, error) {
		return client.Implementation(m.ctx, uri, line, character)
	})
}

// TypeDefinition 查找符号类型定义位置。
func (m *Manager) TypeDefinition(filePath string, line, character int) ([]LocationResult, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]LocationResult, error) {
		return client.TypeDefinition(m.ctx, uri, line, character)
	})
}

// CallHierarchy 查询调用层级，direction: incoming|outgoing|both。
func (m *Manager) CallHierarchy(filePath string, line, character int, direction string) ([]CallHierarchyResult, error) {
	dir, err := normalizeCallHierarchyDirection(direction)
	if err != nil {
		return nil, err
	}

	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]CallHierarchyResult, error) {
		return client.CallHierarchy(m.ctx, uri, line, character, dir)
	})
}

// TypeHierarchy 查询类型层级，direction: supertypes|subtypes|both。
func (m *Manager) TypeHierarchy(filePath string, line, character int, direction string) ([]TypeHierarchyResult, error) {
	dir, err := normalizeTypeHierarchyDirection(direction)
	if err != nil {
		return nil, err
	}

	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]TypeHierarchyResult, error) {
		return client.TypeHierarchy(m.ctx, uri, line, character, dir)
	})
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
