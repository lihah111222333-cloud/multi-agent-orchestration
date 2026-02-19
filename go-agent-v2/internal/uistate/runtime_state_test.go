package uistate

import (
	"math"
	"strings"
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

	normalized := NormalizedEvent{UIType: UITypePlanDelta}
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

func TestExtractUserAttachmentsFromPayload_LocalImagePrefersURL(t *testing.T) {
	payload := map[string]any{
		"input": []any{
			map[string]any{
				"type": "localImage",
				"path": "/tmp/clipboard-2.png",
				"url":  "data:image/png;base64,BBBB",
			},
		},
	}
	attachments := extractUserAttachmentsFromPayload(payload)
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if attachments[0].Path != "/tmp/clipboard-2.png" {
		t.Fatalf("path = %q", attachments[0].Path)
	}
	if attachments[0].PreviewURL != "data:image/png;base64,BBBB" {
		t.Fatalf("previewUrl = %q", attachments[0].PreviewURL)
	}
}

func mustRawJSON(raw string) []byte {
	return []byte(raw)
}

func TestApplyAgentEvent_CommandEndDoesNotLeaveRunning(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-command"

	begin := NormalizeEvent("exec_command_begin", "", mustRawJSON(`{"command":"echo hi"}`))
	mgr.ApplyAgentEvent(threadID, begin, map[string]any{"command": "echo hi"})

	end := NormalizeEvent("exec_command_end", "", mustRawJSON(`{"exit_code":0}`))
	mgr.ApplyAgentEvent(threadID, end, map[string]any{"exit_code": 0})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "idle" {
		t.Fatalf("status after command end = %q, want idle", got)
	}
}

func TestApplyAgentEvent_ItemLifecycleCommandDoesNotLeaveRunning(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-item-command"

	begin := NormalizeEventFromPayload("item/started", "item/started", map[string]any{
		"type":    "commandExecution",
		"command": "echo hi",
	})
	mgr.ApplyAgentEvent(threadID, begin, map[string]any{
		"type":    "commandExecution",
		"command": "echo hi",
	})

	end := NormalizeEventFromPayload("item/completed", "item/completed", map[string]any{
		"type":      "commandExecution",
		"exit_code": float64(0),
	})
	mgr.ApplyAgentEvent(threadID, end, map[string]any{
		"type":      "commandExecution",
		"exit_code": 0,
	})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "idle" {
		t.Fatalf("status after item/completed command = %q, want idle", got)
	}
	items := snapshot.TimelinesByThread[threadID]
	if len(items) == 0 {
		t.Fatalf("timeline is empty, want command item")
	}
	last := items[len(items)-1]
	if last.Kind != "command" || last.Status != "completed" {
		t.Fatalf("last timeline item = %#v, want completed command", last)
	}
}

func TestApplyAgentEvent_AgentTaskCompleteStopsRunning(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-agent-task"

	start := NormalizeEvent("agent/event/task_started", "agent/event/task_started", nil)
	mgr.ApplyAgentEvent(threadID, start, map[string]any{})

	reasoning := NormalizeEvent("agent_reasoning_delta", "", mustRawJSON(`{"delta":"继续执行"}`))
	mgr.ApplyAgentEvent(threadID, reasoning, map[string]any{"delta": "继续执行"})

	complete := NormalizeEvent("agent/event/task_complete", "agent/event/task_complete", nil)
	mgr.ApplyAgentEvent(threadID, complete, map[string]any{})

	if got := mgr.Snapshot().Statuses[threadID]; got != "idle" {
		t.Fatalf("status after agent task complete = %q, want idle", got)
	}
}

func TestApplyAgentEvent_ApprovalDepthClearsOnExecutionAndTurnComplete(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-approval"

	approval := NormalizeEvent("exec_approval_request", "", mustRawJSON(`{"command":"rm -rf /tmp/x"}`))
	mgr.ApplyAgentEvent(threadID, approval, map[string]any{"command": "rm -rf /tmp/x"})
	if got := mgr.Snapshot().Statuses[threadID]; got != "waiting" {
		t.Fatalf("status after approval = %q, want waiting", got)
	}

	begin := NormalizeEvent("exec_command_begin", "", mustRawJSON(`{"command":"rm -rf /tmp/x"}`))
	mgr.ApplyAgentEvent(threadID, begin, map[string]any{"command": "rm -rf /tmp/x"})
	if got := mgr.Snapshot().Statuses[threadID]; got != "running" {
		t.Fatalf("status after command begin = %q, want running", got)
	}

	complete := NormalizeEvent("turn_complete", "", nil)
	mgr.ApplyAgentEvent(threadID, complete, map[string]any{})
	if got := mgr.Snapshot().Statuses[threadID]; got != "idle" {
		t.Fatalf("status after turn complete = %q, want idle", got)
	}
}

func TestReplaceThreadsDoesNotOverrideDerivedStatus(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-replace"

	begin := NormalizeEvent("exec_command_begin", "", mustRawJSON(`{"command":"go test"}`))
	end := NormalizeEvent("exec_command_end", "", mustRawJSON(`{"exit_code":0}`))
	mgr.ApplyAgentEvent(threadID, begin, map[string]any{"command": "go test"})
	mgr.ApplyAgentEvent(threadID, end, map[string]any{"exit_code": 0})

	mgr.ReplaceThreads([]ThreadSnapshot{
		{ID: threadID, Name: "thread-replace", State: "running"},
	})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "idle" {
		t.Fatalf("status after ReplaceThreads = %q, want idle", got)
	}
	if len(snapshot.Threads) != 1 || snapshot.Threads[0].State != "idle" {
		t.Fatalf("thread snapshot state = %#v, want idle", snapshot.Threads)
	}
}

func TestReplaceThreadsSetsDefaultIdleHeader(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-default-header"

	mgr.ReplaceThreads([]ThreadSnapshot{
		{ID: threadID, Name: "thread-default-header", State: "idle"},
	})

	snapshot := mgr.Snapshot()
	if got := snapshot.StatusHeadersByThread[threadID]; got != "等待指示" {
		t.Fatalf("default header for idle thread = %q, want 等待指示", got)
	}
}

func TestTurnStartedUsesWorkingHeader(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-turn-start"

	start := NormalizeEvent("turn_started", "", nil)
	mgr.ApplyAgentEvent(threadID, start, map[string]any{})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "thinking" {
		t.Fatalf("status after turn_started = %q, want thinking", got)
	}
	if got := snapshot.StatusHeadersByThread[threadID]; got != "工作中" {
		t.Fatalf("header after turn_started = %q, want 工作中", got)
	}
}

func TestLifecycleOverlays_TerminalAndMCP(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-overlay"

	terminal := NormalizeEvent(
		"item/commandExecution/terminalInteraction",
		"item/commandExecution/terminalInteraction",
		mustRawJSON(`{"stdin":"","command":"tail -f app.log"}`),
	)
	mgr.ApplyAgentEvent(threadID, terminal, map[string]any{
		"stdin":   "",
		"command": "tail -f app.log",
	})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "waiting" {
		t.Fatalf("status after terminal wait = %q, want waiting", got)
	}
	header := snapshot.StatusHeadersByThread[threadID]
	if !strings.Contains(header, "等待后台终端") {
		t.Fatalf("terminal wait header = %q, want contain 等待后台终端", header)
	}

	output := NormalizeEvent("exec_command_output_delta", "", mustRawJSON(`{"output":"line"}`))
	mgr.ApplyAgentEvent(threadID, output, map[string]any{"output": "line"})
	if got := mgr.Snapshot().Statuses[threadID]; got == "waiting" {
		t.Fatalf("status after command output = %q, want non-waiting", got)
	}
	commandEnd := NormalizeEvent("exec_command_end", "", mustRawJSON(`{"exit_code":0}`))
	mgr.ApplyAgentEvent(threadID, commandEnd, map[string]any{"exit_code": 0})

	mcpUpdate := NormalizeEvent(
		"mcp_startup_update",
		"codex/event/mcp_startup_update",
		mustRawJSON(`{"server":"filesystem"}`),
	)
	mgr.ApplyAgentEvent(threadID, mcpUpdate, map[string]any{"server": "filesystem"})
	snapshot = mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "syncing" {
		t.Fatalf("status after mcp startup update = %q, want syncing", got)
	}
	if header := snapshot.StatusHeadersByThread[threadID]; !strings.Contains(header, "MCP 启动中") {
		t.Fatalf("mcp startup header = %q, want contain MCP 启动中", header)
	}

	mcpComplete := NormalizeEvent("mcp_startup_complete", "", nil)
	mgr.ApplyAgentEvent(threadID, mcpComplete, map[string]any{})
	if got := mgr.Snapshot().Statuses[threadID]; got != "idle" {
		t.Fatalf("status after mcp startup complete = %q, want idle", got)
	}
}

func TestDynamicToolCallDoesNotIncreaseRunningDepth(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-dynamic-tool"

	ev := NormalizeEvent("dynamic_tool_call", "", mustRawJSON(`{"tool":"lsp_hover","file":"main.go"}`))
	mgr.ApplyAgentEvent(threadID, ev, map[string]any{
		"tool": "lsp_hover",
		"file": "main.go",
	})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "idle" {
		t.Fatalf("status after dynamic_tool_call = %q, want idle", got)
	}
}

func TestTokenUsageUpdatesAndKeepsLastLimit(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-token"

	first := NormalizeEvent("token_count", "thread/tokenUsage/updated", mustRawJSON(`{"input":1200,"output":300,"context_window_tokens":10000}`))
	mgr.ApplyAgentEvent(threadID, first, map[string]any{
		"input":                 1200,
		"output":                300,
		"context_window_tokens": 10000,
	})

	snapshot := mgr.Snapshot()
	usage := snapshot.TokenUsageByThread[threadID]
	if usage.UsedTokens != 1500 {
		t.Fatalf("used tokens = %d, want 1500", usage.UsedTokens)
	}
	if usage.ContextWindowTokens != 10000 {
		t.Fatalf("context window tokens = %d, want 10000", usage.ContextWindowTokens)
	}
	if math.Abs(usage.UsedPercent-15) > 0.001 {
		t.Fatalf("used percent = %f, want 15", usage.UsedPercent)
	}

	second := NormalizeEvent("token_count", "thread/tokenUsage/updated", mustRawJSON(`{"input":2000,"output":500}`))
	mgr.ApplyAgentEvent(threadID, second, map[string]any{
		"input":  2000,
		"output": 500,
	})

	snapshot = mgr.Snapshot()
	usage = snapshot.TokenUsageByThread[threadID]
	if usage.UsedTokens != 2500 {
		t.Fatalf("used tokens after second update = %d, want 2500", usage.UsedTokens)
	}
	if usage.ContextWindowTokens != 10000 {
		t.Fatalf("context window tokens after second update = %d, want 10000", usage.ContextWindowTokens)
	}
}

func TestTokenUsageUpdatesFromThreadTokenUsageShape(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-token-v2"

	event := NormalizeEvent(
		"token_count",
		"thread/tokenUsage/updated",
		mustRawJSON(`{"tokenUsage":{"total":{"totalTokens":3200,"inputTokens":2500,"outputTokens":700},"modelContextWindow":200000}}`),
	)
	mgr.ApplyAgentEvent(threadID, event, map[string]any{
		"tokenUsage": map[string]any{
			"total": map[string]any{
				"totalTokens":  3200,
				"inputTokens":  2500,
				"outputTokens": 700,
			},
			"modelContextWindow": 200000,
		},
	})

	usage := mgr.Snapshot().TokenUsageByThread[threadID]
	if usage.UsedTokens != 3200 {
		t.Fatalf("used tokens = %d, want 3200", usage.UsedTokens)
	}
	if usage.ContextWindowTokens != 200000 {
		t.Fatalf("context window tokens = %d, want 200000", usage.ContextWindowTokens)
	}
}

func TestTokenUsageUpdatesFromTokenCountInfoShape(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-token-info"

	event := NormalizeEvent(
		"token_count",
		"codex/event/token_count",
		mustRawJSON(`{"info":{"total_token_usage":{"total_tokens":1800,"input_tokens":1400,"output_tokens":400},"model_context_window":128000}}`),
	)
	mgr.ApplyAgentEvent(threadID, event, map[string]any{
		"info": map[string]any{
			"total_token_usage": map[string]any{
				"total_tokens":  1800,
				"input_tokens":  1400,
				"output_tokens": 400,
			},
			"model_context_window": 128000,
		},
	})

	usage := mgr.Snapshot().TokenUsageByThread[threadID]
	if usage.UsedTokens != 1800 {
		t.Fatalf("used tokens = %d, want 1800", usage.UsedTokens)
	}
	if usage.ContextWindowTokens != 128000 {
		t.Fatalf("context window tokens = %d, want 128000", usage.ContextWindowTokens)
	}
}

func TestTokenUsageIgnoresOversizedInfoTotalWhenThreadUsageExists(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-token-oversized-info"

	threadEvent := NormalizeEvent(
		"token_count",
		"thread/tokenUsage/updated",
		mustRawJSON(`{"tokenUsage":{"total":{"totalTokens":119000},"modelContextWindow":258000}}`),
	)
	mgr.ApplyAgentEvent(threadID, threadEvent, map[string]any{
		"tokenUsage": map[string]any{
			"total": map[string]any{
				"totalTokens": 119000,
			},
			"modelContextWindow": 258000,
		},
	})

	infoEvent := NormalizeEvent(
		"token_count",
		"codex/event/token_count",
		mustRawJSON(`{"info":{"total_token_usage":{"total_tokens":40000000},"model_context_window":258000}}`),
	)
	mgr.ApplyAgentEvent(threadID, infoEvent, map[string]any{
		"info": map[string]any{
			"total_token_usage": map[string]any{
				"total_tokens": 40000000,
			},
			"model_context_window": 258000,
		},
	})

	usage := mgr.Snapshot().TokenUsageByThread[threadID]
	if usage.UsedTokens != 119000 {
		t.Fatalf("used tokens = %d, want 119000", usage.UsedTokens)
	}
	if usage.ContextWindowTokens != 258000 {
		t.Fatalf("context window tokens = %d, want 258000", usage.ContextWindowTokens)
	}
	if math.Abs(usage.UsedPercent-46.124031) > 0.01 {
		t.Fatalf("used percent = %f, want around 46.12", usage.UsedPercent)
	}
}

func TestTokenUsagePrefersInfoLastUsageOverInfoTotalUsage(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-token-info-last-preferred"

	event := NormalizeEvent(
		"token_count",
		"codex/event/token_count",
		mustRawJSON(`{"info":{"total_token_usage":{"total_tokens":40900000},"last_token_usage":{"total_tokens":119000},"model_context_window":258000}}`),
	)
	mgr.ApplyAgentEvent(threadID, event, map[string]any{
		"info": map[string]any{
			"total_token_usage": map[string]any{
				"total_tokens": 40900000,
			},
			"last_token_usage": map[string]any{
				"total_tokens": 119000,
			},
			"model_context_window": 258000,
		},
	})

	usage := mgr.Snapshot().TokenUsageByThread[threadID]
	if usage.UsedTokens != 119000 {
		t.Fatalf("used tokens = %d, want 119000", usage.UsedTokens)
	}
	if usage.ContextWindowTokens != 258000 {
		t.Fatalf("context window tokens = %d, want 258000", usage.ContextWindowTokens)
	}
	if math.Abs(usage.UsedPercent-46.124031) > 0.01 {
		t.Fatalf("used percent = %f, want around 46.12", usage.UsedPercent)
	}
}

func TestTokenUsageDropsOversizedInfoTotalWithoutFallback(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-token-info-total-only"

	event := NormalizeEvent(
		"token_count",
		"codex/event/token_count",
		mustRawJSON(`{"info":{"total_token_usage":{"total_tokens":40900000},"model_context_window":258000}}`),
	)
	mgr.ApplyAgentEvent(threadID, event, map[string]any{
		"info": map[string]any{
			"total_token_usage": map[string]any{
				"total_tokens": 40900000,
			},
			"model_context_window": 258000,
		},
	})

	usage := mgr.Snapshot().TokenUsageByThread[threadID]
	if usage.UsedTokens != 0 {
		t.Fatalf("used tokens = %d, want 0", usage.UsedTokens)
	}
	if usage.ContextWindowTokens != 258000 {
		t.Fatalf("context window tokens = %d, want 258000", usage.ContextWindowTokens)
	}
	if usage.UsedPercent != 0 {
		t.Fatalf("used percent = %f, want 0", usage.UsedPercent)
	}
}

func TestTokenUsageDropsOversizedInfoInputOutputWithoutFallback(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-token-info-io-only"

	event := NormalizeEvent(
		"token_count",
		"codex/event/token_count",
		mustRawJSON(`{"info":{"total_token_usage":{"input_tokens":30000000,"output_tokens":10900000},"model_context_window":258000}}`),
	)
	mgr.ApplyAgentEvent(threadID, event, map[string]any{
		"info": map[string]any{
			"total_token_usage": map[string]any{
				"input_tokens":  30000000,
				"output_tokens": 10900000,
			},
			"model_context_window": 258000,
		},
	})

	usage := mgr.Snapshot().TokenUsageByThread[threadID]
	if usage.UsedTokens != 0 {
		t.Fatalf("used tokens = %d, want 0", usage.UsedTokens)
	}
	if usage.ContextWindowTokens != 258000 {
		t.Fatalf("context window tokens = %d, want 258000", usage.ContextWindowTokens)
	}
	if usage.UsedPercent != 0 {
		t.Fatalf("used percent = %f, want 0", usage.UsedPercent)
	}
}

func TestApplyAgentEvent_PatchApplyEndDoesNotLeaveEditing(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-patch"

	begin := NormalizeEvent("patch_apply_begin", "", mustRawJSON(`{"file":"main.go"}`))
	mgr.ApplyAgentEvent(threadID, begin, map[string]any{"file": "main.go"})

	end := NormalizeEvent("patch_apply_end", "", mustRawJSON(`{"file":"main.go"}`))
	mgr.ApplyAgentEvent(threadID, end, map[string]any{"file": "main.go"})

	if got := mgr.Snapshot().Statuses[threadID]; got != "idle" {
		t.Fatalf("status after patch_apply_end = %q, want idle", got)
	}
}

func TestReasoningHeaderOverlayAndReset(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-reasoning-header"

	start := NormalizeEvent("turn_started", "", nil)
	mgr.ApplyAgentEvent(threadID, start, map[string]any{})

	reasoning := NormalizeEvent("agent_reasoning_delta", "", mustRawJSON(`{"delta":"**分析需求** 先梳理现状"}`))
	mgr.ApplyAgentEvent(threadID, reasoning, map[string]any{"delta": "**分析需求** 先梳理现状"})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "thinking" {
		t.Fatalf("status after reasoning = %q, want thinking", got)
	}
	if got := snapshot.StatusHeadersByThread[threadID]; got != "分析需求" {
		t.Fatalf("reasoning header = %q, want 分析需求", got)
	}

	commandBegin := NormalizeEvent("exec_command_begin", "", mustRawJSON(`{"command":"echo hi"}`))
	mgr.ApplyAgentEvent(threadID, commandBegin, map[string]any{"command": "echo hi"})
	if got := mgr.Snapshot().StatusHeadersByThread[threadID]; got != "工作中" {
		t.Fatalf("header while command running = %q, want 工作中", got)
	}

	complete := NormalizeEvent("turn_complete", "", nil)
	mgr.ApplyAgentEvent(threadID, complete, map[string]any{})
	if got := mgr.Snapshot().StatusHeadersByThread[threadID]; got != "等待指示" {
		t.Fatalf("header after turn complete = %q, want 等待指示", got)
	}
}

func TestReasoningSectionBreakAllowsHeaderRefresh(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-reasoning-break"

	start := NormalizeEvent("turn_started", "", nil)
	mgr.ApplyAgentEvent(threadID, start, map[string]any{})

	first := NormalizeEvent("agent_reasoning_delta", "", mustRawJSON(`{"delta":"**分析问题** 先看现状"}`))
	mgr.ApplyAgentEvent(threadID, first, map[string]any{"delta": "**分析问题** 先看现状"})
	if got := mgr.Snapshot().StatusHeadersByThread[threadID]; got != "分析问题" {
		t.Fatalf("first reasoning header = %q, want 分析问题", got)
	}

	breakEvent := NormalizeEvent("agent_reasoning_section_break", "", mustRawJSON(`{"delta":"\\n\\n"}`))
	mgr.ApplyAgentEvent(threadID, breakEvent, map[string]any{"delta": "\n\n"})

	second := NormalizeEvent("agent_reasoning_delta", "", mustRawJSON(`{"delta":"**实现修复** 开始改代码"}`))
	mgr.ApplyAgentEvent(threadID, second, map[string]any{"delta": "**实现修复** 开始改代码"})
	if got := mgr.Snapshot().StatusHeadersByThread[threadID]; got != "实现修复" {
		t.Fatalf("second reasoning header = %q, want 实现修复", got)
	}
}

func TestBackgroundOverlaySetAndClear(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-background"

	background := NormalizeEvent("background_event", "", mustRawJSON(`{"message":"索引仓库中"}`))
	mgr.ApplyAgentEvent(threadID, background, map[string]any{"message": "索引仓库中"})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "idle" {
		t.Fatalf("status after background_event = %q, want idle", got)
	}
	if header := snapshot.StatusHeadersByThread[threadID]; !strings.Contains(header, "后台处理中") {
		t.Fatalf("background header = %q, want contain 后台处理中", header)
	}
	if details := snapshot.StatusDetailsByThread[threadID]; details == "" {
		t.Fatal("background details should not be empty")
	}

	clear := NormalizeEvent("background_event", "", mustRawJSON(`{"status":"completed"}`))
	mgr.ApplyAgentEvent(threadID, clear, map[string]any{"status": "completed"})
	if got := mgr.Snapshot().Statuses[threadID]; got != "idle" {
		t.Fatalf("status after completed background_event = %q, want idle", got)
	}
}

func TestMCPStartupPersistsAcrossTurnLifecycle(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-mcp-lifecycle"

	mcpUpdate := NormalizeEvent(
		"mcp_startup_update",
		"codex/event/mcp_startup_update",
		mustRawJSON(`{"server":"filesystem"}`),
	)
	mgr.ApplyAgentEvent(threadID, mcpUpdate, map[string]any{"server": "filesystem"})

	if got := mgr.Snapshot().Statuses[threadID]; got != "syncing" {
		t.Fatalf("status after mcp_startup_update = %q, want syncing", got)
	}

	turnStart := NormalizeEvent("turn_started", "", nil)
	mgr.ApplyAgentEvent(threadID, turnStart, map[string]any{})
	if got := mgr.Snapshot().Statuses[threadID]; got != "thinking" {
		t.Fatalf("status after turn_started = %q, want thinking", got)
	}

	turnComplete := NormalizeEvent("turn_complete", "", nil)
	mgr.ApplyAgentEvent(threadID, turnComplete, map[string]any{})
	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "syncing" {
		t.Fatalf("status after turn_complete with MCP active = %q, want syncing", got)
	}
	if header := snapshot.StatusHeadersByThread[threadID]; !strings.Contains(header, "MCP 启动中") {
		t.Fatalf("header after turn_complete with MCP active = %q, want contain MCP 启动中", header)
	}

	mcpComplete := NormalizeEvent("mcp_startup_complete", "", nil)
	mgr.ApplyAgentEvent(threadID, mcpComplete, map[string]any{})
	if got := mgr.Snapshot().Statuses[threadID]; got != "idle" {
		t.Fatalf("status after mcp_startup_complete = %q, want idle", got)
	}
}

func TestStreamErrorOverlayRecoversOnNextLifecycleEvent(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-stream-error"

	errEvent := NormalizeEvent("stream_error", "", mustRawJSON(`{"message":"连接中断"}`))
	mgr.ApplyAgentEvent(threadID, errEvent, map[string]any{"message": "连接中断"})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "error" {
		t.Fatalf("status after stream_error = %q, want error", got)
	}
	if header := snapshot.StatusHeadersByThread[threadID]; !strings.Contains(header, "连接中断") {
		t.Fatalf("error header = %q, want contain 连接中断", header)
	}

	reasoning := NormalizeEvent("agent_reasoning_delta", "", mustRawJSON(`{"delta":"继续执行"}`))
	mgr.ApplyAgentEvent(threadID, reasoning, map[string]any{"delta": "继续执行"})

	snapshot = mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "thinking" {
		t.Fatalf("status after recovery event = %q, want thinking", got)
	}
	if got := snapshot.StatusHeadersByThread[threadID]; got == "连接中断" {
		t.Fatalf("error header should be cleared, got %q", got)
	}
}

func TestStreamErrorUsesAdditionalDetails(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-stream-error-details"

	errEvent := NormalizeEvent(
		"stream_error",
		"",
		mustRawJSON(`{"message":"连接中断","additional_details":"网络请求超时，请检查代理配置"}`),
	)
	mgr.ApplyAgentEvent(threadID, errEvent, map[string]any{
		"message":            "连接中断",
		"additional_details": "网络请求超时，请检查代理配置",
	})

	snapshot := mgr.Snapshot()
	if got := snapshot.Statuses[threadID]; got != "error" {
		t.Fatalf("status after stream_error = %q, want error", got)
	}
	if got := snapshot.StatusDetailsByThread[threadID]; got != "网络请求超时，请检查代理配置" {
		t.Fatalf("status details after stream_error = %q, want additional_details", got)
	}

	reasoning := NormalizeEvent("agent_reasoning_delta", "", mustRawJSON(`{"delta":"继续执行"}`))
	mgr.ApplyAgentEvent(threadID, reasoning, map[string]any{"delta": "继续执行"})

	snapshot = mgr.Snapshot()
	if got := snapshot.StatusDetailsByThread[threadID]; got != "模型推理中" {
		t.Fatalf("status details after recovery = %q, want 模型推理中", got)
	}
}

func TestAppendHistory_AccumulatesRecords(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-append"

	// 初始 hydrate 第一页 (最新的两条 user 消息)
	mgr.HydrateHistory(threadID, []HistoryRecord{
		{ID: 3, Role: "user", Content: "第三条"},
		{ID: 4, Role: "user", Content: "第四条"},
	})

	timeline := mgr.Snapshot().TimelinesByThread[threadID]
	if len(timeline) != 2 {
		t.Fatalf("after HydrateHistory: timeline len = %d, want 2", len(timeline))
	}

	// 追加第二页 (更早的两条)
	mgr.AppendHistory(threadID, []HistoryRecord{
		{ID: 1, Role: "user", Content: "第一条"},
		{ID: 2, Role: "user", Content: "第二条"},
	})

	timeline = mgr.Snapshot().TimelinesByThread[threadID]
	if len(timeline) != 4 {
		t.Fatalf("after AppendHistory: timeline len = %d, want 4", len(timeline))
	}
	// 验证顺序: HydrateHistory 的两条在前, AppendHistory 的两条追加在后
	if timeline[0].Text != "第三条" {
		t.Fatalf("timeline[0].Text = %q, want 第三条", timeline[0].Text)
	}
	if timeline[1].Text != "第四条" {
		t.Fatalf("timeline[1].Text = %q, want 第四条", timeline[1].Text)
	}
	if timeline[2].Text != "第一条" {
		t.Fatalf("timeline[2].Text = %q, want 第一条", timeline[2].Text)
	}
	if timeline[3].Text != "第二条" {
		t.Fatalf("timeline[3].Text = %q, want 第二条", timeline[3].Text)
	}
}

func TestAppendHistory_EmptyRecordsNoOp(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-append-empty"

	mgr.HydrateHistory(threadID, []HistoryRecord{
		{ID: 1, Role: "user", Content: "hello"},
	})

	mgr.AppendHistory(threadID, []HistoryRecord{})

	timeline := mgr.Snapshot().TimelinesByThread[threadID]
	if len(timeline) != 1 {
		t.Fatalf("timeline len = %d, want 1 (unchanged)", len(timeline))
	}
}
