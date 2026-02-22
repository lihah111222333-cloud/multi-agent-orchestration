package lsp

import (
	"context"
	"encoding/json"
	"fmt"
)

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
