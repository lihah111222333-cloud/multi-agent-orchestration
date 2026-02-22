package apiserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/lsp"
)

func TestCallHierarchyToolSchema_DirectionEnum(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_call_hierarchy")
	props, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing: %#v", tool.InputSchema["properties"])
	}
	direction, ok := props["direction"].(map[string]any)
	if !ok {
		t.Fatalf("direction schema missing: %#v", props["direction"])
	}
	enumValues, ok := direction["enum"].([]string)
	if !ok || len(enumValues) != 3 {
		t.Fatalf("direction enum invalid: %#v", direction["enum"])
	}
}

func TestTypeHierarchyToolSchema_DirectionEnum(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_type_hierarchy")
	props, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing: %#v", tool.InputSchema["properties"])
	}
	direction, ok := props["direction"].(map[string]any)
	if !ok {
		t.Fatalf("direction schema missing: %#v", props["direction"])
	}
	enumValues, ok := direction["enum"].([]string)
	if !ok || len(enumValues) != 3 {
		t.Fatalf("direction enum invalid: %#v", direction["enum"])
	}
}

func TestCallHierarchyHandler_FilePathRequired(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspCallHierarchy(json.RawMessage(`{"line":1,"column":1}`))
	if !strings.Contains(result, "file_path is required") {
		t.Fatalf("expected file_path validation error, got %q", result)
	}
}

func TestTypeHierarchyHandler_FilePathRequired(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspTypeHierarchy(json.RawMessage(`{"line":1,"column":1}`))
	if !strings.Contains(result, "file_path is required") {
		t.Fatalf("expected file_path validation error, got %q", result)
	}
}

func TestCallHierarchyHandler_DirectionValidation(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspCallHierarchy(json.RawMessage(`{"file_path":"/tmp/a.go","line":0,"column":0,"direction":"sideways"}`))
	if !strings.Contains(result, "incoming|outgoing|both") {
		t.Fatalf("expected direction validation error, got %q", result)
	}
}

func TestTypeHierarchyHandler_DirectionValidation(t *testing.T) {
	s := &Server{lsp: lsp.NewManager(nil)}
	result := s.lspTypeHierarchy(json.RawMessage(`{"file_path":"/tmp/a.go","line":0,"column":0,"direction":"up"}`))
	if !strings.Contains(result, "supertypes|subtypes|both") {
		t.Fatalf("expected direction validation error, got %q", result)
	}
}
