package apiserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/lsp"
)

func TestCodeActionToolSchema_RequiresPosition(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_code_action")

	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("required schema missing: %#v", tool.InputSchema["required"])
	}
	if len(required) != 3 || required[0] != "file_path" || required[1] != "line" || required[2] != "column" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestSignatureHelpToolSchema_RequiresPosition(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_signature_help")

	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("required schema missing: %#v", tool.InputSchema["required"])
	}
	if len(required) != 3 || required[0] != "file_path" || required[1] != "line" || required[2] != "column" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestFormatToolSchema_RequiresFilePath(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_format")

	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("required schema missing: %#v", tool.InputSchema["required"])
	}
	if len(required) != 1 || required[0] != "file_path" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestCodeActionHandler_FilePathRequired(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspCodeAction(json.RawMessage(`{"line":0,"column":0}`))
	if !strings.Contains(result, "file_path is required") {
		t.Fatalf("expected file_path validation error, got %q", result)
	}
}

func TestCodeActionHandler_LineAndColumnRequired(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}

	lineMissing := s.lspCodeAction(json.RawMessage(`{"file_path":"/tmp/a.go","column":0}`))
	if !strings.Contains(lineMissing, "line is required") {
		t.Fatalf("expected line required error, got %q", lineMissing)
	}

	columnMissing := s.lspCodeAction(json.RawMessage(`{"file_path":"/tmp/a.go","line":0}`))
	if !strings.Contains(columnMissing, "column is required") {
		t.Fatalf("expected column required error, got %q", columnMissing)
	}
}

func TestCodeActionHandler_PositionMustBeNonNegative(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspCodeAction(json.RawMessage(`{"file_path":"/tmp/a.go","line":-1,"column":0}`))
	if !strings.Contains(result, "line and column must be >= 0") {
		t.Fatalf("expected non-negative position error, got %q", result)
	}
}

func TestSignatureHelpHandler_FilePathRequired(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspSignatureHelp(json.RawMessage(`{"line":0,"column":0}`))
	if !strings.Contains(result, "file_path is required") {
		t.Fatalf("expected file_path validation error, got %q", result)
	}
}

func TestSignatureHelpHandler_LineAndColumnRequired(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}

	lineMissing := s.lspSignatureHelp(json.RawMessage(`{"file_path":"/tmp/a.go","column":0}`))
	if !strings.Contains(lineMissing, "line is required") {
		t.Fatalf("expected line required error, got %q", lineMissing)
	}

	columnMissing := s.lspSignatureHelp(json.RawMessage(`{"file_path":"/tmp/a.go","line":0}`))
	if !strings.Contains(columnMissing, "column is required") {
		t.Fatalf("expected column required error, got %q", columnMissing)
	}
}

func TestSignatureHelpHandler_PositionMustBeNonNegative(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspSignatureHelp(json.RawMessage(`{"file_path":"/tmp/a.go","line":0,"column":-1}`))
	if !strings.Contains(result, "line and column must be >= 0") {
		t.Fatalf("expected non-negative position error, got %q", result)
	}
}

func TestFormatHandler_FilePathRequired(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspFormat(json.RawMessage(`{"tab_size":2}`))
	if !strings.Contains(result, "file_path is required") {
		t.Fatalf("expected file_path validation error, got %q", result)
	}
}
