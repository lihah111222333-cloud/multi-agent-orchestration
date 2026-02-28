package uistate

import "testing"

func TestNormalizeActivityToolName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain lsp",
			in:   "lsp_open_file",
			want: "lsp_open_file",
		},
		{
			name: "slash form",
			in:   "lsp/open_file",
			want: "lsp_open_file",
		},
		{
			name: "dot form",
			in:   "lsp.open_file",
			want: "lsp_open_file",
		},
		{
			name: "namespaced form",
			in:   "functions.lsp_open_file",
			want: "lsp_open_file",
		},
		{
			name: "mixed separators and case",
			in:   "  Functions:lsp-open-file  ",
			want: "lsp_open_file",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeActivityToolName(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeActivityToolName(%q)=%q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIncrActivityStatCountsLSPVariants(t *testing.T) {
	t.Parallel()

	mgr := NewRuntimeManager()
	threadID := "thread-1"
	names := []string{
		"lsp_open_file",
		"lsp/open_file",
		"lsp.open_file",
		"functions.lsp_open_file",
	}

	for _, name := range names {
		mgr.IncrActivityStat(threadID, "toolCall", name)
	}

	stats := mgr.Snapshot().ActivityStatsByThread[threadID]
	if got, want := stats.LSPCalls, int64(len(names)); got != want {
		t.Fatalf("LSPCalls=%d, want %d", got, want)
	}
	for _, name := range names {
		if got := stats.ToolCalls[name]; got != 1 {
			t.Fatalf("ToolCalls[%q]=%d, want 1", name, got)
		}
	}
}

func TestIncrActivityStatNonLSPDoesNotCount(t *testing.T) {
	t.Parallel()

	mgr := NewRuntimeManager()
	threadID := "thread-2"
	names := []string{
		"code_run",
		"functions.code_run",
		"json_render",
		"mcp__playwright__browser_click",
	}

	for _, name := range names {
		mgr.IncrActivityStat(threadID, "toolCall", name)
	}

	stats := mgr.Snapshot().ActivityStatsByThread[threadID]
	if stats.LSPCalls != 0 {
		t.Fatalf("LSPCalls=%d, want 0", stats.LSPCalls)
	}
}

func TestResolveActivityToolName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		text    string
		payload map[string]any
		want    string
	}{
		{
			name: "prefer text",
			text: "lsp_open_file",
			payload: map[string]any{
				"tool_name": "lsp_diagnostics",
			},
			want: "lsp_open_file",
		},
		{
			name: "top level tool",
			payload: map[string]any{
				"tool": "lsp_hover",
			},
			want: "lsp_hover",
		},
		{
			name: "top level tool_name",
			payload: map[string]any{
				"tool_name": "lsp_diagnostics",
			},
			want: "lsp_diagnostics",
		},
		{
			name: "nested item tool_name",
			payload: map[string]any{
				"item": map[string]any{
					"tool_name": "lsp_references",
				},
			},
			want: "lsp_references",
		},
		{
			name: "missing",
			want: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveActivityToolName(tc.text, tc.payload)
			if got != tc.want {
				t.Fatalf("resolveActivityToolName(%q, %v)=%q, want %q", tc.text, tc.payload, got, tc.want)
			}
		})
	}
}

func TestApplyUITypeDepthsToolCallUsesPayloadToolName(t *testing.T) {
	t.Parallel()

	mgr := NewRuntimeManager()
	threadID := "thread-3"

	mgr.mu.Lock()
	mgr.ensureThreadLocked(threadID)
	rt := mgr.runtime[threadID]
	mgr.applyUITypeDepthsLocked(
		threadID,
		rt,
		UITypeToolCall,
		"mcp_tool_call_begin",
		"",
		"",
		map[string]any{"tool_name": "lsp_diagnostics"},
	)
	mgr.mu.Unlock()

	stats := mgr.Snapshot().ActivityStatsByThread[threadID]
	if got := stats.ToolCalls["lsp_diagnostics"]; got != 1 {
		t.Fatalf("ToolCalls[%q]=%d, want 1", "lsp_diagnostics", got)
	}
	if got := stats.LSPCalls; got != 1 {
		t.Fatalf("LSPCalls=%d, want 1", got)
	}
}

