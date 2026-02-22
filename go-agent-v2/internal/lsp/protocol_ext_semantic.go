package lsp

import (
	"encoding/json"
	"fmt"
)

// SemanticTokenResultLimit 是 semantic_tokens decoded 输出上限。
const SemanticTokenResultLimit = 200

// SemanticTokensParams textDocument/semanticTokens/full 请求参数。
type SemanticTokensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SemanticTokens textDocument/semanticTokens/full 原始返回。
type SemanticTokens struct {
	ResultID string `json:"resultId,omitempty"`
	Data     []int  `json:"data"`
}

// DecodedSemanticToken 是相对编码展开后的语义 token。
type DecodedSemanticToken struct {
	Line           int      `json:"line"`
	StartCharacter int      `json:"startCharacter"`
	Length         int      `json:"length"`
	TokenType      string   `json:"tokenType"`
	TokenModifiers []string `json:"tokenModifiers,omitempty"`
}

// SemanticTokensResult 是稳定输出结构，包含原始与解码结果。
type SemanticTokensResult struct {
	ResultID string                 `json:"resultId,omitempty"`
	Data     []int                  `json:"data,omitempty"`
	Decoded  []DecodedSemanticToken `json:"decoded,omitempty"`
}

// FoldingRangeParams textDocument/foldingRange 请求参数。
type FoldingRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// FoldingRange 是折叠区间。
type FoldingRange struct {
	StartLine      int    `json:"startLine"`
	StartCharacter *int   `json:"startCharacter,omitempty"`
	EndLine        int    `json:"endLine"`
	EndCharacter   *int   `json:"endCharacter,omitempty"`
	Kind           string `json:"kind,omitempty"`
	CollapsedText  string `json:"collapsedText,omitempty"`
}

// decodeSemanticTokens 兼容解码:
// SemanticTokens | int[] | null
func decodeSemanticTokens(raw json.RawMessage) (*SemanticTokens, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var payload SemanticTokens
	if err := json.Unmarshal(raw, &payload); err == nil {
		return &payload, nil
	}

	var data []int
	if err := json.Unmarshal(raw, &data); err == nil {
		return &SemanticTokens{Data: data}, nil
	}

	return nil, fmt.Errorf("decode semantic tokens: unsupported payload")
}

func decodeSemanticTokenData(data []int, legend *SemanticTokensLegend, limit int) ([]DecodedSemanticToken, error) {
	if legend == nil {
		return nil, fmt.Errorf("semantic tokens legend unavailable")
	}
	if len(data)%5 != 0 {
		return nil, fmt.Errorf("semantic token data length must be multiple of 5")
	}
	if limit <= 0 {
		limit = SemanticTokenResultLimit
	}

	out := make([]DecodedSemanticToken, 0, minInt(len(data)/5, limit))
	currentLine := 0
	currentStart := 0

	for i := 0; i+4 < len(data); i += 5 {
		deltaLine := data[i]
		deltaStart := data[i+1]
		length := data[i+2]
		tokenTypeIndex := data[i+3]
		modifierBits := data[i+4]

		if deltaLine < 0 || deltaStart < 0 || length < 0 || tokenTypeIndex < 0 || modifierBits < 0 {
			return nil, fmt.Errorf("semantic token data contains negative value")
		}

		if deltaLine == 0 {
			currentStart += deltaStart
		} else {
			currentLine += deltaLine
			currentStart = deltaStart
		}

		tokenType := fmt.Sprintf("unknown(%d)", tokenTypeIndex)
		if tokenTypeIndex < len(legend.TokenTypes) {
			tokenType = legend.TokenTypes[tokenTypeIndex]
		}

		out = append(out, DecodedSemanticToken{
			Line:           currentLine,
			StartCharacter: currentStart,
			Length:         length,
			TokenType:      tokenType,
			TokenModifiers: decodeTokenModifiers(modifierBits, legend.TokenModifiers),
		})
		if len(out) >= limit {
			break
		}
	}

	return out, nil
}

func decodeTokenModifiers(bits int, modifierNames []string) []string {
	if bits == 0 || len(modifierNames) == 0 {
		return nil
	}

	out := make([]string, 0, len(modifierNames))
	for i, name := range modifierNames {
		if bits&(1<<i) != 0 {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// decodeFoldingRanges 兼容解码:
// []FoldingRange | null，并做空值与边界过滤。
func decodeFoldingRanges(raw json.RawMessage) ([]FoldingRange, error) {
	if isNullRaw(raw) {
		return nil, nil
	}

	var ranges []FoldingRange
	if err := json.Unmarshal(raw, &ranges); err != nil {
		return nil, fmt.Errorf("decode folding ranges: %w", err)
	}

	out := make([]FoldingRange, 0, len(ranges))
	for _, item := range ranges {
		if !validFoldingRange(item) {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func validFoldingRange(item FoldingRange) bool {
	if item.StartLine < 0 || item.EndLine < 0 || item.EndLine < item.StartLine {
		return false
	}
	if item.StartCharacter != nil && *item.StartCharacter < 0 {
		return false
	}
	if item.EndCharacter != nil && *item.EndCharacter < 0 {
		return false
	}
	if item.StartLine == item.EndLine && item.StartCharacter != nil && item.EndCharacter != nil && *item.EndCharacter < *item.StartCharacter {
		return false
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
