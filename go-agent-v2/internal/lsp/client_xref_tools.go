package lsp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Implementation 返回实现位置，兼容 Location/Location[]/LocationLink[]。
func (c *Client) Implementation(ctx context.Context, uri string, line, character int) ([]LocationResult, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}

	var raw json.RawMessage
	err := c.call("textDocument/implementation", DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}, &raw)
	if err != nil {
		return nil, err
	}
	return decodeLocationsLike(raw)
}

// TypeDefinition 返回类型定义位置，兼容 Location/Location[]/LocationLink[]。
func (c *Client) TypeDefinition(ctx context.Context, uri string, line, character int) ([]LocationResult, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}

	var raw json.RawMessage
	err := c.call("textDocument/typeDefinition", DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}, &raw)
	if err != nil {
		return nil, err
	}
	return decodeLocationsLike(raw)
}
