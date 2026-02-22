package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodeAction_RequiresFilePath(t *testing.T) {
	m := NewManager(nil)

	_, err := m.CodeAction("", 0, 0, -1, -1, nil)
	if err == nil {
		t.Fatal("expected file_path validation error")
	}
	if !strings.Contains(err.Error(), "file_path is required") {
		t.Fatalf("expected file_path error, got %v", err)
	}
}

func TestCodeActionRange_Normalization(t *testing.T) {
	endLine, endColumn, err := normalizeCodeActionRange(2, 3, -1, -1)
	if err != nil {
		t.Fatalf("normalizeCodeActionRange default end error: %v", err)
	}
	if endLine != 2 || endColumn != 3 {
		t.Fatalf("normalizeCodeActionRange default end = (%d,%d), want (2,3)", endLine, endColumn)
	}

	_, _, err = normalizeCodeActionRange(2, 3, 1, 5)
	if err == nil {
		t.Fatal("expected invalid range error")
	}
	if !strings.Contains(err.Error(), "range end must be >= start position") {
		t.Fatalf("unexpected invalid range error: %v", err)
	}
}

func TestSignatureHelp_RequiresFilePath(t *testing.T) {
	m := NewManager(nil)

	_, err := m.SignatureHelp("", 0, 0)
	if err == nil {
		t.Fatal("expected file_path validation error")
	}
	if !strings.Contains(err.Error(), "file_path is required") {
		t.Fatalf("expected file_path error, got %v", err)
	}
}

func TestSignatureHelpDecode_DocumentationAndLabelUnion(t *testing.T) {
	raw := json.RawMessage(`{
		"signatures":[
			{
				"label":"foo(a, b)",
				"documentation":{"kind":"markdown","value":"**doc**"},
				"parameters":[
					{"label":"a","documentation":"first"},
					{"label":[5,6],"documentation":{"kind":"plaintext","value":"second"}}
				]
			}
		],
		"activeSignature":0,
		"activeParameter":1
	}`)

	result, err := decodeSignatureHelp(raw)
	if err != nil {
		t.Fatalf("decodeSignatureHelp: %v", err)
	}
	if result == nil || len(result.Signatures) != 1 {
		t.Fatalf("unexpected signatures: %#v", result)
	}

	signature := result.Signatures[0]
	if signature.Documentation != "**doc**" || signature.DocumentationKind != "markdown" {
		t.Fatalf("unexpected signature documentation: %#v", signature)
	}
	if len(signature.Parameters) != 2 {
		t.Fatalf("unexpected parameters: %#v", signature.Parameters)
	}
	if signature.Parameters[0].Label != "a" {
		t.Fatalf("expected first parameter label a, got %#v", signature.Parameters[0])
	}
	if len(signature.Parameters[1].LabelOffsets) != 2 || signature.Parameters[1].LabelOffsets[0] != 5 {
		t.Fatalf("expected second parameter label offsets, got %#v", signature.Parameters[1])
	}
}

func TestFormat_RequiresFilePath(t *testing.T) {
	m := NewManager(nil)

	_, err := m.Format("", 4, true)
	if err == nil {
		t.Fatal("expected file_path validation error")
	}
	if !strings.Contains(err.Error(), "file_path is required") {
		t.Fatalf("expected file_path error, got %v", err)
	}
}

func TestFormatDecode_TextEdits(t *testing.T) {
	edits, err := decodeTextEdits(json.RawMessage("null"))
	if err != nil {
		t.Fatalf("decodeTextEdits(null): %v", err)
	}
	if edits != nil {
		t.Fatalf("expected nil edits for null, got %#v", edits)
	}

	raw := json.RawMessage(`[
		{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":1}},"newText":"x"}
	]`)
	edits, err = decodeTextEdits(raw)
	if err != nil {
		t.Fatalf("decodeTextEdits(array): %v", err)
	}
	if len(edits) != 1 || edits[0].NewText != "x" {
		t.Fatalf("unexpected edits: %#v", edits)
	}
}

func TestActionTools_BootstrapFailureOnUnsupportedExtension(t *testing.T) {
	m := NewManager(nil)

	_, err := m.CodeAction("/tmp/not-supported.md", 0, 0, -1, -1, nil)
	if err == nil || !strings.Contains(err.Error(), "no language server configured for extension: .md") {
		t.Fatalf("CodeAction expected unsupported extension error, got %v", err)
	}

	_, err = m.SignatureHelp("/tmp/not-supported.md", 0, 0)
	if err == nil || !strings.Contains(err.Error(), "no language server configured for extension: .md") {
		t.Fatalf("SignatureHelp expected unsupported extension error, got %v", err)
	}

	_, err = m.Format("/tmp/not-supported.md", 4, true)
	if err == nil || !strings.Contains(err.Error(), "no language server configured for extension: .md") {
		t.Fatalf("Format expected unsupported extension error, got %v", err)
	}
}
