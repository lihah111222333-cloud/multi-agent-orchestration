package runner

import (
	"encoding/json"
	"testing"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

func TestSubmitEmitsUserMessageEvent(t *testing.T) {
	client := &fakeClient{}
	mgr, err := NewAgentManager(
		func(int, string) agentcore.Client { return client },
		func(int, string) agentcore.Client { return client },
	)
	if err != nil {
		t.Fatalf("NewAgentManager() error = %v", err)
	}

	var events []agentcore.Event
	mgr.SetOnEvent(func(agentID string, event agentcore.Event) {
		events = append(events, event)
	})

	// Manually insert an agent process (bypass Launch).
	proc := &AgentProcess{ID: "sub-1", Name: "sub", Client: client, State: StateIdle}
	mgr.mu.Lock()
	mgr.agents["sub-1"] = proc
	mgr.mu.Unlock()

	if submitErr := mgr.Submit("sub-1", "hello sub", nil, nil); submitErr != nil {
		t.Fatalf("Submit() error = %v", submitErr)
	}

	// Must have emitted a user_message event.
	found := false
	for _, ev := range events {
		if ev.Type == "user_message" {
			found = true
			var payload map[string]any
			if err := json.Unmarshal(ev.Data, &payload); err != nil {
				t.Fatalf("unmarshal user_message payload: %v", err)
			}
			if payload["content"] != "hello sub" {
				t.Fatalf("content = %q, want %q", payload["content"], "hello sub")
			}
			if payload["role"] != "user" {
				t.Fatalf("role = %q, want %q", payload["role"], "user")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected user_message event to be emitted by Submit()")
	}
}

func TestExtractLastAgentMessageExpandedKeys(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{name: "last_agent_message", payload: map[string]any{"last_agent_message": "report1"}, want: "report1"},
		{name: "lastAgentMessage", payload: map[string]any{"lastAgentMessage": "report2"}, want: "report2"},
		{name: "summary", payload: map[string]any{"summary": "report3"}, want: "report3"},
		{name: "result", payload: map[string]any{"result": "report4"}, want: "report4"},
		{name: "message", payload: map[string]any{"message": "report5"}, want: "report5"},
		{name: "output", payload: map[string]any{"output": "report6"}, want: "report6"},
		{name: "content", payload: map[string]any{"content": "report7"}, want: "report7"},
		{name: "response", payload: map[string]any{"response": "report8"}, want: "report8"},
		{name: "text", payload: map[string]any{"text": "report9"}, want: "report9"},
		{name: "nested_turn", payload: map[string]any{
			"turn": map[string]any{"last_agent_message": "nested_report"},
		}, want: "nested_report"},
		{name: "nested_msg", payload: map[string]any{
			"msg": map[string]any{"summary": "msg_report"},
		}, want: "msg_report"},
		{name: "empty", payload: map[string]any{}, want: ""},
		{name: "nil", payload: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLastAgentMessageFromMap(tt.payload)
			if got != tt.want {
				t.Errorf("extractLastAgentMessageFromMap() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractLastAgentMessageFromRawJSON(t *testing.T) {
	raw := json.RawMessage(`{"output": "task done successfully"}`)
	got := extractLastAgentMessage(raw)
	if got != "task done successfully" {
		t.Errorf("extractLastAgentMessage() = %q, want %q", got, "task done successfully")
	}
}
