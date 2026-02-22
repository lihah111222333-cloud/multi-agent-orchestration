package apiserver

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

func TestAppendBindingThreads(t *testing.T) {
	seen := map[string]struct{}{
		"agent-1": {},
	}
	base := []threadListItem{
		{ID: "agent-1", Name: "Agent 1", State: "running"},
	}

	bindings := []store.AgentCodexBinding{
		{AgentID: ""},
		{AgentID: "agent-1", CodexThreadID: "thread-a"},
		{AgentID: "agent-2", CodexThreadID: "thread-b"},
	}

	got := appendBindingThreads(base, seen, bindings)
	if len(got) != 2 {
		t.Fatalf("len(got)=%d, want 2", len(got))
	}
	if got[1].ID != "agent-2" {
		t.Fatalf("got[1].ID=%q, want agent-2", got[1].ID)
	}
	if got[1].State != "idle" {
		t.Fatalf("got[1].State=%q, want idle", got[1].State)
	}
}

func TestAppendAgentStatusThreads(t *testing.T) {
	seen := map[string]struct{}{
		"agent-1": {},
	}
	base := []threadListItem{
		{ID: "agent-1", Name: "Agent 1", State: "running"},
	}

	items := []store.AgentStatus{
		{AgentID: "", AgentName: "x", Status: "running"},
		{AgentID: "agent-1", AgentName: "Dup", Status: "idle"},
		{AgentID: "agent-2", AgentName: "", Status: ""},
		{AgentID: "agent-3", AgentName: "Agent 3", Status: "stuck"},
	}

	got := appendAgentStatusThreads(base, seen, items)
	if len(got) != 3 {
		t.Fatalf("len(got)=%d, want 3", len(got))
	}

	if got[1].ID != "agent-2" || got[1].Name != "agent-2" || got[1].State != "idle" {
		t.Fatalf("got[1]=%+v, want ID=agent-2 Name=agent-2 State=idle", got[1])
	}

	if got[2].ID != "agent-3" || got[2].Name != "Agent 3" || got[2].State != "idle" {
		t.Fatalf("got[2]=%+v, want ID=agent-3 Name=Agent 3 State=idle", got[2])
	}
}

func TestAppendArchivedThreads(t *testing.T) {
	seen := map[string]struct{}{
		"agent-1": {},
	}
	base := []threadListItem{
		{ID: "agent-1", Name: "Agent 1", State: "running"},
	}

	archived := map[string]int64{
		"":        123,
		"agent-1": 124, // already seen
		"agent-2": 100,
		"agent-3": 200,
		"agent-4": 0, // invalid timestamp
	}

	got := appendArchivedThreads(base, seen, archived)
	if len(got) != 3 {
		t.Fatalf("len(got)=%d, want 3", len(got))
	}
	if got[1].ID != "agent-3" || got[1].Name != "agent-3" || got[1].State != "idle" {
		t.Fatalf("got[1]=%+v, want ID=agent-3 Name=agent-3 State=idle", got[1])
	}
	if got[2].ID != "agent-2" || got[2].Name != "agent-2" || got[2].State != "idle" {
		t.Fatalf("got[2]=%+v, want ID=agent-2 Name=agent-2 State=idle", got[2])
	}
}
