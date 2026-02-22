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
