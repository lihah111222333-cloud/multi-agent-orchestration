package tooladapter

import (
	"encoding/json"
	"testing"

	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
)

func TestWithLSPToolCallMeta_InjectsRuntimeFields(t *testing.T) {
	reqID := int64(9)
	raw := json.RawMessage(`{"file_path":"a.go","threadId":"thread-7"}`)
	got := withLSPToolCallMeta(raw, tools.ToolCallContext{
		AgentID:   "agent-1",
		CallID:    "call-2",
		RequestID: &reqID,
	})

	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	meta, ok := payload[lspToolCallMetaKey].(map[string]any)
	if !ok || meta == nil {
		t.Fatalf("missing tool meta in payload: %#v", payload[lspToolCallMetaKey])
	}

	if v, _ := meta["agent_id"].(string); v != "agent-1" {
		t.Fatalf("agent_id = %q, want agent-1", v)
	}
	if v, _ := meta["call_id"].(string); v != "call-2" {
		t.Fatalf("call_id = %q, want call-2", v)
	}
	if v, _ := meta["thread_id"].(string); v != "thread-7" {
		t.Fatalf("thread_id = %q, want thread-7", v)
	}
	if v, ok := meta["request_id"].(float64); !ok || int64(v) != reqID {
		t.Fatalf("request_id = %#v, want %d", meta["request_id"], reqID)
	}
}

func TestExtractThreadIDFromToolArgs_Nested(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"payload": map[string]any{
				"thread_id": "thread-nested",
			},
		},
	}
	if got := extractThreadIDFromToolArgs(payload); got != "thread-nested" {
		t.Fatalf("extractThreadIDFromToolArgs = %q, want thread-nested", got)
	}
}
