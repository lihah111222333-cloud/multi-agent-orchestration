package apiserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func TestHandleClientResponse(t *testing.T) {
	ch := make(chan *Response, 1)
	s := &Server{
		pending: map[int64]chan *Response{
			42: ch,
		},
	}
	env := rpcEnvelope{
		ID:     json.RawMessage("42"),
		Result: json.RawMessage(`{"ok":true}`),
	}

	if !s.handleClientResponse(env) {
		t.Fatal("expected client response to be handled")
	}
	select {
	case resp := <-ch:
		if resp.ID != int64(42) {
			t.Fatalf("response id = %v, want 42", resp.ID)
		}
	default:
		t.Fatal("expected response sent to pending channel")
	}
}

func TestExtractToolFilePath(t *testing.T) {
	if got := extractToolFilePath(map[string]any{"path": "a.go"}); got != "a.go" {
		t.Fatalf("extractToolFilePath(path) = %q, want a.go", got)
	}
	if got := extractToolFilePath(map[string]any{}); got != "" {
		t.Fatalf("extractToolFilePath(empty) = %q, want empty", got)
	}
}

func TestBuildToolNotifyPayload(t *testing.T) {
	call := codex.DynamicToolCallData{Tool: "lsp_hover", CallID: "c1"}
	result := strings.Repeat("x", 600)
	payload := buildToolNotifyPayload("agent-1", call, map[string]any{"path": "a.go"}, "a.go", true, 3, 2*time.Second, result)

	if payload["tool"] != "lsp_hover" {
		t.Fatalf("tool = %v, want lsp_hover", payload["tool"])
	}
	preview, _ := payload["resultPreview"].(string)
	if len(preview) != 500 {
		t.Fatalf("resultPreview len = %d, want 500", len(preview))
	}
}

func TestMergePayloadFieldsKeepsTokenUsageContainers(t *testing.T) {
	payload := map[string]any{
		"threadId": "agent-1",
	}
	raw := json.RawMessage(`{
		"tokenUsage":{"total":{"totalTokens":321},"modelContextWindow":258000},
		"info":{"total_token_usage":{"total_tokens":654},"model_context_window":128000},
		"msg":{"usage":{"total":{"totalTokens":777}}}
	}`)

	mergePayloadFields(payload, raw)

	if _, ok := payload["tokenUsage"].(map[string]any); !ok {
		t.Fatalf("payload tokenUsage should be kept as object")
	}
	if _, ok := payload["info"].(map[string]any); !ok {
		t.Fatalf("payload info should be kept as object")
	}
	if _, ok := payload["usage"].(map[string]any); !ok {
		t.Fatalf("payload usage should be extracted from nested msg")
	}
}

func TestMergePayloadFieldsKeepsTurnAndLastAgentMessage(t *testing.T) {
	payload := map[string]any{
		"threadId": "agent-2",
	}
	raw := json.RawMessage(`{
		"turn":{"id":"turn-1","status":"completed","lastAgentMessage":"done"},
		"msg":{"last_agent_message":"legacy_done"}
	}`)

	mergePayloadFields(payload, raw)

	if _, ok := payload["turn"].(map[string]any); !ok {
		t.Fatalf("payload turn should be kept as object")
	}
	if got, _ := payload["last_agent_message"].(string); got != "legacy_done" {
		t.Fatalf("payload last_agent_message = %q, want legacy_done", got)
	}
}

func TestConnEntryEnqueueBackpressure(t *testing.T) {
	entry := &connEntry{
		outbox:  make(chan wsOutbound, 1),
		closeCh: make(chan struct{}),
	}
	if ok := entry.enqueue(1, []byte("a")); !ok {
		t.Fatal("expected first enqueue to succeed")
	}
	if ok := entry.enqueue(1, []byte("b")); ok {
		t.Fatal("expected enqueue to fail when queue is full")
	}
	if depth := entry.outboxDepth(); depth != 1 {
		t.Fatalf("outbox depth = %d, want 1", depth)
	}
	close(entry.closeCh)
	if ok := entry.enqueue(1, []byte("c")); ok {
		t.Fatal("expected enqueue to fail after close")
	}
}

func TestSendResponseViaOutbox_DisconnectsOverloadedConn(t *testing.T) {
	entry := &connEntry{
		outbox:  make(chan wsOutbound, 1),
		closeCh: make(chan struct{}),
	}
	entry.outbox <- wsOutbound{msgType: 1, data: []byte("busy")}

	s := &Server{
		conns: map[string]*connEntry{
			"conn-1": entry,
		},
	}
	resp := newResult(int64(1), map[string]any{"ok": true})
	if ok := s.sendResponseViaOutbox("conn-1", entry, resp, "unit_test_overloaded"); ok {
		t.Fatal("expected sendResponseViaOutbox to fail when queue is full")
	}
	s.mu.RLock()
	_, exists := s.conns["conn-1"]
	s.mu.RUnlock()
	if exists {
		t.Fatal("expected overloaded connection removed from server map")
	}
}

