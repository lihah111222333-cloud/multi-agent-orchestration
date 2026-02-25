package codexadapter

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

// ========================================
// E2: appendThreadItems — 泛型去重合并
// ========================================

func TestAppendThreadItems_Bindings(t *testing.T) {
	seen := map[string]struct{}{}
	threads := appendThreadItems(nil, seen, []store.AgentCodexBinding{
		{AgentID: "a1"},
		{AgentID: "a2"},
		{AgentID: ""}, // should skip
	})
	if len(threads) != 2 {
		t.Fatalf("got %d, want 2", len(threads))
	}
	if threads[0].ID != "a1" || threads[1].ID != "a2" {
		t.Errorf("unexpected threads: %+v", threads)
	}
}

func TestAppendThreadItems_AgentStatus(t *testing.T) {
	seen := map[string]struct{}{}
	threads := appendThreadItems(nil, seen, []store.AgentStatus{
		{AgentID: "s1", AgentName: "Status One"},
		{AgentID: "s2"},
	})
	if len(threads) != 2 {
		t.Fatalf("got %d, want 2", len(threads))
	}
	if threads[0].Name != "Status One" {
		t.Errorf("name not preserved: got %q", threads[0].Name)
	}
	if threads[1].Name != "s2" {
		t.Errorf("empty name should fallback to ID: got %q", threads[1].Name)
	}
}

func TestAppendThreadItems_Dedup(t *testing.T) {
	seen := map[string]struct{}{"a1": {}}
	threads := appendThreadItems(nil, seen, []store.AgentCodexBinding{
		{AgentID: "a1"}, // already seen
		{AgentID: "a2"},
	})
	if len(threads) != 1 {
		t.Fatalf("got %d, want 1 (a1 should be deduped)", len(threads))
	}
	if threads[0].ID != "a2" {
		t.Errorf("got %q, want a2", threads[0].ID)
	}
}

func TestAppendThreadItems_RunnerAgentInfo(t *testing.T) {
	seen := map[string]struct{}{}
	threads := appendThreadItems(nil, seen, []runner.AgentInfo{
		{ID: "r1", Name: "Runner One", State: runner.StateRunning},
		{ID: "r2", State: runner.StateIdle},
	})
	if len(threads) != 2 {
		t.Fatalf("got %d, want 2", len(threads))
	}
	if threads[0].State != "running" {
		t.Errorf("state: got %q, want running", threads[0].State)
	}
}

// ========================================
// E3: toThreadSnapshots — 泛型快照构建
// ========================================

func TestToThreadSnapshots_FromAgentInfo(t *testing.T) {
	items := []runner.AgentInfo{
		{ID: "r1", Name: "Runner", State: runner.StateRunning},
		{ID: "", Name: "skip"},
	}
	snaps := toThreadSnapshots(items)
	if len(snaps) != 1 {
		t.Fatalf("got %d, want 1", len(snaps))
	}
	if snaps[0].ID != "r1" || snaps[0].Name != "Runner" {
		t.Errorf("unexpected: %+v", snaps[0])
	}
}

func TestToThreadSnapshots_FromListItems(t *testing.T) {
	items := []threadListItem{
		{ID: "t1", Name: "Thread One", State: "idle"},
		{ID: "  ", Name: "skip"},
	}
	snaps := toThreadSnapshots(items)
	if len(snaps) != 1 {
		t.Fatalf("got %d, want 1", len(snaps))
	}
	if snaps[0] != (uistate.ThreadSnapshot{ID: "t1", Name: "Thread One", State: "idle"}) {
		t.Errorf("unexpected: %+v", snaps[0])
	}
}
