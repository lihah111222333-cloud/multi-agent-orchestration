package lsp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceSymbolValidation_ExactlyOnePathOrLanguage(t *testing.T) {
	h := NewToolHandlers(NewManager(nil), nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "both path and language",
			args: map[string]any{"action": "workspace_symbol", "path": "main.go", "language": "go", "query": "Handler"},
		},
		{
			name: "neither path nor language",
			args: map[string]any{"action": "workspace_symbol", "query": "Handler"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			got := strings.ToLower(h.WorkspaceSymbol(raw))
			if !strings.Contains(got, "exactly one of path or language is required") {
				t.Fatalf("WorkspaceSymbol() = %q, want exact one-of validation error", got)
			}
		})
	}
}

func TestWorkspaceSymbolValidation_RejectDirectoryPathWithoutLanguage(t *testing.T) {
	h := NewToolHandlers(NewManager(nil), nil)
	dir := t.TempDir()

	raw, err := json.Marshal(map[string]any{
		"action": "workspace_symbol",
		"path":   filepath.Clean(dir),
		"query":  "Handler",
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	got := strings.ToLower(h.WorkspaceSymbol(raw))
	if !strings.Contains(got, "directory path is not supported") {
		t.Fatalf("WorkspaceSymbol() = %q, want directory-path validation error", got)
	}
}
