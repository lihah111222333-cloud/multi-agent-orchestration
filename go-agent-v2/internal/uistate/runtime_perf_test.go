package uistate

import (
	"testing"
)

func setDiffTextHelper(mgr *RuntimeManager, threadID, diff string) {
	ev := NormalizeEvent("turn_diff", "", mustRawJSON(`{"diff":"`+diff+`"}`))
	mgr.ApplyAgentEvent(threadID, ev, map[string]any{"diff": diff})
}

// ── SnapshotLight ────────────────────────────────────────────────

func TestSnapshotLight_ExcludesTimelinesAndDiffs(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-light"

	// 添加 timeline 数据
	mgr.HydrateHistory(threadID, []HistoryRecord{
		{ID: 1, Role: "user", Content: "hello"},
		{ID: 2, Role: "assistant", Content: "world"},
	})

	// 添加 diff 数据
	setDiffTextHelper(mgr, threadID, "diff --git a/main.go b/main.go")

	// 验证 Snapshot() 包含 timeline 和 diff
	full := mgr.Snapshot()
	if len(full.TimelinesByThread[threadID]) == 0 {
		t.Fatal("full snapshot: timeline should not be empty")
	}
	if full.DiffTextByThread[threadID] == "" {
		t.Fatal("full snapshot: diff text is empty")
	}

	// 验证 SnapshotLight() 不包含 timeline 和 diff
	light := mgr.SnapshotLight()
	if len(light.TimelinesByThread) != 0 {
		t.Fatalf("light snapshot: timelinesByThread should be empty, got %d entries", len(light.TimelinesByThread))
	}
	if len(light.DiffTextByThread) != 0 {
		t.Fatalf("light snapshot: diffTextByThread should be empty, got %d entries", len(light.DiffTextByThread))
	}
}

func TestSnapshotLight_PreservesOtherFields(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-light-fields"

	mgr.ReplaceThreads([]ThreadSnapshot{
		{ID: threadID, Name: "test", State: "idle"},
	})

	turnStart := NormalizeEvent("turn_started", "", nil)
	mgr.ApplyAgentEvent(threadID, turnStart, map[string]any{})

	light := mgr.SnapshotLight()

	// threads 应该保留
	if len(light.Threads) != 1 {
		t.Fatalf("light snapshot: threads len = %d, want 1", len(light.Threads))
	}
	if light.Threads[0].ID != threadID {
		t.Fatalf("light snapshot: thread id = %q, want %q", light.Threads[0].ID, threadID)
	}

	// statuses 应该保留
	if got := light.Statuses[threadID]; got != "thinking" {
		t.Fatalf("light snapshot: status = %q, want thinking", got)
	}

	// headers 应该保留
	if got := light.StatusHeadersByThread[threadID]; got == "" {
		t.Fatal("light snapshot: status header is empty")
	}
}

// ── ThreadTimeline ────────────────────────────────────────────────

func TestThreadTimeline_ReturnsSingleThread(t *testing.T) {
	mgr := NewRuntimeManager()

	mgr.HydrateHistory("thread-a", []HistoryRecord{
		{ID: 1, Role: "user", Content: "msg a"},
	})
	mgr.HydrateHistory("thread-b", []HistoryRecord{
		{ID: 2, Role: "user", Content: "msg b1"},
		{ID: 3, Role: "user", Content: "msg b2"},
	})

	timelineA := mgr.ThreadTimeline("thread-a")
	if len(timelineA) != 1 {
		t.Fatalf("thread-a timeline len = %d, want 1", len(timelineA))
	}
	if timelineA[0].Text != "msg a" {
		t.Fatalf("thread-a timeline[0].Text = %q, want 'msg a'", timelineA[0].Text)
	}

	timelineB := mgr.ThreadTimeline("thread-b")
	if len(timelineB) != 2 {
		t.Fatalf("thread-b timeline len = %d, want 2", len(timelineB))
	}
}

func TestThreadTimeline_EmptyForUnknownThread(t *testing.T) {
	mgr := NewRuntimeManager()
	timeline := mgr.ThreadTimeline("nonexistent")
	if timeline == nil {
		t.Fatal("ThreadTimeline should return empty slice, not nil")
	}
	if len(timeline) != 0 {
		t.Fatalf("timeline len = %d, want 0", len(timeline))
	}
}

func TestThreadTimeline_EmptyStringReturnsNil(t *testing.T) {
	mgr := NewRuntimeManager()
	timeline := mgr.ThreadTimeline("")
	if timeline != nil {
		t.Fatal("ThreadTimeline('') should return nil")
	}
}

