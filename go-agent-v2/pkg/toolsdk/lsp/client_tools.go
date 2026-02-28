package lsp

import (
	"context"
	"encoding/json"
	"fmt"
)

const defaultFormattingTabSize = 4

// CodeAction 查询指定范围可用的 code action/command。
func (c *Client) CodeAction(
	_ context.Context,
	uri string,
	line, character, endLine, endCharacter int,
	only []string,
) ([]CodeActionResult, error) {
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

	raw, err := c.callRawWhenRunning("textDocument/codeAction", params)
	if err != nil {
		return nil, err
	}
	return decodeCodeActions(raw)
}

// SignatureHelp 查询指定位置的函数签名提示。
func (c *Client) SignatureHelp(_ context.Context, uri string, line, character int) (*SignatureHelpResult, error) {
	raw, err := c.callPositionRaw("textDocument/signatureHelp", uri, line, character)
	if err != nil {
		return nil, err
	}
	return decodeSignatureHelp(raw)
}

// Format 获取文档格式化建议，不自动应用编辑。
func (c *Client) Format(_ context.Context, uri string, tabSize int, insertSpaces bool) ([]TextEdit, error) {
	if tabSize <= 0 {
		tabSize = defaultFormattingTabSize
	}

	raw, err := c.callRawWhenRunning("textDocument/formatting", DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Options: FormattingOptions{
			TabSize:      tabSize,
			InsertSpaces: insertSpaces,
		},
	})
	if err != nil {
		return nil, err
	}
	return decodeTextEdits(raw)
}

// SemanticTokens 获取语义高亮 token，并按 legend 解码。
func (c *Client) SemanticTokens(_ context.Context, uri string) (*SemanticTokensResult, error) {
	if err := c.ensureRunning(); err != nil {
		return nil, err
	}

	legend := c.SemanticTokensLegend()
	if legend == nil {
		return nil, fmt.Errorf("semantic tokens legend unavailable")
	}

	raw, err := c.callRawWhenRunning("textDocument/semanticTokens/full", SemanticTokensParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	if err != nil {
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
	if maxDataLen := tokenLimit * 5; maxDataLen < len(data) {
		return append([]int(nil), data[:maxDataLen]...)
	}
	return append([]int(nil), data...)
}

// FoldingRange 获取可折叠区间，并执行边界过滤。
func (c *Client) FoldingRange(_ context.Context, uri string) ([]FoldingRange, error) {
	raw, err := c.callRawWhenRunning("textDocument/foldingRange", FoldingRangeParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	if err != nil {
		return nil, err
	}

	return decodeFoldingRanges(raw)
}

// Implementation 返回实现位置，兼容 Location/Location[]/LocationLink[]。
func (c *Client) Implementation(_ context.Context, uri string, line, character int) ([]LocationResult, error) {
	return c.positionLocationsLike("textDocument/implementation", uri, line, character)
}

// TypeDefinition 返回类型定义位置，兼容 Location/Location[]/LocationLink[]。
func (c *Client) TypeDefinition(_ context.Context, uri string, line, character int) ([]LocationResult, error) {
	return c.positionLocationsLike("textDocument/typeDefinition", uri, line, character)
}

func (c *Client) positionLocationsLike(method, uri string, line, character int) ([]LocationResult, error) {
	raw, err := c.callPositionRaw(method, uri, line, character)
	if err != nil {
		return nil, err
	}
	return decodeLocationsLike(raw)
}

// CallHierarchy 查询调用层级。
func (c *Client) CallHierarchy(ctx context.Context, uri string, line, character int, direction string) ([]CallHierarchyResult, error) {
	if err := c.ensureRunning(); err != nil {
		return nil, err
	}

	items, err := prepareHierarchyItems(c, "textDocument/prepareCallHierarchy", uri, line, character, decodePrepareCallHierarchyItems)
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
			incoming, err := callNullableSlice[CallHierarchyIncomingCall](c, "callHierarchy/incomingCalls", CallHierarchyIncomingCallsParams{Item: item}, "decode callHierarchy incoming")
			if err != nil {
				return nil, err
			}
			entry.Incoming = incoming
		}
		if direction == "outgoing" || direction == "both" {
			outgoing, err := callNullableSlice[CallHierarchyOutgoingCall](c, "callHierarchy/outgoingCalls", CallHierarchyOutgoingCallsParams{Item: item}, "decode callHierarchy outgoing")
			if err != nil {
				return nil, err
			}
			entry.Outgoing = outgoing
		}
		out = append(out, entry)
	}

	return out, nil
}

// TypeHierarchy 查询类型层级。
func (c *Client) TypeHierarchy(ctx context.Context, uri string, line, character int, direction string) ([]TypeHierarchyResult, error) {
	if err := c.ensureRunning(); err != nil {
		return nil, err
	}

	items, err := prepareHierarchyItems(c, "textDocument/prepareTypeHierarchy", uri, line, character, decodePrepareTypeHierarchyItems)
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
			supertypes, err := callNullableSlice[TypeHierarchyItem](c, "typeHierarchy/supertypes", TypeHierarchySupertypesParams{Item: item}, "decode typeHierarchy supertypes")
			if err != nil {
				return nil, err
			}
			entry.Supertypes = supertypes
		}
		if direction == "subtypes" || direction == "both" {
			subtypes, err := callNullableSlice[TypeHierarchyItem](c, "typeHierarchy/subtypes", TypeHierarchySubtypesParams{Item: item}, "decode typeHierarchy subtypes")
			if err != nil {
				return nil, err
			}
			entry.Subtypes = subtypes
		}
		out = append(out, entry)
	}

	return out, nil
}

func (c *Client) callPositionRaw(method, uri string, line, character int) (json.RawMessage, error) {
	return c.callRawWhenRunning(method, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	})
}

func prepareHierarchyItems[T any](
	client *Client,
	method, uri string,
	line, character int,
	decode func(json.RawMessage) ([]T, error),
) ([]T, error) {
	raw, err := client.callPositionRaw(method, uri, line, character)
	if err != nil {
		return nil, err
	}
	return decode(raw)
}

func callNullableSlice[T any](client *Client, method string, params any, errPrefix string) ([]T, error) {
	raw, err := client.callRawWhenRunning(method, params)
	if err != nil {
		return nil, err
	}
	if isNullRaw(raw) {
		return nil, nil
	}
	var items []T
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return items, nil
}
