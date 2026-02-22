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
