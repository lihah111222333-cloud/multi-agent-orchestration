package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSemanticTokens_RequiresFilePath(t *testing.T) {
	m := NewManager(nil)

	_, err := m.SemanticTokens("")
	if err == nil {
		t.Fatal("expected file_path validation error")
	}
	if !strings.Contains(err.Error(), "file_path is required") {
		t.Fatalf("expected file_path error, got %v", err)
	}
}

func TestSemanticTokensDecode_LegendRequired(t *testing.T) {
	_, err := decodeSemanticTokenData([]int{0, 0, 3, 0, 0}, nil, SemanticTokenResultLimit)
	if err == nil {
		t.Fatal("expected legend required error")
	}
	if !strings.Contains(err.Error(), "legend unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSemanticTokensDecode_Limit(t *testing.T) {
	legend := &SemanticTokensLegend{TokenTypes: []string{"type"}, TokenModifiers: []string{"declaration"}}

	total := SemanticTokenResultLimit + 17
	data := make([]int, 0, total*5)
	for range total {
		data = append(data, 0, 1, 2, 0, 1)
	}

	decoded, err := decodeSemanticTokenData(data, legend, SemanticTokenResultLimit)
	if err != nil {
		t.Fatalf("decodeSemanticTokenData: %v", err)
	}
	if len(decoded) != SemanticTokenResultLimit {
		t.Fatalf("len(decoded) = %d, want %d", len(decoded), SemanticTokenResultLimit)
	}
}

func TestLimitSemanticTokenData_LimitAndCopy(t *testing.T) {
	total := SemanticTokenResultLimit + 17
	data := make([]int, 0, total*5)
	for range total {
		data = append(data, 0, 1, 2, 0, 1)
	}

	limited := limitSemanticTokenData(data, SemanticTokenResultLimit)
	if len(limited) != SemanticTokenResultLimit*5 {
		t.Fatalf("len(limited) = %d, want %d", len(limited), SemanticTokenResultLimit*5)
	}

	data[0] = 9
	if limited[0] == 9 {
		t.Fatal("limited data should be copied, but references original slice")
	}

	small := []int{0, 1, 2, 0, 1}
	smallLimited := limitSemanticTokenData(small, SemanticTokenResultLimit)
	if len(smallLimited) != len(small) {
		t.Fatalf("len(smallLimited) = %d, want %d", len(smallLimited), len(small))
	}
}

func TestSemanticTokensDecode_InvalidData(t *testing.T) {
	legend := &SemanticTokensLegend{TokenTypes: []string{"type"}}
	_, err := decodeSemanticTokenData([]int{0, 1, 2, 0}, legend, SemanticTokenResultLimit)
	if err == nil {
		t.Fatal("expected invalid semantic token data error")
	}
	if !strings.Contains(err.Error(), "multiple of 5") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSemanticTokensPayloadDecode_NullAndArray(t *testing.T) {
	tokens, err := decodeSemanticTokens(json.RawMessage("null"))
	if err != nil {
		t.Fatalf("decodeSemanticTokens(null): %v", err)
	}
	if tokens != nil {
		t.Fatalf("expected nil tokens for null, got %#v", tokens)
	}

	tokens, err = decodeSemanticTokens(json.RawMessage(`[0,1,2,0,1]`))
	if err != nil {
		t.Fatalf("decodeSemanticTokens(array): %v", err)
	}
	if tokens == nil || len(tokens.Data) != 5 {
		t.Fatalf("unexpected tokens from array payload: %#v", tokens)
	}
}

func TestFoldingRange_RequiresFilePath(t *testing.T) {
	m := NewManager(nil)

	_, err := m.FoldingRange("")
	if err == nil {
		t.Fatal("expected file_path validation error")
	}
	if !strings.Contains(err.Error(), "file_path is required") {
		t.Fatalf("expected file_path error, got %v", err)
	}
}

func TestFoldingRangeDecode_BoundaryFilter(t *testing.T) {
	raw := json.RawMessage(`[
		{"startLine":1,"endLine":3},
		{"startLine":4,"endLine":2},
		{"startLine":2,"startCharacter":8,"endLine":2,"endCharacter":3},
		{"startLine":-1,"endLine":0}
	]`)

	ranges, err := decodeFoldingRanges(raw)
	if err != nil {
		t.Fatalf("decodeFoldingRanges: %v", err)
	}
	if len(ranges) != 1 {
		t.Fatalf("len(ranges) = %d, want 1", len(ranges))
	}
	if ranges[0].StartLine != 1 || ranges[0].EndLine != 3 {
		t.Fatalf("unexpected filtered range: %#v", ranges[0])
	}
}

func TestSemanticTools_BootstrapFailureOnUnsupportedExtension(t *testing.T) {
	m := NewManager(nil)

	_, err := m.SemanticTokens("/tmp/not-supported.md")
	if err == nil || !strings.Contains(err.Error(), "no language server configured for extension: .md") {
		t.Fatalf("SemanticTokens expected unsupported extension error, got %v", err)
	}

	_, err = m.FoldingRange("/tmp/not-supported.md")
	if err == nil || !strings.Contains(err.Error(), "no language server configured for extension: .md") {
		t.Fatalf("FoldingRange expected unsupported extension error, got %v", err)
	}
}
