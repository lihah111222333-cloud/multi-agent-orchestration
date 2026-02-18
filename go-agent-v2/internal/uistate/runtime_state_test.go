package uistate

import "testing"

func TestResolveEventFields_TextFallback(t *testing.T) {
	normalized := NormalizedEvent{Text: " from normalized "}
	payload := map[string]any{
		"uiText": "from payload",
		"delta":  "from delta",
	}

	result := resolveEventFields(normalized, payload)
	if result.text != "from normalized" {
		t.Fatalf("text = %q, want %q", result.text, "from normalized")
	}
}

func TestResolveEventFields_TextFromPayloadPriority(t *testing.T) {
	normalized := NormalizedEvent{}
	payload := map[string]any{
		"uiText":  "ui text",
		"delta":   "delta",
		"text":    "text",
		"content": "content",
	}

	result := resolveEventFields(normalized, payload)
	if result.text != "ui text" {
		t.Fatalf("text = %q, want %q", result.text, "ui text")
	}
}

func TestResolveEventFields_FilesFallback(t *testing.T) {
	normalized := NormalizedEvent{
		File:  "normalized.go",
		Files: []string{"normalized.go", "other.go"},
	}
	payload := map[string]any{}

	result := resolveEventFields(normalized, payload)
	if result.file != "normalized.go" {
		t.Fatalf("file = %q, want normalized.go", result.file)
	}
	if len(result.files) != 2 {
		t.Fatalf("files len = %d, want 2", len(result.files))
	}
}

func TestResolveEventFields_ExitCodeFallback(t *testing.T) {
	normalized := NormalizedEvent{}
	payload := map[string]any{
		"uiExitCode": float64(9),
		"exit_code":  float64(1),
	}
	result := resolveEventFields(normalized, payload)
	if result.exitCode == nil {
		t.Fatal("exitCode is nil")
	}
	if *result.exitCode != 9 {
		t.Fatalf("exitCode = %d, want 9", *result.exitCode)
	}
}

func TestCanMergeToolCall(t *testing.T) {
	elapsed := 12
	last := TimelineItem{Kind: "tool", Tool: "lsp_hover"}
	if !canMergeToolCall(last, "lsp_hover", "file.go", "preview", &elapsed) {
		t.Fatal("expected mergeable tool call")
	}
	if canMergeToolCall(last, "other_tool", "file.go", "preview", &elapsed) {
		t.Fatal("unexpected merge for different tool name")
	}
}

func TestHydrateContentPayload(t *testing.T) {
	rec := HistoryRecord{Content: "hello"}
	payload := map[string]any{
		"text": "existing",
	}
	hydrateContentPayload(rec, payload)

	if payload["text"] != "existing" {
		t.Fatalf("text overwritten: %v", payload["text"])
	}
	if payload["delta"] != "hello" {
		t.Fatalf("delta = %v, want hello", payload["delta"])
	}
	if payload["content"] != "hello" {
		t.Fatalf("content = %v, want hello", payload["content"])
	}
	if payload["output"] != "hello" {
		t.Fatalf("output = %v, want hello", payload["output"])
	}
}