func TestApplyAgentEventStartedStreamingTerminalOrder(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-order"

	mgr.ApplyAgentEvent(threadID, NormalizedEvent{
		UIType:  UITypeTurnStarted,
		RawType: "turn_started",
		Method:  "turn/started",
	}, map[string]any{"turnId": "turn-1"})
	snap := mgr.Snapshot()
	if got := snap.Statuses[threadID]; got != "thinking" {
		t.Fatalf("status after started = %q, want %q", got, "thinking")
	}

	mgr.ApplyAgentEvent(threadID, NormalizedEvent{
		UIType:  UITypeAssistantDelta,
		RawType: "agent_message_delta",
		Method:  "item/agentMessage/delta",
		Text:    "hello",
	}, map[string]any{"delta": "hello"})
	snap = mgr.Snapshot()
	if got := snap.Statuses[threadID]; got != "thinking" {
		t.Fatalf("status during streaming = %q, want %q", got, "thinking")
	}
	timeline := snap.TimelinesByThread[threadID]
	if len(timeline) == 0 {
		t.Fatalf("expected timeline item during streaming")
	}
	last := timeline[len(timeline)-1]
	if last.Kind != "assistant" {
		t.Fatalf("last timeline kind = %q, want %q", last.Kind, "assistant")
	}
	if last.Text != "hello" {
		t.Fatalf("assistant text = %q, want %q", last.Text, "hello")
	}

	mgr.ApplyAgentEvent(threadID, NormalizedEvent{
		UIType:  UITypeTurnComplete,
		RawType: "turn_complete",
		Method:  "turn/completed",
	}, map[string]any{"turnId": "turn-1"})
	snap = mgr.Snapshot()
	if got := snap.Statuses[threadID]; got != "idle" {
		t.Fatalf("status after terminal = %q, want %q", got, "idle")
	}
}

func TestApplyAgentEventUnknownMethodIgnored(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-unknown"

	normalized := NormalizeEvent("unknown_event_type", "unknown/method", nil)
	mgr.ApplyAgentEvent(threadID, normalized, map[string]any{"foo": "bar"})

	snap := mgr.Snapshot()
	if got := snap.Statuses[threadID]; got != "idle" {
		t.Fatalf("status for unknown method = %q, want %q", got, "idle")
	}
	if got := len(snap.TimelinesByThread[threadID]); got != 0 {
		t.Fatalf("timeline entries for unknown method = %d, want 0", got)
	}
}

func TestHydrateHistoryDeferredDuringStreaming(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-deferred"

	// Step 1: 模拟 turn 开始 + assistant 流式 delta
	mgr.ApplyAgentEvent(threadID, NormalizedEvent{
		UIType:  UITypeTurnStarted,
		RawType: "turn_started",
		Method:  "turn/started",
	}, map[string]any{"turnId": "turn-1"})

	mgr.ApplyAgentEvent(threadID, NormalizedEvent{
		UIType:  UITypeAssistantDelta,
		RawType: "agent_message_delta",
		Method:  "item/agentMessage/delta",
		Text:    "streaming...",
	}, map[string]any{"delta": "streaming..."})

	// Step 2: 在流式中调用 HydrateHistory — 应该被跳过
	records := []HistoryRecord{
		{ID: 1, Role: "user", Content: "hello from history"},
		{ID: 2, Role: "assistant", EventType: "agent_message", Content: "reply from history"},
	}
	hydrated := mgr.HydrateHistory(threadID, records)
	if hydrated {
		t.Fatalf("HydrateHistory should return false during streaming")
	}

	// 确认 timeline 仍保留流式内容（未被覆盖）
	snap := mgr.Snapshot()
	timeline := snap.TimelinesByThread[threadID]
	foundStreaming := false
	for _, item := range timeline {
		if item.Text == "streaming..." {
			foundStreaming = true
			break
		}
	}
	if !foundStreaming {
		t.Fatalf("expected streaming delta to remain in timeline during skip")
	}

	// Step 3: 确认 pendingHydration 已被设置
	mgr.mu.RLock()
	rt := mgr.runtime[threadID]
	hasPending := rt != nil && len(rt.pendingHydration) > 0
	mgr.mu.RUnlock()
	if !hasPending {
		t.Fatalf("expected pendingHydration to be set after skipped HydrateHistory")
	}

	// Step 4: 触发 turn_complete — 应触发延迟 hydration
	mgr.ApplyAgentEvent(threadID, NormalizedEvent{
		UIType:  UITypeTurnComplete,
		RawType: "turn_complete",
		Method:  "turn/completed",
	}, map[string]any{"turnId": "turn-1"})

	// Step 5: 验证 timeline 已包含 pending records 的内容
	snap = mgr.Snapshot()
	timeline = snap.TimelinesByThread[threadID]
	foundUser := false
	foundAssistant := false
	for _, item := range timeline {
		if item.Kind == "user" && item.Text == "hello from history" {
			foundUser = true
		}
		if item.Kind == "assistant" && item.Text == "reply from history" {
			foundAssistant = true
		}
	}
	if !foundUser {
		t.Fatalf("expected user message from hydration in timeline, got: %+v", timeline)
	}
	if !foundAssistant {
		t.Fatalf("expected assistant message from hydration in timeline, got: %+v", timeline)
	}

	// Step 6: 确认 pendingHydration 已被清空
	mgr.mu.RLock()
	rt = mgr.runtime[threadID]
	hasPending = rt != nil && len(rt.pendingHydration) > 0
	mgr.mu.RUnlock()
	if hasPending {
		t.Fatalf("expected pendingHydration to be cleared after replay")
	}
}
