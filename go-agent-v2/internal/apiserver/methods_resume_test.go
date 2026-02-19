package apiserver

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

func TestIsLikelyCodexThreadID(t *testing.T) {
	valid := "019c718c-5e83-73e1-8582-ed7f4fa04312"
	if !isLikelyCodexThreadID(valid) {
		t.Fatalf("expected valid uuid thread id")
	}
	if !isLikelyCodexThreadID("urn:uuid:" + valid) {
		t.Fatalf("expected valid urn:uuid thread id")
	}
	if isLikelyCodexThreadID("thread-1771420074563-15") {
		t.Fatalf("thread-* alias should not be treated as codex thread id")
	}
}

func TestMetadataThreadID(t *testing.T) {
	raw := json.RawMessage(`{"thread":{"id":"019c718c-5e83-73e1-8582-ed7f4fa04312"}}`)
	if got := metadataThreadID(raw); got != "019c718c-5e83-73e1-8582-ed7f4fa04312" {
		t.Fatalf("metadataThreadID = %q", got)
	}

	raw = json.RawMessage(`{"threadId":"019c718c-5e83-73e1-8582-ed7f4fa04312"}`)
	if got := metadataThreadID(raw); got != "019c718c-5e83-73e1-8582-ed7f4fa04312" {
		t.Fatalf("metadataThreadID(threadId) = %q", got)
	}
}

func TestResolveResumeThreadIDFromMessagesPrefersMeaningfulSession(t *testing.T) {
	threadA := "019c7185-68a1-7e72-9d6c-396cfa3a3ce4"
	threadB := "019c7185-6f21-7a21-bd41-a3c0ee28d6dd"
	threadC := "019c7185-7379-7822-88ca-ebe23aa1c2f7"

	// ListByAgent 返回倒序（最新在前）。
	msgs := []store.AgentMessage{
		{EventType: "codex/event/mcp_startup_update", Method: "codex/event/mcp_startup_update"},
		{EventType: "session_configured", Method: "thread/started", Metadata: mustJSON(`{"thread":{"id":"` + threadC + `"}}`)},
		{EventType: "codex/event/mcp_startup_update", Method: "codex/event/mcp_startup_update"},
		{EventType: "session_configured", Method: "thread/started", Metadata: mustJSON(`{"thread":{"id":"` + threadB + `"}}`)},
		{Role: "user", EventType: "user_message", Method: "turn/start"},
		{EventType: "session_configured", Method: "thread/started", Metadata: mustJSON(`{"thread":{"id":"` + threadA + `"}}`)},
	}

	if got := resolveResumeThreadIDFromMessages(msgs); got != threadA {
		t.Fatalf("resolveResumeThreadIDFromMessages = %q, want %q", got, threadA)
	}
}

func TestResolveResumeThreadIDFromMessagesFallbackLatestSession(t *testing.T) {
	threadA := "019c7185-68a1-7e72-9d6c-396cfa3a3ce4"
	threadB := "019c7185-6f21-7a21-bd41-a3c0ee28d6dd"

	msgs := []store.AgentMessage{
		{EventType: "codex/event/mcp_startup_update", Method: "codex/event/mcp_startup_update"},
		{EventType: "session_configured", Method: "thread/started", Metadata: mustJSON(`{"thread":{"id":"` + threadB + `"}}`)},
		{EventType: "codex/event/mcp_startup_update", Method: "codex/event/mcp_startup_update"},
		{EventType: "session_configured", Method: "thread/started", Metadata: mustJSON(`{"thread":{"id":"` + threadA + `"}}`)},
	}

	if got := resolveResumeThreadIDFromMessages(msgs); got != threadB {
		t.Fatalf("resolveResumeThreadIDFromMessages = %q, want %q", got, threadB)
	}
}

