package uistate

import (
	"testing"
)

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

func TestResolveEventFields_PlanSnapshot(t *testing.T) {
	normalized := NormalizedEvent{}
	payload := map[string]any{
		"plan": []any{
			map[string]any{"step": "定位任务列表渲染链路", "status": "completed"},
			map[string]any{"step": "核对本次会话工具调用日志", "status": "in_progress"},
			map[string]any{"step": "给出结论与修复建议", "status": "pending"},
		},
	}

	result := resolveEventFields(normalized, payload)
	if !result.planSet {
		t.Fatal("planSet = false, want true")
	}
	if result.planDone == nil {
		t.Fatal("planDone is nil")
	}
	if *result.planDone {
		t.Fatal("planDone = true, want false")
	}
	if result.text == "" {
		t.Fatal("text is empty, want formatted plan snapshot")
	}
	if got := result.text; got != "✓ 已完成 1/3 项任务\n1. ☑ 定位任务列表渲染链路\n2. ◐ 核对本次会话工具调用日志\n3. ○ 给出结论与修复建议" {
		t.Fatalf("unexpected plan snapshot text: %q", got)
	}
}

func TestApplyAgentEvent_PlanUpdateReplacesExistingPlan(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-test"

	firstPayload := map[string]any{
		"plan": []any{
			map[string]any{"step": "步骤A", "status": "in_progress"},
			map[string]any{"step": "步骤B", "status": "pending"},
		},
	}
	secondPayload := map[string]any{
		"plan": []any{
			map[string]any{"step": "步骤A", "status": "completed"},
			map[string]any{"step": "步骤B", "status": "completed"},
		},
	}

	normalized := NormalizedEvent{UIType: UITypePlanDelta, UIStatus: UIStatusThinking}
	mgr.ApplyAgentEvent(threadID, normalized, firstPayload)
	mgr.ApplyAgentEvent(threadID, normalized, secondPayload)

	timeline := mgr.Snapshot().TimelinesByThread[threadID]
	if len(timeline) != 1 {
		t.Fatalf("timeline len = %d, want 1", len(timeline))
	}
	item := timeline[0]
	if item.Kind != "plan" {
		t.Fatalf("kind = %q, want plan", item.Kind)
	}
	if !item.Done {
		t.Fatal("plan item should be marked done")
	}
	if got := item.Text; got != "✓ 已完成 2/2 项任务\n1. ☑ 步骤A\n2. ☑ 步骤B" {
		t.Fatalf("unexpected final plan text: %q", got)
	}
}

func TestExtractUserAttachmentsFromPayload(t *testing.T) {
	payload := map[string]any{
		"input": []any{
			map[string]any{"type": "localImage", "path": "/tmp/screen.png"},
			map[string]any{"type": "image", "url": "https://example.com/a.png"},
			map[string]any{"type": "mention", "path": "/tmp/spec.md"},
		},
	}
	attachments := extractUserAttachmentsFromPayload(payload)
	if len(attachments) != 3 {
		t.Fatalf("len(attachments) = %d, want 3", len(attachments))
	}
	if attachments[0].Kind != "image" || attachments[0].PreviewURL != "file:///tmp/screen.png" {
		t.Fatalf("attachments[0] = %+v", attachments[0])
	}
	if attachments[1].Kind != "image" || attachments[1].PreviewURL != "https://example.com/a.png" {
		t.Fatalf("attachments[1] = %+v", attachments[1])
	}
	if attachments[2].Kind != "file" || attachments[2].Path != "/tmp/spec.md" {
		t.Fatalf("attachments[2] = %+v", attachments[2])
	}
}

func TestHydrateHistory_UserAttachmentsFromMetadata(t *testing.T) {
	mgr := NewRuntimeManager()
	mgr.HydrateHistory("thread-1", []HistoryRecord{
		{
			ID:      1,
			Role:    "user",
			Content: "看图",
			Metadata: mustRawJSON(`{
				"input": [
					{"type":"localImage","path":"/tmp/screen.png"}
				]
			}`),
		},
	})

	timeline := mgr.Snapshot().TimelinesByThread["thread-1"]
	if len(timeline) != 1 {
		t.Fatalf("timeline len = %d, want 1", len(timeline))
	}
	item := timeline[0]
	if item.Kind != "user" {
		t.Fatalf("kind = %q, want user", item.Kind)
	}
	if len(item.Attachments) != 1 {
		t.Fatalf("attachments len = %d, want 1", len(item.Attachments))
	}
	if item.Attachments[0].Path != "/tmp/screen.png" {
		t.Fatalf("attachment path = %q", item.Attachments[0].Path)
	}
}

func mustRawJSON(raw string) []byte {
	return []byte(raw)
}
