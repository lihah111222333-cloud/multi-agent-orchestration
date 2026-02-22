package apiserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/lsp"
)

func TestSemanticTokensToolSchema_RequiresFilePath(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_semantic_tokens")

	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("required schema missing: %#v", tool.InputSchema["required"])
	}
	if len(required) != 1 || required[0] != "file_path" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestFoldingRangeToolSchema_RequiresFilePath(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_folding_range")

	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("required schema missing: %#v", tool.InputSchema["required"])
	}
	if len(required) != 1 || required[0] != "file_path" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestSemanticTokensHandler_FilePathRequired(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspSemanticTokens(json.RawMessage(`{}`))
	if !strings.Contains(result, "file_path is required") {
		t.Fatalf("expected file_path validation error, got %q", result)
	}
}

func TestFoldingRangeHandler_FilePathRequired(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspFoldingRange(json.RawMessage(`{}`))
	if !strings.Contains(result, "file_path is required") {
		t.Fatalf("expected file_path validation error, got %q", result)
	}
}