func TestResolveResumeThreadIDsFromMessagesOrdersCandidates(t *testing.T) {
	threadA := "019c7185-68a1-7e72-9d6c-396cfa3a3ce4"
	threadB := "019c7185-6f21-7a21-bd41-a3c0ee28d6dd"
	threadC := "019c7185-7379-7822-88ca-ebe23aa1c2f7"

	// 倒序：最新在前。A 为 meaningful，B/C 无 meaningful。
	msgs := []store.AgentMessage{
		{EventType: "session_configured", Method: "thread/started", Metadata: mustJSON(`{"thread":{"id":"` + threadC + `"}}`)},
		{EventType: "session_configured", Method: "thread/started", Metadata: mustJSON(`{"thread":{"id":"` + threadB + `"}}`)},
		{Role: "assistant", EventType: "agent_message", Method: "item/agentmessage/delta"},
		{EventType: "session_configured", Method: "thread/started", Metadata: mustJSON(`{"thread":{"id":"` + threadA + `"}}`)},
	}

	got := resolveResumeThreadIDsFromMessages(msgs)
	if len(got) != 3 {
		t.Fatalf("resolveResumeThreadIDsFromMessages len = %d, want 3, got=%v", len(got), got)
	}
	if got[0] != threadA {
		t.Fatalf("first candidate = %q, want %q", got[0], threadA)
	}
	if got[1] != threadC || got[2] != threadB {
		t.Fatalf("fallback order mismatch, got=%v, want=[%s %s %s]", got, threadA, threadC, threadB)
	}
}

