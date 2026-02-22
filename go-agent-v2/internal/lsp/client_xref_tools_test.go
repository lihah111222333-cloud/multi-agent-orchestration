package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkspaceSymbol_RequiresQuery(t *testing.T) {
	m := NewManager(nil)

	_, err := m.WorkspaceSymbol("", "go", " ")
	if err == nil {
		t.Fatal("expected query validation error")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("expected query error, got %v", err)
	}
}

func TestImplementation_RequiresFilePath(t *testing.T) {
	m := NewManager(nil)

	_, err := m.Implementation("", 0, 0)
	if err == nil {
		t.Fatal("expected file_path validation error")
	}
	if !strings.Contains(err.Error(), "file_path is required") {
		t.Fatalf("expected file_path error, got %v", err)
	}
}

func TestTypeDefinition_RequiresFilePath(t *testing.T) {
	m := NewManager(nil)

	_, err := m.TypeDefinition("", 0, 0)
	if err == nil {
		t.Fatal("expected file_path validation error")
	}
	if !strings.Contains(err.Error(), "file_path is required") {
		t.Fatalf("expected file_path error, got %v", err)
	}
}

func TestImplementationDecode_LocationLink(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"targetUri":"file:///impl.go",
			"targetRange":{"start":{"line":3,"character":1},"end":{"line":3,"character":8}},
			"targetSelectionRange":{"start":{"line":3,"character":2},"end":{"line":3,"character":7}}
		}
	]`)

	got, err := decodeLocationsLike(raw)
	if err != nil {
		t.Fatalf("decodeLocationsLike: %v", err)
	}
	if len(got) != 1 || got[0].LocationLink == nil {
		t.Fatalf("expected one location link, got %#v", got)
	}
	if got[0].LocationLink.TargetURI != "file:///impl.go" {
		t.Fatalf("unexpected target uri: %#v", got[0].LocationLink)
	}
}

func TestTypeDefinitionDecode_LocationLink(t *testing.T) {
	raw := json.RawMessage(`{
		"targetUri":"file:///types.go",
		"targetRange":{"start":{"line":10,"character":0},"end":{"line":10,"character":12}},
		"targetSelectionRange":{"start":{"line":10,"character":5},"end":{"line":10,"character":11}}
	}`)

	got, err := decodeLocationsLike(raw)
	if err != nil {
		t.Fatalf("decodeLocationsLike: %v", err)
	}
	if len(got) != 1 || got[0].LocationLink == nil {
		t.Fatalf("expected one location link, got %#v", got)
	}
	if got[0].Canonical == nil || got[0].Canonical.URI != "file:///types.go" {
		t.Fatalf("expected canonical location, got %#v", got[0].Canonical)
	}
}
