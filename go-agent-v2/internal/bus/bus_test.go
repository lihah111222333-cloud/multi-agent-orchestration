package bus

import (
	"encoding/json"
	"testing"
	"time"
)

// ========================================
// MessageBus 测试
// ========================================

func TestPublishSubscribe(t *testing.T) {
	b := NewMessageBus()
	sub := b.Subscribe("s1", "agent.a0")

	b.Publish(Message{
		Topic:   "agent.a0.output",
		From:    "a0",
		To:      "*",
		Type:    MsgAgentOutput,
		Payload: json.RawMessage(`{"delta":"hello"}`),
	})

	select {
	case msg := <-sub.Ch:
		if msg.Topic != "agent.a0.output" {
			t.Errorf("topic = %q, want agent.a0.output", msg.Topic)
		}
		if msg.Seq != 1 {
			t.Errorf("seq = %d, want 1", msg.Seq)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for message")
	}
}

func TestTopicFiltering(t *testing.T) {
	b := NewMessageBus()
	subA := b.Subscribe("sa", "agent.a0")
	subB := b.Subscribe("sb", "agent.b1")
	subAll := b.Subscribe("sall", "*")

	b.Publish(Message{Topic: "agent.a0.output", Type: MsgAgentOutput})

	// subA should receive
	select {
	case <-subA.Ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subA should receive agent.a0.output")
	}

	// subB should NOT receive
	select {
	case <-subB.Ch:
		t.Fatal("subB should not receive agent.a0.output")
	case <-time.After(50 * time.Millisecond):
	}

	// subAll should receive (wildcard)
	select {
	case <-subAll.Ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subAll should receive with '*' filter")
	}
}

func TestMatchTopic(t *testing.T) {
	tests := []struct {
		filter, topic string
		want          bool
	}{
		{"*", "anything", true},
		{"*", "agent.a0.output", true},
		{"agent.a0", "agent.a0", true},
		{"agent.a0", "agent.a0.output", true},
		{"agent.a0", "agent.a0.status", true},
		{"agent.a0", "agent.a1.output", false},
		{"agent.a0", "agent.a0x", false},
		{"system", "system", true},
		{"system", "system.health", true},
		{"system", "agent.a0", false},
	}
	for _, tc := range tests {
		got := matchTopic(tc.filter, tc.topic)
		if got != tc.want {
			t.Errorf("matchTopic(%q, %q) = %v, want %v", tc.filter, tc.topic, got, tc.want)
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	b := NewMessageBus()
	b.Subscribe("s1", "*")
	if b.SubscriberCount() != 1 {
		t.Fatalf("count = %d, want 1", b.SubscriberCount())
	}
	b.Unsubscribe("s1")
	if b.SubscriberCount() != 0 {
		t.Fatalf("count = %d, want 0", b.SubscriberCount())
	}
}

func TestOnPublishCallback(t *testing.T) {
	b := NewMessageBus()
	var captured Message
	b.SetOnPublish(func(msg Message) {
		captured = msg
	})

	b.Publish(Message{Topic: "test", Type: "ping"})

	if captured.Topic != "test" {
		t.Errorf("captured topic = %q, want test", captured.Topic)
	}
}

func TestSeq(t *testing.T) {
	b := NewMessageBus()
	b.Publish(Message{Topic: "t1"})
	b.Publish(Message{Topic: "t2"})
	b.Publish(Message{Topic: "t3"})
	if b.Seq() != 3 {
		t.Errorf("seq = %d, want 3", b.Seq())
	}
}

// ========================================
// OrchestrationState 测试
// ========================================

func TestOrchestrationLifecycle(t *testing.T) {
	b := NewMessageBus()
	orch := NewOrchestrationState(b)

	orch.BeginRun("run-1", "部署", "开始部署", "system")
	snap := orch.Snapshot()
	if snap.ActiveCount != 1 {
		t.Errorf("active_count = %d, want 1", snap.ActiveCount)
	}
	if !snap.Running {
		t.Error("running should be true")
	}

	orch.UpdateRun("run-1", "部署", "50% 完成", "system")
	snap = orch.Snapshot()
	if snap.ActiveRuns[0].StatusDetails != "50% 完成" {
		t.Errorf("status_details = %q, want 50%% 完成", snap.ActiveRuns[0].StatusDetails)
	}

	orch.EndRun("run-1", "system")
	snap = orch.Snapshot()
	if snap.ActiveCount != 0 {
		t.Errorf("active_count = %d, want 0", snap.ActiveCount)
	}
	if snap.Running {
		t.Error("running should be false after end")
	}
}

func TestOrchestrationReset(t *testing.T) {
	b := NewMessageBus()
	orch := NewOrchestrationState(b)

	orch.BeginRun("r1", "h", "d", "s")
	orch.BeginRun("r2", "h", "d", "s")
	orch.Reset("test")

	snap := orch.Snapshot()
	if snap.ActiveCount != 0 {
		t.Errorf("active_count after reset = %d, want 0", snap.ActiveCount)
	}
}

func TestBindingWarning(t *testing.T) {
	b := NewMessageBus()
	orch := NewOrchestrationState(b)

	orch.SetBindingWarning("session expired", "system")
	snap := orch.Snapshot()
	if snap.BindingWarning != "session expired" {
		t.Errorf("binding_warning = %q, want 'session expired'", snap.BindingWarning)
	}
}
