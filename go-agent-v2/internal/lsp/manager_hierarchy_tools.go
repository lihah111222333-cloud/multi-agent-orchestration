package lsp

import (
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

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
