package uistate

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRuntimeManager_ApplyAgentEvent(t *testing.T) {
	manager := NewRuntimeManager()
	manager.ReplaceThreads([]ThreadSnapshot{{ID: "thread-1", Name: "thread-1", State: "idle"}})

	manager.ApplyAgentEvent("thread-1", NormalizedEvent{
		UIType:   UITypeTurnStarted,
		UIStatus: UIStatusThinking,
	}, map[string]any{})
	manager.ApplyAgentEvent("thread-1", NormalizedEvent{
		UIType:   UITypeAssistantDelta,
		UIStatus: UIStatusThinking,
		Text:     "hello",
	}, map[string]any{})
	manager.ApplyAgentEvent("thread-1", NormalizedEvent{
		UIType:   UITypeTurnComplete,
		UIStatus: UIStatusIdle,
	}, map[string]any{})

	snapshot := manager.Snapshot()
	if got := snapshot.Statuses["thread-1"]; got != "idle" {
		t.Fatalf("status = %q, want idle", got)
	}

	timeline := snapshot.TimelinesByThread["thread-1"]
	if len(timeline) == 0 {
		t.Fatal("timeline should not be empty")
	}
	last := timeline[len(timeline)-1]
	if last.Kind != "assistant" {
		t.Fatalf("last kind = %q, want assistant", last.Kind)
	}
	if last.Text != "hello" {
		t.Fatalf("assistant text = %q, want hello", last.Text)
	}
}

func TestRuntimeManager_HydrateHistory(t *testing.T) {
	manager := NewRuntimeManager()
	manager.ReplaceThreads([]ThreadSnapshot{{ID: "thread-2", Name: "thread-2", State: "idle"}})

	manager.HydrateHistory("thread-2", []HistoryRecord{
		{
			ID:        1,
			Role:      "user",
			EventType: "user_message",
			Content:   "hi",
			CreatedAt: time.Now().Add(-2 * time.Minute),
		},
		{
			ID:        2,
			Role:      "assistant",
			EventType: "agent_message_delta",
			Content:   "hello",
			Metadata:  json.RawMessage(`{"delta":"hello"}`),
			CreatedAt: time.Now().Add(-1 * time.Minute),
		},
		{
			ID:        3,
			Role:      "assistant",
			EventType: "agent_message_completed",
			Content:   "",
			Metadata:  json.RawMessage(`{}`),
			CreatedAt: time.Now(),
		},
	})

	snapshot := manager.Snapshot()
	timeline := snapshot.TimelinesByThread["thread-2"]
	if len(timeline) < 2 {
		t.Fatalf("timeline length = %d, want >= 2", len(timeline))
	}
	if timeline[0].Kind != "user" {
		t.Fatalf("timeline[0].kind = %q, want user", timeline[0].Kind)
	}
	if timeline[len(timeline)-1].Kind != "assistant" {
		t.Fatalf("last kind = %q, want assistant", timeline[len(timeline)-1].Kind)
	}
}

func TestRuntimeManager_WorkspaceState(t *testing.T) {
	manager := NewRuntimeManager()
	manager.ReplaceWorkspaceRuns([]map[string]any{
		{"runKey": "rk-1", "status": "active"},
	})
	manager.ApplyWorkspaceMergeResult("rk-1", map[string]any{
		"status": "merged",
		"merged": 3,
	})

	snapshot := manager.Snapshot()
	run := snapshot.WorkspaceRunsByKey["rk-1"]
	if run == nil {
		t.Fatal("workspace run rk-1 should exist")
	}
	if run["status"] != "merged" {
		t.Fatalf("run status = %v, want merged", run["status"])
	}
}
