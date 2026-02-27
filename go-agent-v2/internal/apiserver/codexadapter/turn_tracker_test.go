// turn_tracker_test.go — turn tracker payload 解析纯函数的行为基线测试。
package codexadapter

import (
	"testing"

	trackersvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/tracker"
)

// ========================================
// normalizeTrackedTurnStatus
// ========================================

func TestNormalizeTrackedTurnStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   string
	}{
		{"completed", "completed", "completed"},
		{"complete", "Complete", "completed"},
		{"done", "DONE", "completed"},
		{"success", "success", "completed"},
		{"succeeded", "Succeeded", "completed"},
		{"interrupted", "interrupted", "interrupted"},
		{"cancelled", "cancelled", "interrupted"},
		{"canceled", "canceled", "interrupted"},
		{"aborted", "ABORTED", "interrupted"},
		{"failed", "failed", "failed"},
		{"error", "Error", "failed"},
		{"timeout", "TIMEOUT", "failed"},
		{"empty", "", "completed"},
		{"unknown", "foobar", "foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trackersvc.NormalizeTrackedTurnStatus(tt.status)
			if got != tt.want {
				t.Errorf("normalizeTrackedTurnStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// ========================================
// threadStatusTerminalFromPayload
// ========================================

func TestThreadStatusTerminalFromPayload(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]any
		wStatus  string
		wReason  string
		terminal bool
	}{
		{"nil", nil, "", "", false},
		{"no_status", map[string]any{}, "", "", false},
		{"idle", map[string]any{"status": "idle"}, "completed", "thread_status_idle", true},
		{"system_error", map[string]any{"status": "systemError"}, "failed", "thread_status_system_error", true},
		{"system_error_snake", map[string]any{"status": "system_error"}, "failed", "thread_status_system_error", true},
		{"error", map[string]any{"status": "error"}, "failed", "thread_status_system_error", true},
		{"not_loaded", map[string]any{"status": "notLoaded"}, "failed", "thread_status_not_loaded", true},
		{"running", map[string]any{"status": "running"}, "", "", false},
		{"status_map", map[string]any{"status": map[string]any{"type": "idle"}}, "completed", "thread_status_idle", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, reason, terminal := trackersvc.ThreadStatusTerminalFromPayload(tt.payload)
			if status != tt.wStatus || reason != tt.wReason || terminal != tt.terminal {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)",
					status, reason, terminal, tt.wStatus, tt.wReason, tt.terminal)
			}
		})
	}
}

// ========================================
// trackedTurnSummaryFromPayload
// ========================================

func TestTrackedTurnSummaryFromPayload(t *testing.T) {
	if got := trackersvc.TrackedTurnSummaryFromPayload(nil); got != "" {
		t.Errorf("nil: got %q", got)
	}
	p := map[string]any{"lastAgentMessage": "hello"}
	if got := trackersvc.TrackedTurnSummaryFromPayload(p); got != "hello" {
		t.Errorf("top-level: got %q, want 'hello'", got)
	}
	p = map[string]any{"turn": map[string]any{"last_agent_message": "nested"}}
	if got := trackersvc.TrackedTurnSummaryFromPayload(p); got != "nested" {
		t.Errorf("nested turn: got %q, want 'nested'", got)
	}
	p = map[string]any{"msg": map[string]any{"lastAgentMessage": "msg_nested"}}
	if got := trackersvc.TrackedTurnSummaryFromPayload(p); got != "msg_nested" {
		t.Errorf("nested msg: got %q, want 'msg_nested'", got)
	}
}

// ========================================
// extractTrackedTurnID / Status / Reason
// ========================================

func TestExtractTrackedTurnID(t *testing.T) {
	if got := trackersvc.ExtractTrackedTurnID(nil); got != "" {
		t.Errorf("nil: got %q", got)
	}
	p := map[string]any{"turn": map[string]any{"id": "turn-123"}}
	if got := trackersvc.ExtractTrackedTurnID(p); got != "turn-123" {
		t.Errorf("turn.id: got %q, want 'turn-123'", got)
	}
	p = map[string]any{"turnId": "turn-456"}
	if got := trackersvc.ExtractTrackedTurnID(p); got != "turn-456" {
		t.Errorf("turnId: got %q, want 'turn-456'", got)
	}
}

