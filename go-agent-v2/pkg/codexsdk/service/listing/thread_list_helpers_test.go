package listing

import (
	"reflect"
	"testing"
)

func TestAppendThreadItems_Bindings(t *testing.T) {
	seen := map[string]struct{}{}
	threads := AppendThreadItems(nil, seen, []AgentCodexBinding{
		{AgentID: "a1"},
		{AgentID: "a2"},
		{AgentID: ""},
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
	threads := AppendThreadItems(nil, seen, []AgentStatus{
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
	threads := AppendThreadItems(nil, seen, []AgentCodexBinding{
		{AgentID: "a1"},
		{AgentID: "a2"},
	})
	if len(threads) != 1 {
		t.Fatalf("got %d, want 1 (a1 should be deduped)", len(threads))
	}
	if threads[0].ID != "a2" {
		t.Errorf("got %q, want a2", threads[0].ID)
	}
}

func TestAppendThreadItems_AgentInfo(t *testing.T) {
	seen := map[string]struct{}{}
	threads := AppendThreadItems(nil, seen, []AgentInfo{
		{ID: "r1", Name: "Runner One", State: "running"},
		{ID: "r2", State: "idle"},
	})
	if len(threads) != 2 {
		t.Fatalf("got %d, want 2", len(threads))
	}
	if threads[0].State != "running" {
		t.Errorf("state: got %q, want running", threads[0].State)
	}
}

func TestToThreadSnapshots_FromAgentInfo(t *testing.T) {
	items := []AgentInfo{
		{ID: "r1", Name: "Runner", State: "running"},
		{ID: "", Name: "skip"},
	}
	snaps := ToThreadSnapshots(items)
	if len(snaps) != 1 {
		t.Fatalf("got %d, want 1", len(snaps))
	}
	if snaps[0].ID != "r1" || snaps[0].Name != "Runner" {
		t.Errorf("unexpected: %+v", snaps[0])
	}
}

func TestToThreadSnapshots_FromListItems(t *testing.T) {
	items := []ThreadListItem{
		{ID: "t1", Name: "Thread One", State: "idle"},
		{ID: "  ", Name: "skip"},
	}
	snaps := ToThreadSnapshots(items)
	if len(snaps) != 1 {
		t.Fatalf("got %d, want 1", len(snaps))
	}
	if snaps[0] != (ThreadSnapshot{ID: "t1", Name: "Thread One", State: "idle"}) {
		t.Errorf("unexpected: %+v", snaps[0])
	}
}

func TestLoadedThreadIDsFromAgents(t *testing.T) {
	agents := []AgentInfo{
		{ID: "  thread-c  "},
		{ID: "thread-a"},
		{ID: "thread-b"},
		{ID: "thread-a"},
		{ID: ""},
	}
	got := LoadedThreadIDsFromAgents(agents)
	want := []string{"thread-a", "thread-b", "thread-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadedThreadIDsFromAgents() = %v, want %v", got, want)
	}
}

func TestPaginateLoadedThreadIDsAppliesCursorAndLimit(t *testing.T) {
	ids := []string{"thread-a", "thread-b", "thread-c", "thread-d"}
	cursor := "thread-b"
	limit := uint32(1)

	gotPage, gotNext := PaginateLoadedThreadIDs(ids, &cursor, &limit)
	wantPage := []string{"thread-c"}
	if !reflect.DeepEqual(gotPage, wantPage) {
		t.Fatalf("page = %v, want %v", gotPage, wantPage)
	}
	if gotNext == nil || *gotNext != "thread-c" {
		t.Fatalf("next cursor = %v, want thread-c", gotNext)
	}
}

func TestPaginateLoadedThreadIDsReturnsEmptyPageWhenCursorAfterTail(t *testing.T) {
	ids := []string{"thread-a", "thread-b"}
	cursor := "thread-z"

	gotPage, gotNext := PaginateLoadedThreadIDs(ids, &cursor, nil)
	if len(gotPage) != 0 {
		t.Fatalf("page len = %d, want 0", len(gotPage))
	}
	if gotPage == nil {
		t.Fatalf("page should be empty slice, got nil")
	}
	if gotNext != nil {
		t.Fatalf("next cursor = %v, want nil", gotNext)
	}
}

func TestPaginateLoadedThreadIDsClampsLimitToAtLeastOne(t *testing.T) {
	ids := []string{"thread-a", "thread-b"}
	limit := uint32(0)

	gotPage, gotNext := PaginateLoadedThreadIDs(ids, nil, &limit)
	wantPage := []string{"thread-a"}
	if !reflect.DeepEqual(gotPage, wantPage) {
		t.Fatalf("page = %v, want %v", gotPage, wantPage)
	}
	if gotNext == nil || *gotNext != "thread-a" {
		t.Fatalf("next cursor = %v, want thread-a", gotNext)
	}
}
