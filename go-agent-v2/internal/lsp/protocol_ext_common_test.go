package lsp

import (
	"encoding/json"
	"testing"
)

func TestDecodeLocationsLike_UnionSupportsNullSingleArray(t *testing.T) {
	t.Run("null", func(t *testing.T) {
		got, err := decodeLocationsLike(json.RawMessage("null"))
		if err != nil {
			t.Fatalf("decodeLocationsLike(null) error = %v", err)
		}
		if got != nil {
			t.Fatalf("decodeLocationsLike(null) = %#v, want nil", got)
		}
	})

	t.Run("single-location", func(t *testing.T) {
		raw := json.RawMessage(`{"uri":"file:///a.go","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":4}}}`)
		got, err := decodeLocationsLike(raw)
		if err != nil {
			t.Fatalf("decodeLocationsLike(single) error = %v", err)
		}
		if len(got) != 1 || got[0].Location == nil {
			t.Fatalf("decodeLocationsLike(single) = %#v, want one Location item", got)
		}
		if got[0].Location.URI != "file:///a.go" {
			t.Fatalf("got uri %q, want file:///a.go", got[0].Location.URI)
		}
	})

	t.Run("array-location-link", func(t *testing.T) {
		raw := json.RawMessage(`[
			{
				"targetUri":"file:///b.go",
				"targetRange":{"start":{"line":2,"character":1},"end":{"line":2,"character":7}},
				"targetSelectionRange":{"start":{"line":2,"character":3},"end":{"line":2,"character":6}}
			}
		]`)
		got, err := decodeLocationsLike(raw)
		if err != nil {
			t.Fatalf("decodeLocationsLike(array-link) error = %v", err)
		}
		if len(got) != 1 || got[0].LocationLink == nil {
			t.Fatalf("decodeLocationsLike(array-link) = %#v, want one LocationLink item", got)
		}
		if got[0].Canonical == nil || got[0].Canonical.URI != "file:///b.go" {
			t.Fatalf("canonical location not built correctly: %#v", got[0].Canonical)
		}
	})
}

func TestDecodeLocationsLike_LocationLinkPreservesFields(t *testing.T) {
	raw := json.RawMessage(`{
		"originSelectionRange":{"start":{"line":1,"character":0},"end":{"line":1,"character":5}},
		"targetUri":"file:///target.go",
		"targetRange":{"start":{"line":10,"character":1},"end":{"line":12,"character":4}},
		"targetSelectionRange":{"start":{"line":11,"character":2},"end":{"line":11,"character":8}}
	}`)

	got, err := decodeLocationsLike(raw)
	if err != nil {
		t.Fatalf("decodeLocationsLike(locationLink) error = %v", err)
	}
	if len(got) != 1 || got[0].LocationLink == nil {
		t.Fatalf("decodeLocationsLike(locationLink) = %#v, want one link", got)
	}

	link := got[0].LocationLink
	if link.TargetURI != "file:///target.go" {
		t.Fatalf("TargetURI = %q, want file:///target.go", link.TargetURI)
	}
	if link.OriginSelectionRange == nil {
		t.Fatal("OriginSelectionRange lost")
	}
	if link.TargetSelectionRange.Start.Line != 11 || link.TargetSelectionRange.Start.Character != 2 {
		t.Fatalf("TargetSelectionRange changed: %#v", link.TargetSelectionRange)
	}
	if got[0].Canonical == nil {
		t.Fatal("Canonical location missing")
	}
	if got[0].Canonical.Range.Start.Line != 11 || got[0].Canonical.Range.Start.Character != 2 {
		t.Fatalf("Canonical range should use targetSelectionRange, got %#v", got[0].Canonical.Range)
	}
}

func TestDecodeWorkspaceSymbols_Union(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"name":"Legacy",
			"kind":12,
			"location":{"uri":"file:///legacy.go","range":{"start":{"line":1,"character":1},"end":{"line":1,"character":5}}}
		},
		{
			"name":"Modern",
			"kind":12,
			"location":{"uri":"file:///modern.go"},
			"data":{"k":"v"}
		}
	]`)

	got, err := decodeWorkspaceSymbols(raw)
	if err != nil {
		t.Fatalf("decodeWorkspaceSymbols error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].SymbolInformation == nil || got[0].SymbolInformation.Name != "Legacy" {
		t.Fatalf("first symbol should be SymbolInformation, got %#v", got[0])
	}
	if got[1].WorkspaceSymbol == nil || got[1].WorkspaceSymbol.Name != "Modern" {
		t.Fatalf("second symbol should be WorkspaceSymbol, got %#v", got[1])
	}
}

func TestDecodeCodeActions_Union(t *testing.T) {
	raw := json.RawMessage(`[
		{"title":"Fix","kind":"quickfix","edit":{"changes":{"file:///a.go":[{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":1}},"newText":"x"}]}}},
		{"title":"Run command","command":"tool.run","arguments":[1,2,3]},
		{"title":"Organize Imports","command":{"title":"Run organize","command":"source.organizeImports"}},
		{"title":"Convert style","data":{"ticket":"S-1"}}
	]`)

	got, err := decodeCodeActions(raw)
	if err != nil {
		t.Fatalf("decodeCodeActions error = %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	if got[0].CodeAction == nil || got[0].CodeAction.Title != "Fix" {
		t.Fatalf("first item should be CodeAction, got %#v", got[0])
	}
	if got[1].Command == nil || got[1].Command.Command != "tool.run" {
		t.Fatalf("second item should be Command, got %#v", got[1])
	}
	if got[2].CodeAction == nil || got[2].CodeAction.Command == nil || got[2].CodeAction.Command.Command != "source.organizeImports" {
		t.Fatalf("third item should be CodeAction with object command, got %#v", got[2])
	}
	if got[3].CodeAction == nil || got[3].CodeAction.Title != "Convert style" {
		t.Fatalf("fourth item should fallback to CodeAction, got %#v", got[3])
	}
}

func TestDecodeDocumentSymbols_Union(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"name":"Modern",
			"kind":12,
			"range":{"start":{"line":2,"character":0},"end":{"line":4,"character":1}},
			"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":11}}
		},
		{
			"name":"Legacy",
			"kind":12,
			"location":{"uri":"file:///legacy.go","range":{"start":{"line":6,"character":1},"end":{"line":7,"character":3}}}
		}
	]`)

	got, err := decodeDocumentSymbols(raw)
	if err != nil {
		t.Fatalf("decodeDocumentSymbols error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "Modern" || got[1].Name != "Legacy" {
		t.Fatalf("unexpected names: %#v", got)
	}
	if got[1].Range.Start.Line != 6 || got[1].Range.Start.Character != 1 {
		t.Fatalf("legacy symbol range not converted correctly: %#v", got[1].Range)
	}
}

func TestDecodeSemanticTokensLegend_Initialize(t *testing.T) {
	provider := map[string]any{
		"legend": map[string]any{
			"tokenTypes":     []string{"type", "function"},
			"tokenModifiers": []string{"declaration"},
		},
	}
	legend := decodeSemanticTokensLegend(provider)
	if legend == nil {
		t.Fatal("decodeSemanticTokensLegend returned nil")
	}
	if len(legend.TokenTypes) != 2 || legend.TokenTypes[0] != "type" {
		t.Fatalf("unexpected tokenTypes: %#v", legend.TokenTypes)
	}
	if len(legend.TokenModifiers) != 1 || legend.TokenModifiers[0] != "declaration" {
		t.Fatalf("unexpected tokenModifiers: %#v", legend.TokenModifiers)
	}
}
