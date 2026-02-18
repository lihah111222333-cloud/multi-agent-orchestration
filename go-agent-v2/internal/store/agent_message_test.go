package store

import (
	"encoding/json"
	"testing"
)

func TestBuildMessageDedupKey(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		method    string
		metadata  any
		want      string
	}{
		{
			name:      "turn completed with turn id",
			eventType: "turn_complete",
			method:    "turn/completed",
			metadata: map[string]any{
				"turn": map[string]any{"id": "turn-123"},
			},
			want: "turn/completed|turn-123",
		},
		{
			name:      "legacy task complete with msg turn_id",
			eventType: "codex/event/task_complete",
			method:    "codex/event/task_complete",
			metadata: map[string]any{
				"msg": map[string]any{"turn_id": "turn-456"},
			},
			want: "codex/event/task_complete|turn-456",
		},
		{
			name:      "dynamic tool call with camel call id",
			eventType: "dynamic_tool_call",
			method:    "dynamic-tool/called",
			metadata: map[string]any{
				"callId": "call-789",
			},
			want: "dynamic-tool/called|call-789",
		},
		{
			name:      "non idempotent event returns empty",
			eventType: "agent_message_delta",
			method:    "item/agentMessage/delta",
			metadata: map[string]any{
				"id": "delta-1",
			},
			want: "",
		},
		{
			name:      "missing id returns empty",
			eventType: "turn_complete",
			method:    "turn/completed",
			metadata: map[string]any{
				"turn": map[string]any{},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.metadata != nil {
				data, err := json.Marshal(tt.metadata)
				if err != nil {
					t.Fatalf("marshal metadata: %v", err)
				}
				raw = data
			}
			got := BuildMessageDedupKey(tt.eventType, tt.method, raw)
			if got != tt.want {
				t.Fatalf("BuildMessageDedupKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
