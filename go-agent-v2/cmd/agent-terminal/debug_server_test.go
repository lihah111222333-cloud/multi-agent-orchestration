package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func resetDebugBridgeStateForTest() {
	debugBridgeHub.mu.Lock()
	debugBridgeHub.seq = 0
	debugBridgeHub.head = 0
	debugBridgeHub.size = 0
	debugBridgeHub.ring = nil // 下次 publish 时延迟分配
	debugBridgeHub.mu.Unlock()

	debugBridgeMetrics.publishedTotal.Store(0)
	debugBridgeMetrics.droppedTotal.Store(0)
	debugBridgeMetrics.overflowCount.Store(0)
	debugBridgeMetrics.droppableSkipTotal.Store(0)
	debugBridgeMetrics.pollRequestTotal.Store(0)
	debugBridgeMetrics.pollResponseTotal.Store(0)
	debugBridgeMetrics.pollEventOutTotal.Store(0)
	debugBridgeMetrics.pollWriteFailTotal.Store(0)
	debugBridgeEnabled.Store(false)
}

func TestHandleFirstPoll(t *testing.T) {
	resetDebugBridgeStateForTest()
	debugBridgeEnabled.Store(true)
	rec := httptest.NewRecorder()
	params := debugPollParams{after: 0, effectiveLimit: 200}

	if !handleFirstPoll(rec, 1, params, time.Now()) {
		t.Fatal("expected first poll to be handled")
	}
	if rec.Code == 0 {
		t.Fatal("expected response status to be written")
	}
}

func TestHandleFirstPoll_NotFirstPoll(t *testing.T) {
	rec := httptest.NewRecorder()
	params := debugPollParams{after: 10, effectiveLimit: 200}

	if handleFirstPoll(rec, 1, params, time.Now()) {
		t.Fatal("expected non-first poll to skip helper")
	}
}

func TestPublishDebugBridgeEvent_DisabledByDefault(t *testing.T) {
	resetDebugBridgeStateForTest()

	publishDebugBridgeEvent("ui/state/changed", map[string]any{"threadId": "t-1"})

	debugBridgeHub.mu.RLock()
	got := debugBridgeHub.size
	debugBridgeHub.mu.RUnlock()
	if got != 0 {
		t.Fatalf("expected no queued event when debug bridge disabled, got %d", got)
	}
	if published := debugBridgeMetrics.publishedTotal.Load(); published != 0 {
		t.Fatalf("expected published_total=0 when debug bridge disabled, got %d", published)
	}
}

func TestPublishDebugBridgeEvent_EnabledQueuesEvent(t *testing.T) {
	resetDebugBridgeStateForTest()
	debugBridgeEnabled.Store(true)

	publishDebugBridgeEvent("ui/state/changed", map[string]any{"threadId": "t-1"})

	debugBridgeHub.mu.RLock()
	got := debugBridgeHub.size
	debugBridgeHub.mu.RUnlock()
	if got != 1 {
		t.Fatalf("expected queued event when debug bridge enabled, got %d", got)
	}
	if published := debugBridgeMetrics.publishedTotal.Load(); published != 1 {
		t.Fatalf("expected published_total=1 when enabled, got %d", published)
	}
}

// ────────────────────────────────────────────────────
// shouldLogOverflow — 溢出日志采样 (按方法类型)
// ────────────────────────────────────────────────────

func TestShouldLogOverflow_LowFreqAlwaysLogs(t *testing.T) {
	// 低频方法: 任何溢出次数都应该打 WARN
	for _, n := range []int64{1, 2, 50, 500, 1001} {
		if !shouldLogOverflow("thread/start", n) {
			t.Errorf("low-freq method thread/start at count=%d should always log", n)
		}
	}
	if !shouldLogOverflow("turn/completed", 999) {
		t.Error("low-freq method turn/completed should always log")
	}
}

func TestShouldLogOverflow_HighFreqFirstOverflowLogs(t *testing.T) {
	// 高频方法: 第 1 次溢出仍然打印
	for _, method := range []string{
		"ui/state/changed",
		"account/rateLimits/updated",
		"item/content/delta",
		"response.output_text.delta",
	} {
		if !shouldLogOverflow(method, 1) {
			t.Errorf("high-freq method %q at count=1 should log", method)
		}
	}
}