// TestReadLoopPanicRecovery_DisconnectsConn 验证 D10: readLoop panic 后连接被清理。
func TestReadLoopPanicRecovery_DisconnectsConn(t *testing.T) {
	entry := &connEntry{
		outbox:  make(chan wsOutbound, 1),
		closeCh: make(chan struct{}),
	}
	s := &Server{
		conns: map[string]*connEntry{
			"panic-conn": entry,
		},
	}

	// 模拟 readLoop 中发生 panic 的场景:
	// 直接调用带 recover 的闭包，验证 disconnectConn 被调用。
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 模拟 readLoop 的 recover 逻辑
		defer func() {
			if r := recover(); r != nil {
				s.disconnectConn("panic-conn")
			}
		}()
		panic("simulated handler panic")
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("panic recovery goroutine did not complete")
	}

	s.mu.RLock()
	_, exists := s.conns["panic-conn"]
	s.mu.RUnlock()
	if exists {
		t.Fatal("D10: expected panicked connection to be removed from server map")
	}
}

// TestHandleApprovalRequest_ProcNil_AutoDenies 验证 P1:
// proc==nil 时 handleApprovalRequest 通过 event.DenyFunc 自动拒绝, 防止 codex turn 挂起。
func TestHandleApprovalRequest_ProcNil_AutoDenies(t *testing.T) {
	s := &Server{
		mgr:   nil, // mgr==nil → proc 查不到
		conns: map[string]*connEntry{},
	}

	denied := false
	event := codex.Event{
		Type: "exec_approval_request",
		DenyFunc: func() error {
			denied = true
			return nil
		},
	}

	s.handleApprovalRequest("gone-agent", "item/commandExecution/requestApproval", nil, event)

	if !denied {
		t.Fatal("P1: expected DenyFunc to be called when proc is nil")
	}
}

func TestAgentEventHandlerBackfillsTurnCompletedSummaryFromTaskComplete(t *testing.T) {
	srv := &Server{
		mgr: runner.NewAgentManager(),
	}

	var completedPayload map[string]any
	srv.SetNotifyHook(func(method string, params any) {
		if method != "turn/completed" {
			return
		}
		completedPayload = util.ToMapAny(params)
	})

	handler := srv.AgentEventHandler("thread-abc")
	summary := "已完成：修复了 JSON-RPC 回调，并补充了测试。"

	handler(codex.Event{
		Type: "codex/event/task_complete",
		Data: json.RawMessage(`{
			"id":"turn_123",
			"msg":{
				"type":"task_complete",
				"turn_id":"turn_123",
				"last_agent_message":"已完成：修复了 JSON-RPC 回调，并补充了测试。"
			}
		}`),
	})
	handler(codex.Event{
		Type: "turn_complete",
		Data: json.RawMessage(`{
			"turn":{"id":"turn_123","items":[],"status":"completed","error":null}
		}`),
	})

	if completedPayload == nil {
		t.Fatal("expected turn/completed payload")
	}
	turn, _ := completedPayload["turn"].(map[string]any)
	if turn == nil {
		t.Fatalf("expected turn payload")
	}
	if got, _ := turn["lastAgentMessage"].(string); got != summary {
		t.Fatalf("turn.lastAgentMessage = %q, want %q", got, summary)
	}
	if got, _ := completedPayload["lastAgentMessage"].(string); got != summary {
		t.Fatalf("lastAgentMessage = %q, want %q", got, summary)
	}
}

func TestAgentEventHandlerTurnCompletedPreservesExplicitSummary(t *testing.T) {
	srv := &Server{
		mgr: runner.NewAgentManager(),
	}

	var completedPayload map[string]any
	srv.SetNotifyHook(func(method string, params any) {
		if method != "turn/completed" {
			return
		}
		completedPayload = util.ToMapAny(params)
	})

	handler := srv.AgentEventHandler("thread-explicit")
	handler(codex.Event{
		Type: "turn_complete",
		Data: json.RawMessage(`{
			"turn":{"id":"turn_explicit","status":"completed","lastAgentMessage":"explicit_summary"}
		}`),
	})

	turn, _ := completedPayload["turn"].(map[string]any)
	if turn == nil {
		t.Fatalf("expected turn payload")
	}
	if got, _ := turn["lastAgentMessage"].(string); got != "explicit_summary" {
		t.Fatalf("turn.lastAgentMessage = %q, want explicit_summary", got)
	}
}

