package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCallHierarchyDirection_Normalization(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "both"},
		{"incoming", "incoming"},
		{"outgoing", "outgoing"},
		{"both", "both"},
		{"  InComing  ", "incoming"},
	}

	for _, tc := range cases {
		got, err := normalizeCallHierarchyDirection(tc.in)
		if err != nil {
			t.Fatalf("normalizeCallHierarchyDirection(%q) error = %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeCallHierarchyDirection(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if _, err := normalizeCallHierarchyDirection("sideways"); err == nil {
		t.Fatal("expected invalid direction error")
	} else if !strings.Contains(err.Error(), "incoming|outgoing|both") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTypeHierarchyDirection_Normalization(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "both"},
		{"supertypes", "supertypes"},
		{"subtypes", "subtypes"},
		{"both", "both"},
		{"  SuPerTypes  ", "supertypes"},
	}

	for _, tc := range cases {
		got, err := normalizeTypeHierarchyDirection(tc.in)
		if err != nil {
			t.Fatalf("normalizeTypeHierarchyDirection(%q) error = %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeTypeHierarchyDirection(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if _, err := normalizeTypeHierarchyDirection("invalid"); err == nil {
		t.Fatal("expected invalid direction error")
	} else if !strings.Contains(err.Error(), "supertypes|subtypes|both") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallHierarchyPrepare_DecodeNullAndItems(t *testing.T) {
	items, err := decodePrepareCallHierarchyItems(json.RawMessage("null"))
	if err != nil {
		t.Fatalf("decodePrepareCallHierarchyItems(null): %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil items, got %#v", items)
	}

	raw := json.RawMessage(`[
		{"name":"Foo","kind":12,"uri":"file:///a.go","range":{"start":{"line":1,"character":0},"end":{"line":1,"character":3}},"selectionRange":{"start":{"line":1,"character":0},"end":{"line":1,"character":3}}}
	]`)
	items, err = decodePrepareCallHierarchyItems(raw)
	if err != nil {
		t.Fatalf("decodePrepareCallHierarchyItems(array): %v", err)
	}
	if len(items) != 1 || items[0].Name != "Foo" {
		t.Fatalf("unexpected prepare items: %#v", items)
	}
}

func TestTypeHierarchyPrepare_DecodeNullAndItems(t *testing.T) {
	items, err := decodePrepareTypeHierarchyItems(json.RawMessage("null"))
	if err != nil {
		t.Fatalf("decodePrepareTypeHierarchyItems(null): %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil items, got %#v", items)
	}

	raw := json.RawMessage(`[
		{"name":"Iface","kind":11,"uri":"file:///a.go","range":{"start":{"line":2,"character":0},"end":{"line":2,"character":5}},"selectionRange":{"start":{"line":2,"character":0},"end":{"line":2,"character":5}}}
	]`)
	items, err = decodePrepareTypeHierarchyItems(raw)
	if err != nil {
		t.Fatalf("decodePrepareTypeHierarchyItems(array): %v", err)
	}
	if len(items) != 1 || items[0].Name != "Iface" {
		t.Fatalf("unexpected prepare items: %#v", items)
	}
}
