package lsp

import (
	"context"
	"encoding/json"
	"fmt"
)

// CallHierarchy 查询调用层级。
func (c *Client) CallHierarchy(ctx context.Context, uri string, line, character int, direction string) ([]CallHierarchyResult, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}

	items, err := c.prepareCallHierarchy(ctx, uri, line, character)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	out := make([]CallHierarchyResult, 0, len(items))
	for _, item := range items {
		entry := CallHierarchyResult{Item: item}
		if direction == "incoming" || direction == "both" {
			incoming, err := c.callHierarchyIncoming(ctx, item)
			if err != nil {
				return nil, err
			}
			entry.Incoming = incoming
		}
		if direction == "outgoing" || direction == "both" {
			outgoing, err := c.callHierarchyOutgoing(ctx, item)
			if err != nil {
				return nil, err
			}
			entry.Outgoing = outgoing
		}
		out = append(out, entry)
	}

	return out, nil
}

func (c *Client) prepareCallHierarchy(ctx context.Context, uri string, line, character int) ([]CallHierarchyItem, error) {
	_ = ctx
	var raw json.RawMessage
	err := c.call("textDocument/prepareCallHierarchy", PrepareCallHierarchyParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}, &raw)
	if err != nil {
		return nil, err
	}
	return decodePrepareCallHierarchyItems(raw)
}

func (c *Client) callHierarchyIncoming(ctx context.Context, item CallHierarchyItem) ([]CallHierarchyIncomingCall, error) {
	_ = ctx
	var raw json.RawMessage
	err := c.call("callHierarchy/incomingCalls", CallHierarchyIncomingCallsParams{Item: item}, &raw)
	if err != nil {
		return nil, err
	}
	if isNullRaw(raw) {
		return nil, nil
	}
	var calls []CallHierarchyIncomingCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, fmt.Errorf("decode callHierarchy incoming: %w", err)
	}
	return calls, nil
}

func (c *Client) callHierarchyOutgoing(ctx context.Context, item CallHierarchyItem) ([]CallHierarchyOutgoingCall, error) {
	_ = ctx
	var raw json.RawMessage
	err := c.call("callHierarchy/outgoingCalls", CallHierarchyOutgoingCallsParams{Item: item}, &raw)
	if err != nil {
		return nil, err
	}
	if isNullRaw(raw) {
		return nil, nil
	}
	var calls []CallHierarchyOutgoingCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, fmt.Errorf("decode callHierarchy outgoing: %w", err)
	}
	return calls, nil
}

// TypeHierarchy 查询类型层级。
func (c *Client) TypeHierarchy(ctx context.Context, uri string, line, character int, direction string) ([]TypeHierarchyResult, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}

	items, err := c.prepareTypeHierarchy(ctx, uri, line, character)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	out := make([]TypeHierarchyResult, 0, len(items))
	for _, item := range items {
		entry := TypeHierarchyResult{Item: item}
		if direction == "supertypes" || direction == "both" {
			supertypes, err := c.typeHierarchySupertypes(ctx, item)
			if err != nil {
				return nil, err
			}
			entry.Supertypes = supertypes
		}
		if direction == "subtypes" || direction == "both" {
			subtypes, err := c.typeHierarchySubtypes(ctx, item)
			if err != nil {
				return nil, err
			}
			entry.Subtypes = subtypes
		}
		out = append(out, entry)
	}

	return out, nil
}

func (c *Client) prepareTypeHierarchy(ctx context.Context, uri string, line, character int) ([]TypeHierarchyItem, error) {
	_ = ctx
	var raw json.RawMessage
	err := c.call("textDocument/prepareTypeHierarchy", PrepareTypeHierarchyParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}, &raw)
	if err != nil {
		return nil, err
	}
	return decodePrepareTypeHierarchyItems(raw)
}

func (c *Client) typeHierarchySupertypes(ctx context.Context, item TypeHierarchyItem) ([]TypeHierarchyItem, error) {
	_ = ctx
	var raw json.RawMessage
	err := c.call("typeHierarchy/supertypes", TypeHierarchySupertypesParams{Item: item}, &raw)
	if err != nil {
		return nil, err
	}
	if isNullRaw(raw) {
		return nil, nil
	}
	var items []TypeHierarchyItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode typeHierarchy supertypes: %w", err)
	}
	return items, nil
}

func (c *Client) typeHierarchySubtypes(ctx context.Context, item TypeHierarchyItem) ([]TypeHierarchyItem, error) {
	_ = ctx
	var raw json.RawMessage
	err := c.call("typeHierarchy/subtypes", TypeHierarchySubtypesParams{Item: item}, &raw)
	if err != nil {
		return nil, err
	}
	if isNullRaw(raw) {
		return nil, nil
	}
	var items []TypeHierarchyItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode typeHierarchy subtypes: %w", err)
	}
	return items, nil
}