func TestAgentEventHandlerTurnCompletedWithoutSummaryKeepsEmpty(t *testing.T) {
	srv := &Server{
		mgr: runner.NewAgentManager(),
	}

	var completedPayload map[string]any
	srv.SetNotifyHook(func(method string, params any) {
		if method != "turn/completed" {
			return
		}
		completedPayload = util.ToMapAny(params)
	})

	handler := srv.AgentEventHandler("thread-empty")
	handler(codex.Event{
		Type: "turn_complete",
		Data: json.RawMessage(`{
			"turn":{"id":"turn_empty","status":"completed"}
		}`),
	})

	if completedPayload == nil {
		t.Fatal("expected turn/completed payload")
	}
	turn, _ := completedPayload["turn"].(map[string]any)
	if turn == nil {
		t.Fatalf("expected turn payload")
	}
	if _, exists := turn["lastAgentMessage"]; exists {
		t.Fatalf("did not expect turn.lastAgentMessage when no summary exists")
	}
	if _, exists := completedPayload["lastAgentMessage"]; exists {
		t.Fatalf("did not expect payload lastAgentMessage when no summary exists")
	}
}

func TestAgentEventHandlerBackfillsSummaryWhenTaskCompleteMissingTurnID(t *testing.T) {
	srv := &Server{
		mgr: runner.NewAgentManager(),
	}

	var completedPayload map[string]any
	srv.SetNotifyHook(func(method string, params any) {
		if method != "turn/completed" {
			return
		}
		completedPayload = util.ToMapAny(params)
	})

	handler := srv.AgentEventHandler("thread-missing-id")
	handler(codex.Event{
		Type: "codex/event/task_complete",
		Data: json.RawMessage(`{
			"msg":{"last_agent_message":"summary_without_turn_id"}
		}`),
	})
	handler(codex.Event{
		Type: "turn_complete",
		Data: json.RawMessage(`{
			"turn":{"id":"turn_missing_id","status":"completed"}
		}`),
	})

	turn, _ := completedPayload["turn"].(map[string]any)
	if turn == nil {
		t.Fatalf("expected turn payload")
	}
	if got, _ := turn["lastAgentMessage"].(string); got != "summary_without_turn_id" {
		t.Fatalf("turn.lastAgentMessage = %q, want summary_without_turn_id", got)
	}
}

func TestAgentEventHandlerDoesNotLeakSummaryAcrossDifferentTurnID(t *testing.T) {
	srv := &Server{
		mgr: runner.NewAgentManager(),
	}

	var completedPayload map[string]any
	srv.SetNotifyHook(func(method string, params any) {
		if method != "turn/completed" {
			return
		}
		completedPayload = util.ToMapAny(params)
	})

	handler := srv.AgentEventHandler("thread-no-leak")
	handler(codex.Event{
		Type: "codex/event/task_complete",
		Data: json.RawMessage(`{
			"id":"turn_1",
			"msg":{"turn_id":"turn_1","last_agent_message":"summary_turn_1"}
		}`),
	})
	handler(codex.Event{
		Type: "turn_complete",
		Data: json.RawMessage(`{
			"turn":{"id":"turn_2","status":"completed"}
		}`),
	})

	if completedPayload == nil {
		t.Fatal("expected turn/completed payload")
	}
	turn, _ := completedPayload["turn"].(map[string]any)
	if turn == nil {
		t.Fatalf("expected turn payload")
	}
	if _, exists := turn["lastAgentMessage"]; exists {
		t.Fatalf("did not expect stale summary for different turn id")
	}
	if _, exists := completedPayload["lastAgentMessage"]; exists {
		t.Fatalf("did not expect stale payload summary for different turn id")
	}
}

func TestAgentEventHandler_StreamErrorRetryLifecycle(t *testing.T) {
	srv := &Server{
		mgr:                 runner.NewAgentManager(),
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}
	threadID := "thread-stream-retry-lifecycle"
	_ = srv.beginTrackedTurn(threadID, "turn-stream-1")

	completedCount := 0
	lastErrorPayload := map[string]any{}
	srv.SetNotifyHook(func(method string, params any) {
		switch method {
		case "turn/completed":
			completedCount++
		case "error":
			lastErrorPayload = util.ToMapAny(params)
		}
	})

	handler := srv.AgentEventHandler(threadID)
	handler(codex.Event{
		Type: codex.EventStreamError,
		Data: json.RawMessage(`{
			"message":"Reconnecting... 1/5",
			"phase":"reconnect",
			"willRetry": true
		}`),
	})

	if completedCount != 0 {
		t.Fatalf("retryable stream error should not complete turn, completedCount=%d", completedCount)
	}
	if !srv.hasActiveTrackedTurn(threadID) {
		t.Fatal("retryable stream error should keep tracked turn active")
	}
	if got, _ := lastErrorPayload["willRetry"].(bool); !got {
		t.Fatalf("error payload willRetry = %v, want true", lastErrorPayload["willRetry"])
	}

	handler(codex.Event{
		Type: codex.EventStreamError,
		Data: json.RawMessage(`{
			"message":"Reconnect failed 5/5",
			"phase":"reconnect",
			"willRetry": false
		}`),
	})

	if completedCount != 1 {
		t.Fatalf("non-retryable stream error should complete turn once, completedCount=%d", completedCount)
	}
	if srv.hasActiveTrackedTurn(threadID) {
		t.Fatal("non-retryable stream error should clear tracked turn")
	}
}