func TestExtractTrackedTurnStatus(t *testing.T) {
	p := map[string]any{"turn": map[string]any{"status": "completed"}}
	if got := trackersvc.ExtractTrackedTurnStatus(p); got != "completed" {
		t.Errorf("turn.status: got %q", got)
	}
	p = map[string]any{"status": "failed"}
	if got := trackersvc.ExtractTrackedTurnStatus(p); got != "failed" {
		t.Errorf("status: got %q", got)
	}
}

func TestExtractTrackedTurnReason(t *testing.T) {
	p := map[string]any{"turn": map[string]any{"reason": "timeout"}}
	if got := trackersvc.ExtractTrackedTurnReason(p); got != "timeout" {
		t.Errorf("turn.reason: got %q", got)
	}
	p = map[string]any{"message": "some error"}
	if got := trackersvc.ExtractTrackedTurnReason(p); got != "some error" {
		t.Errorf("message: got %q", got)
	}
}

// ========================================
// extractTrackedString
// ========================================

func TestExtractTrackedString(t *testing.T) {
	if got := trackersvc.ExtractTrackedString(nil, "a"); got != "" {
		t.Errorf("nil: got %q", got)
	}
	p := map[string]any{"a": "hello", "b": 123}
	if got := trackersvc.ExtractTrackedString(p, "a"); got != "hello" {
		t.Errorf("a: got %q", got)
	}
	if got := trackersvc.ExtractTrackedString(p, "b"); got != "" {
		t.Errorf("non-string: got %q", got)
	}
	if got := trackersvc.ExtractTrackedString(p, "x", "a"); got != "hello" {
		t.Errorf("fallback: got %q", got)
	}
}

// ========================================
// mergeTrackedTurnCompletionPayload
// ========================================

func TestMergeTrackedTurnCompletionPayload(t *testing.T) {
	trackersvc.MergeTrackedTurnCompletionPayload(nil, map[string]any{"a": 1})
	trackersvc.MergeTrackedTurnCompletionPayload(map[string]any{}, nil)

	payload := map[string]any{"status": "running"}
	completion := map[string]any{"status": "completed", "reason": "done"}
	trackersvc.MergeTrackedTurnCompletionPayload(payload, completion)
	if payload["status"] != "completed" {
		t.Errorf("status: got %v, want 'completed'", payload["status"])
	}
	if payload["reason"] != "done" {
		t.Errorf("reason: got %v, want 'done'", payload["reason"])
	}

	payload = map[string]any{
		"turn": map[string]any{"id": "t1", "status": "running"},
	}
	completion = map[string]any{
		"turn": map[string]any{"status": "completed", "reason": "done"},
	}
	trackersvc.MergeTrackedTurnCompletionPayload(payload, completion)
	turnObj := payload["turn"].(map[string]any)
	if turnObj["id"] != "t1" {
		t.Errorf("turn.id preserved: got %v", turnObj["id"])
	}
	if turnObj["status"] != "completed" {
		t.Errorf("turn.status: got %v", turnObj["status"])
	}
	if turnObj["reason"] != "done" {
		t.Errorf("turn.reason: got %v", turnObj["reason"])
	}
}

// ========================================
// injectTrackedTurnSummary
// ========================================

func TestInjectTrackedTurnSummary(t *testing.T) {
	trackersvc.InjectTrackedTurnSummary(nil, "msg")

	p := map[string]any{}
	trackersvc.InjectTrackedTurnSummary(p, "")
	if _, ok := p["lastAgentMessage"]; ok {
		t.Error("empty summary should not inject")
	}

	p = map[string]any{}
	trackersvc.InjectTrackedTurnSummary(p, "hello")
	if p["lastAgentMessage"] != "hello" {
		t.Errorf("top-level: got %v", p["lastAgentMessage"])
	}
	turnObj, _ := p["turn"].(map[string]any)
	if turnObj["lastAgentMessage"] != "hello" {
		t.Errorf("turn: got %v", turnObj["lastAgentMessage"])
	}
}

// ========================================
// trackedTurnSummaryCacheKey
// ========================================

func TestTrackedTurnSummaryCacheKey(t *testing.T) {
	key := trackersvc.TrackedTurnSummaryCacheKey("thread-1", "turn-1")
	if key != "thread-1\x00turn-1" {
		t.Errorf("got %q", key)
	}
	key = trackersvc.TrackedTurnSummaryCacheKey("  thread-1  ", "  ")
	if key != "thread-1\x00" {
		t.Errorf("trimmed: got %q", key)
	}
}
