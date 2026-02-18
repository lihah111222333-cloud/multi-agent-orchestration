package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func resetDebugBridgeStateForTest() {
	debugBridgeHub.mu.Lock()
	debugBridgeHub.nextID = 0
	debugBridgeHub.events = debugBridgeHub.events[:0]
	debugBridgeHub.mu.Unlock()

	debugBridgeMetrics.publishedTotal.Store(0)
	debugBridgeMetrics.droppedTotal.Store(0)
	debugBridgeMetrics.pollRequestTotal.Store(0)
	debugBridgeMetrics.pollResponseTotal.Store(0)
	debugBridgeMetrics.pollEventOutTotal.Store(0)
	debugBridgeMetrics.pollWriteFailTotal.Store(0)
	debugBridgeEnabled.Store(false)
}

func TestHandleFirstPoll(t *testing.T) {
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
	got := len(debugBridgeHub.events)
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
	got := len(debugBridgeHub.events)
	debugBridgeHub.mu.RUnlock()
	if got != 1 {
		t.Fatalf("expected queued event when debug bridge enabled, got %d", got)
	}
	if published := debugBridgeMetrics.publishedTotal.Load(); published != 1 {
		t.Fatalf("expected published_total=1 when enabled, got %d", published)
	}
}