func TestShouldLogOverflow_HighFreqSampled(t *testing.T) {
	// 高频方法: count=2..999 被采样掉
	for _, n := range []int64{2, 50, 500, 999} {
		if shouldLogOverflow("item/content/delta", n) {
			t.Errorf("high-freq method at count=%d should be sampled out", n)
		}
	}
	// count=1000 打印
	if !shouldLogOverflow("item/content/delta", debugBridgeOverflowLogEvery) {
		t.Errorf("high-freq method at count=%d should log (interval hit)", debugBridgeOverflowLogEvery)
	}
}

func TestShouldLogOverflow_HighFreqPatterns(t *testing.T) {
	// 验证所有高频关键词都能匹配
	highFreqMethods := []string{
		"item/content/delta",
		"response.output_text.delta",
		"agent/output/append",
		"agent/stream/chunk",
		"ui/state/changed",
		"account/rateLimits/updated",
	}
	for _, m := range highFreqMethods {
		if shouldLogOverflow(m, 2) {
			t.Errorf("method %q should be recognized as high-freq and sampled at count=2", m)
		}
	}
}

// ────────────────────────────────────────────────────
// Fix 1: isDroppableHighFreqMethod — 分类准确性
// ────────────────────────────────────────────────────

func TestIsDroppableHighFreqMethod_DeltaMethods(t *testing.T) {
	droppable := []string{
		"item/agentMessage/delta",
		"item/reasoning/summaryTextDelta",
		"item/reasoning/textDelta",
		"item/plan/delta",
		"item/commandExecution/outputDelta",
		"item/fileChange/outputDelta",
		"item/started",
		"item/completed",
		"thread/tokenUsage/updated",
		"item/reasoning/summaryPartAdded",
		"account/rateLimits/updated",
	}
	for _, m := range droppable {
		if !isDroppableHighFreqMethod(m) {
			t.Errorf("expected %q to be droppable high-freq", m)
		}
	}
}

func TestIsDroppableHighFreqMethod_NonDroppable(t *testing.T) {
	nonDroppable := []string{
		"ui/state/changed",
		"thread/started",
		"turn/completed",
		"error",
	}
	for _, m := range nonDroppable {
		if isDroppableHighFreqMethod(m) {
			t.Errorf("expected %q to NOT be droppable", m)
		}
	}
}

// ────────────────────────────────────────────────────
// Fix 1: 可丢弃事件入队采样
// ────────────────────────────────────────────────────

func TestPublishDebugBridgeEvent_DroppableHighFreqSampled(t *testing.T) {
	resetDebugBridgeStateForTest()
	debugBridgeEnabled.Store(true)

	// 发 10 条 delta 事件
	for i := 0; i < 10; i++ {
		publishDebugBridgeEvent("item/agentMessage/delta", map[string]any{"threadId": "t-1"})
	}

	debugBridgeHub.mu.RLock()
	got := debugBridgeHub.size
	debugBridgeHub.mu.RUnlock()

	// 采样率 1/5: 10 条应该只入队 2 条
	if got != 2 {
		t.Fatalf("expected 2 events queued from 10 droppable, got %d", got)
	}
}

func TestPublishDebugBridgeEvent_NonDroppableAlwaysQueued(t *testing.T) {
	resetDebugBridgeStateForTest()
	debugBridgeEnabled.Store(true)

	// 发 10 条 state/changed 事件 — 不可丢弃
	for i := 0; i < 10; i++ {
		publishDebugBridgeEvent("ui/state/changed", map[string]any{"threadId": "t-1"})
	}

	debugBridgeHub.mu.RLock()
	got := debugBridgeHub.size
	debugBridgeHub.mu.RUnlock()

	// 不可丢弃: 10 条全部入队
	if got != 10 {
		t.Fatalf("expected all 10 non-droppable events queued, got %d", got)
	}
}

func TestPublishDebugBridgeEvent_DroppableSkipMetric(t *testing.T) {
	resetDebugBridgeStateForTest()
	debugBridgeEnabled.Store(true)

	for i := 0; i < 10; i++ {
		publishDebugBridgeEvent("item/agentMessage/delta", map[string]any{"threadId": "t-1"})
	}

	skipTotal := debugBridgeMetrics.droppableSkipTotal.Load()
	if skipTotal != 10 {
		t.Fatalf("expected droppableSkipTotal=10, got %d", skipTotal)
	}
}