func TestThreadTimeline_WhitespaceTrimsToEmpty(t *testing.T) {
	mgr := NewRuntimeManager()
	timeline := mgr.ThreadTimeline("   ")
	if timeline != nil {
		t.Fatal("ThreadTimeline('   ') should return nil")
	}
}

// ── ThreadDiff ────────────────────────────────────────────────

func TestThreadDiff_ReturnsSingleThread(t *testing.T) {
	mgr := NewRuntimeManager()
	setDiffTextHelper(mgr, "thread-diff", "diff content")

	got := mgr.ThreadDiff("thread-diff")
	if got != "diff content" {
		t.Fatalf("ThreadDiff = %q, want 'diff content'", got)
	}
}

func TestThreadDiff_EmptyForUnknownThread(t *testing.T) {
	mgr := NewRuntimeManager()
	got := mgr.ThreadDiff("nonexistent")
	if got != "" {
		t.Fatalf("ThreadDiff for unknown = %q, want empty", got)
	}
}

func TestThreadDiff_EmptyStringReturnsEmpty(t *testing.T) {
	mgr := NewRuntimeManager()
	got := mgr.ThreadDiff("")
	if got != "" {
		t.Fatalf("ThreadDiff('') = %q, want empty", got)
	}
}

// ── cloneSnapshotLight 隔离性 ────────────────────────────────

func TestSnapshotLight_IsIsolatedFromMutation(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-isolate"

	mgr.ReplaceThreads([]ThreadSnapshot{
		{ID: threadID, Name: "test", State: "idle"},
	})

	light1 := mgr.SnapshotLight()

	// 修改 light1 不应影响 mgr
	light1.Statuses["injected"] = "hacked"
	light1.Threads = append(light1.Threads, ThreadSnapshot{ID: "injected"})

	light2 := mgr.SnapshotLight()
	if _, ok := light2.Statuses["injected"]; ok {
		t.Fatal("mutation of light snapshot leaked into manager")
	}
	if len(light2.Threads) != 1 {
		t.Fatalf("threads len = %d, want 1", len(light2.Threads))
	}
}

// ── HydrateHistory overlay 清理 ────────────────────────────

func TestHydrateHistory_ClearsOverlayStatus(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-hydrate-overlay"

	// 先触发 turn 让 status 变为 thinking
	turnStart := NormalizeEvent("turn_started", "", nil)
	mgr.ApplyAgentEvent(threadID, turnStart, map[string]any{})
	if got := mgr.Snapshot().Statuses[threadID]; got != "thinking" {
		t.Fatalf("pre-hydrate status = %q, want thinking", got)
	}

	// HydrateHistory 后 timeline 应该反映历史
	mgr.HydrateHistory(threadID, []HistoryRecord{
		{ID: 1, Role: "user", Content: "old message"},
	})

	timeline := mgr.Snapshot().TimelinesByThread[threadID]
	if len(timeline) == 0 {
		t.Fatal("timeline is empty after HydrateHistory")
	}
	if timeline[0].Text != "old message" {
		t.Fatalf("timeline[0].Text = %q, want 'old message'", timeline[0].Text)
	}
}

// ── Benchmark: Snapshot vs SnapshotLight ────────────────────

func BenchmarkSnapshot_WithLargeTimeline(b *testing.B) {
	mgr := NewRuntimeManager()
	threadID := "thread-bench"

	// 生成大量 timeline items
	records := make([]HistoryRecord, 500)
	for i := 0; i < 500; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		records[i] = HistoryRecord{
			ID:      int64(i + 1),
			Role:    role,
			Content: "This is a message with some content to simulate real data " + string(rune('a'+i%26)),
		}
	}
	mgr.HydrateHistory(threadID, records)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.Snapshot()
	}
}

func BenchmarkSnapshotLight_WithLargeTimeline(b *testing.B) {
	mgr := NewRuntimeManager()
	threadID := "thread-bench"

	records := make([]HistoryRecord, 500)
	for i := 0; i < 500; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		records[i] = HistoryRecord{
			ID:      int64(i + 1),
			Role:    role,
			Content: "This is a message with some content to simulate real data " + string(rune('a'+i%26)),
		}
	}
	mgr.HydrateHistory(threadID, records)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.SnapshotLight()
	}
}

func BenchmarkThreadTimeline_SingleThread(b *testing.B) {
	mgr := NewRuntimeManager()
	threadID := "thread-bench"

	records := make([]HistoryRecord, 500)
	for i := 0; i < 500; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		records[i] = HistoryRecord{
			ID:      int64(i + 1),
			Role:    role,
			Content: "This is a message with some content to simulate real data " + string(rune('a'+i%26)),
		}
	}
	mgr.HydrateHistory(threadID, records)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.ThreadTimeline(threadID)
	}
}
