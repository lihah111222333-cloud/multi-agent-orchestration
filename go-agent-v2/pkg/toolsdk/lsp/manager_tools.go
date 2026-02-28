package lsp

import (
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

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

func (m *Manager) SignatureHelp(filePath string, line, character int) (*SignatureHelpResult, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) (*SignatureHelpResult, error) {
		return client.SignatureHelp(m.ctx, uri, line, character)
	})
}

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
		if kind := strings.TrimSpace(item); kind != "" {
			out = append(out, kind)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *Manager) SemanticTokens(filePath string) (*SemanticTokensResult, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) (*SemanticTokensResult, error) {
		return client.SemanticTokens(m.ctx, uri)
	})
}

func (m *Manager) FoldingRange(filePath string) ([]FoldingRange, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]FoldingRange, error) {
		return client.FoldingRange(m.ctx, uri)
	})
}

func (m *Manager) Implementation(filePath string, line, character int) ([]LocationResult, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]LocationResult, error) {
		return client.Implementation(m.ctx, uri, line, character)
	})
}

func (m *Manager) TypeDefinition(filePath string, line, character int) ([]LocationResult, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]LocationResult, error) {
		return client.TypeDefinition(m.ctx, uri, line, character)
	})
}

func (m *Manager) CallHierarchy(filePath string, line, character int, direction string) ([]CallHierarchyResult, error) {
	dir, err := normalizeCallHierarchyDirection(direction)
	if err != nil {
		return nil, err
	}
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]CallHierarchyResult, error) {
		return client.CallHierarchy(m.ctx, uri, line, character, dir)
	})
}

func (m *Manager) TypeHierarchy(filePath string, line, character int, direction string) ([]TypeHierarchyResult, error) {
	dir, err := normalizeTypeHierarchyDirection(direction)
	if err != nil {
		return nil, err
	}
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]TypeHierarchyResult, error) {
		return client.TypeHierarchy(m.ctx, uri, line, character, dir)
	})
}

func normalizeDirection(direction string, allowed []string, scope, invalidMsg string) (string, error) {
	dir := strings.ToLower(strings.TrimSpace(direction))
	if dir == "" {
		return "both", nil
	}
	for _, kind := range allowed {
		if dir == kind {
			return dir, nil
		}
	}
	return "", apperrors.New(scope, invalidMsg)
}

func normalizeCallHierarchyDirection(direction string) (string, error) {
	return normalizeDirection(direction, []string{"incoming", "outgoing", "both"}, "LSP.CallHierarchy", "direction must be incoming|outgoing|both")
}

func normalizeTypeHierarchyDirection(direction string) (string, error) {
	return normalizeDirection(direction, []string{"supertypes", "subtypes", "both"}, "LSP.TypeHierarchy", "direction must be supertypes|subtypes|both")
}
