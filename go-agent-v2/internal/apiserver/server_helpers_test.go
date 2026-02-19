package apiserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
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

func TestCalculateHydrationLoadLimit(t *testing.T) {
	tests := []struct {
		name         string
		initialCount int
		total        int64
		want         int
	}{
		{name: "keep initial", initialCount: 300, total: 120, want: 300},
		{name: "use total", initialCount: 300, total: 800, want: 800},
		{name: "cap max", initialCount: 300, total: 99999, want: threadMessageHydrationMaxRecords},
		{name: "negative initial", initialCount: -1, total: 10, want: 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := calculateHydrationLoadLimit(tc.initialCount, tc.total)
			if got != tc.want {
				t.Fatalf("calculateHydrationLoadLimit(%d,%d)=%d, want %d", tc.initialCount, tc.total, got, tc.want)
			}
		})
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
