package lsp

import (
	"context"
	"encoding/json"
	"fmt"
)

const defaultFormattingTabSize = 4

// CodeAction 查询指定范围可用的 code action/command。
func (c *Client) CodeAction(
	ctx context.Context,
	uri string,
	line, character, endLine, endCharacter int,
	only []string,
) ([]CodeActionResult, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	_ = ctx

	params := CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range: Range{
			Start: Position{Line: line, Character: character},
			End:   Position{Line: endLine, Character: endCharacter},
		},
		Context: CodeActionContext{
			Diagnostics: []Diagnostic{},
		},
	}
	if len(only) > 0 {
		params.Context.Only = append([]string(nil), only...)
	}

	var raw json.RawMessage
	if err := c.call("textDocument/codeAction", params, &raw); err != nil {
		return nil, err
	}
	return decodeCodeActions(raw)
}

// SignatureHelp 查询指定位置的函数签名提示。
func (c *Client) SignatureHelp(ctx context.Context, uri string, line, character int) (*SignatureHelpResult, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	_ = ctx

	var raw json.RawMessage
	if err := c.call("textDocument/signatureHelp", SignatureHelpParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}, &raw); err != nil {
		return nil, err
	}
	return decodeSignatureHelp(raw)
}

// Format 获取文档格式化建议，不自动应用编辑。
func (c *Client) Format(ctx context.Context, uri string, tabSize int, insertSpaces bool) ([]TextEdit, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	_ = ctx

	if tabSize <= 0 {
		tabSize = defaultFormattingTabSize
	}

	var raw json.RawMessage
	if err := c.call("textDocument/formatting", DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Options: FormattingOptions{
			TabSize:      tabSize,
			InsertSpaces: insertSpaces,
		},
	}, &raw); err != nil {
		return nil, err
	}
	return decodeTextEdits(raw)
}

// SemanticTokens 获取语义高亮 token，并按 legend 解码。
func (c *Client) SemanticTokens(ctx context.Context, uri string) (*SemanticTokensResult, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	_ = ctx

	legend := c.SemanticTokensLegend()
	if legend == nil {
		return nil, fmt.Errorf("semantic tokens legend unavailable")
	}

	var raw json.RawMessage
	if err := c.call("textDocument/semanticTokens/full", SemanticTokensParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}, &raw); err != nil {
		return nil, err
	}

	tokens, err := decodeSemanticTokens(raw)
	if err != nil {
		return nil, err
	}
	if tokens == nil {
		return nil, nil
	}

	decoded, err := decodeSemanticTokenData(tokens.Data, legend, SemanticTokenResultLimit)
	if err != nil {
		return nil, err
	}

	return &SemanticTokensResult{
		ResultID: tokens.ResultID,
		Data:     limitSemanticTokenData(tokens.Data, SemanticTokenResultLimit),
		Decoded:  decoded,
	}, nil
}

func limitSemanticTokenData(data []int, tokenLimit int) []int {
	if len(data) == 0 {
		return nil
	}
	if tokenLimit <= 0 {
		tokenLimit = SemanticTokenResultLimit
	}

	maxDataLen := tokenLimit * 5
	if maxDataLen > len(data) {
		maxDataLen = len(data)
	}

	return append([]int(nil), data[:maxDataLen]...)
}

// FoldingRange 获取可折叠区间，并执行边界过滤。
func (c *Client) FoldingRange(ctx context.Context, uri string) ([]FoldingRange, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	_ = ctx

	var raw json.RawMessage
	if err := c.call("textDocument/foldingRange", FoldingRangeParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}, &raw); err != nil {
		return nil, err
	}

	return decodeFoldingRanges(raw)
}

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
