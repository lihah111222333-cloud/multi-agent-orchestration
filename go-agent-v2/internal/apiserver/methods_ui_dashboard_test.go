package apiserver

import (
	"context"
	"encoding/json"
	"testing"
)

func TestUIDashboardGetReturnsStableShape(t *testing.T) {
	srv := &Server{}

	raw, err := srv.uiDashboardGet(context.Background(), uiDashboardGetParams{Page: "tasks"})
	if err != nil {
		t.Fatalf("uiDashboardGet error: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("uiDashboardGet type=%T, want map[string]any", raw)
	}

	keys := []string{
		"agents",
		"dags",
		"taskAcks",
		"taskTraces",
		"skills",
		"commandCards",
		"prompts",
		"memory",
	}
	for _, key := range keys {
		value, exists := resp[key]
		if !exists {
			t.Fatalf("missing key %q", key)
		}
		if _, ok := value.([]any); !ok {
			t.Fatalf("key %q type=%T, want []any", key, value)
		}
	}
}

func TestUIDashboardGetAgentsFallsBackToThreads(t *testing.T) {
	srv := &Server{
		methods: map[string]Handler{
			"dashboard/agentStatus": func(_ context.Context, _ json.RawMessage) (any, error) {
				return map[string]any{"agents": []any{}}, nil
			},
			"thread/list": func(_ context.Context, _ json.RawMessage) (any, error) {
				return threadListResponse{
					Threads: []threadListItem{
						{ID: "agent-1", Name: "Agent One", State: "running"},
					},
				}, nil
			},
		},
	}

	raw, err := srv.uiDashboardGet(context.Background(), uiDashboardGetParams{Page: "agents"})
	if err != nil {
		t.Fatalf("uiDashboardGet error: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("uiDashboardGet type=%T, want map[string]any", raw)
	}

	agents, ok := resp["agents"].([]any)
	if !ok {
		t.Fatalf("agents type=%T, want []any", resp["agents"])
	}
	if len(agents) != 1 {
		t.Fatalf("agents len=%d, want 1", len(agents))
	}

	first, ok := agents[0].(map[string]any)
	if !ok {
		t.Fatalf("agents[0] type=%T, want map[string]any", agents[0])
	}
	if got := first["agent_id"]; got != "agent-1" {
		t.Fatalf("agent_id=%v, want agent-1", got)
	}
	if got := first["status"]; got != "running" {
		t.Fatalf("status=%v, want running", got)
	}
	if _, ok := first["updated_at"]; !ok {
		t.Fatal("updated_at is missing")
	}
}
