package apiserver

import (
	"encoding/json"
	"errors"
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

func mustJSON(raw string) json.RawMessage {
	return json.RawMessage(raw)
}