func TestIsHistoricalResumeCandidateError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "no rollout",
			err:  errors.New("rpc error: no rollout found for thread id 019c718c-5e83-73e1-8582-ed7f4fa04312"),
			want: true,
		},
		{
			name: "load rollout failure",
			err:  errors.New("rpc error: failed to load rollout `.`: Is a directory (os error 21)"),
			want: true,
		},
		{
			name: "invalid thread id",
			err:  errors.New("rpc error: invalid thread id"),
			want: true,
		},
		{
			name: "network failure",
			err:  errors.New("websocket closed unexpectedly"),
			want: false,
		},
		{
			name: "websocket close 1006 crash",
			err:  errors.New("AppServerClient.readLoop: read message: websocket: close 1006 (abnormal closure): unexpected EOF"),
			want: true,
		},
		{
			name: "abnormal closure only",
			err:  errors.New("read: abnormal closure"),
			want: true,
		},
		{
			name: "unexpected eof in websocket context",
			err:  errors.New("AppServerClient.readLoop: read message: websocket: close 1006 (abnormal closure): unexpected EOF"),
			want: true,
		},
		// P0: standalone unexpected eof should NOT be candidate (could be transient I/O)
		{
			name: "standalone unexpected eof - not candidate",
			err:  errors.New("readLoop: unexpected eof"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isHistoricalResumeCandidateError(tc.err)
			if got != tc.want {
				t.Fatalf("isHistoricalResumeCandidateError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsCodexProcessCrashError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "websocket close 1006",
			err:  errors.New("websocket: close 1006 (abnormal closure): unexpected EOF"),
			want: true,
		},
		{
			name: "abnormal closure",
			err:  errors.New("read: abnormal closure"),
			want: true,
		},
		{
			name: "unexpected eof in websocket context",
			err:  errors.New("websocket: close 1006 (abnormal closure): unexpected EOF"),
			want: true,
		},
		// P0: standalone unexpected eof should NOT be crash error
		{
			name: "standalone unexpected eof - not crash",
			err:  errors.New("readLoop: unexpected eof"),
			want: false,
		},
		{
			name: "io.ErrUnexpectedEOF - not crash",
			err:  errors.New("read response: unexpected EOF"),
			want: false,
		},
		{
			name: "no rollout - not a crash",
			err:  errors.New("no rollout found for thread id abc"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isCodexProcessCrashError(tc.err)
			if got != tc.want {
				t.Fatalf("isCodexProcessCrashError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ========================================
// buildSessionLostNotification 测试 (TDD RED)
// ========================================

func TestBuildSessionLostNotification(t *testing.T) {
	agentID := "thread-123"
	lastErr := errors.New("websocket: close 1006 (abnormal closure)")

	method, payload := buildSessionLostNotification(agentID, lastErr)

	// Issue #3: 应使用 ui/state/changed 而非自定义 method
	if method != "ui/state/changed" {
		t.Fatalf("method = %q, want %q", method, "ui/state/changed")
	}

	// Issue #2: 应包含错误详情
	detail, ok := payload["detail"].(string)
	if !ok || detail == "" {
		t.Fatalf("payload[detail] missing or empty: %v", payload)
	}
	if detail != lastErr.Error() {
		t.Fatalf("payload[detail] = %q, want %q", detail, lastErr.Error())
	}

	// source 标识
	source, _ := payload["source"].(string)
	if source != "session_lost_warning" {
		t.Fatalf("payload[source] = %q, want %q", source, "session_lost_warning")
	}

	// agent_id 存在
	aid, _ := payload["agent_id"].(string)
	if aid != agentID {
		t.Fatalf("payload[agent_id] = %q, want %q", aid, agentID)
	}

	// warning 消息非空
	warning, _ := payload["warning"].(string)
	if warning == "" {
		t.Fatal("payload[warning] is empty")
	}
}

// ========================================
// buildResumeCandidates 测试 (TDD RED)
// ========================================

func TestBuildResumeCandidates_UUIDThread_UsesDirectly(t *testing.T) {
	uuid := "019c718c-5e83-73e1-8582-ed7f4fa04312"
	got := buildResumeCandidates(uuid, nil)
	if len(got) != 1 || got[0] != uuid {
		t.Fatalf("got %v, want [%s]", got, uuid)
	}
}

func TestBuildResumeCandidates_NonUUID_WithResolved_UsesResolved(t *testing.T) {
	resolved := []string{
		"019c7185-68a1-7e72-9d6c-396cfa3a3ce4",
		"019c7185-6f21-7a21-bd41-a3c0ee28d6dd",
	}
	got := buildResumeCandidates("thread-12345", resolved)
	if len(got) != 2 || got[0] != resolved[0] || got[1] != resolved[1] {
		t.Fatalf("got %v, want %v", got, resolved)
	}
}

func TestBuildResumeCandidates_NonUUID_NoResolved_FallsBackToOriginal(t *testing.T) {
	// 核心 bug 场景: 非 UUID 且解析失败 → 不应丢弃，应回退到原始 ID 让 codex 自行决定。
	got := buildResumeCandidates("thread-12345", nil)
	if len(got) != 1 || got[0] != "thread-12345" {
		t.Fatalf("expected fallback to original id, got %v", got)
	}
}

func TestBuildResumeCandidates_NonUUID_EmptyResolved_FallsBackToOriginal(t *testing.T) {
	got := buildResumeCandidates("thread-12345", []string{})
	if len(got) != 1 || got[0] != "thread-12345" {
		t.Fatalf("expected fallback to original id, got %v", got)
	}
}

// ========================================
// tryResumeCandidates 测试 (TDD RED)
// ========================================

func TestTryResumeCandidates_SuccessOnFirstCandidate(t *testing.T) {
	called := []string{}
	resumeFn := func(id string) error {
		called = append(called, id)
		return nil
	}
	resumedID, err := tryResumeCandidates(
		[]string{"uuid-a", "uuid-b"}, "thread-1", resumeFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resumedID != "uuid-a" {
		t.Fatalf("resumedID = %q, want uuid-a", resumedID)
	}
	if len(called) != 1 {
		t.Fatalf("called %d times, want 1", len(called))
	}
}

func TestTryResumeCandidates_RetriesOnCandidateError(t *testing.T) {
	called := []string{}
	resumeFn := func(id string) error {
		called = append(called, id)
		if id == "uuid-a" {
			return errors.New("rpc error: no rollout found for thread id uuid-a")
		}
		return nil
	}
	resumedID, err := tryResumeCandidates(
		[]string{"uuid-a", "uuid-b"}, "thread-1", resumeFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resumedID != "uuid-b" {
		t.Fatalf("resumedID = %q, want uuid-b", resumedID)
	}
	if len(called) != 2 {
		t.Fatalf("called %d times, want 2", len(called))
	}
}

func TestTryResumeCandidates_PropagatesNonCandidateError(t *testing.T) {
	resumeFn := func(id string) error {
		return errors.New("websocket closed unexpectedly")
	}
	_, err := tryResumeCandidates(
		[]string{"uuid-a"}, "thread-1", resumeFn,
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "websocket closed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTryResumeCandidates_AllExhausted_ReturnsError(t *testing.T) {
	// 所有候选都是 candidate error → 返回 error，避免伪装 resumed 成功。
	resumeFn := func(id string) error {
		return errors.New("rpc error: no rollout found for thread id " + id)
	}
	resumedID, err := tryResumeCandidates(
		[]string{"uuid-a", "uuid-b"}, "thread-1", resumeFn,
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if resumedID != "" {
		t.Fatalf("resumedID = %q, want empty on error", resumedID)
	}
	if !strings.Contains(err.Error(), "all resume candidates unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTryResumeCandidates_NoCandidates_ReturnsError(t *testing.T) {
	// 空候选列表 → 返回 error，避免 thread/resume 虚假成功。
	resumedID, err := tryResumeCandidates(nil, "thread-1", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if resumedID != "" {
		t.Fatalf("resumedID = %q, want empty on error", resumedID)
	}
	if !strings.Contains(err.Error(), "no resume candidates available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustJSON(raw string) json.RawMessage {
	return json.RawMessage(raw)
}
