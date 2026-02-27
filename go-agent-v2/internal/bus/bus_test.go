package bus

import (
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := NewMessageBus()
	sub := b.Subscribe("test-sub", "agent.a0")

	b.Publish(Message{
		Topic: "agent.a0.output",
		From:  "system",
		Type:  MsgAgentOutput,
	})

	select {
	case msg := <-sub.Ch:
		if msg.Topic != "agent.a0.output" {
			t.Fatalf("topic = %q, want %q", msg.Topic, "agent.a0.output")
		}
		if msg.Seq != 1 {
			t.Fatalf("seq = %d, want 1", msg.Seq)
		}
		if msg.Timestamp.IsZero() {
			t.Fatal("timestamp should be set")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestPublishBroadcast(t *testing.T) {
	b := NewMessageBus()
	sub := b.Subscribe("wildcard-sub", TopicAll)

	b.Publish(Message{Topic: "anything.goes", Type: "test"})

	select {
	case msg := <-sub.Ch:
		if msg.Topic != "anything.goes" {
			t.Fatalf("topic = %q, want %q", msg.Topic, "anything.goes")
		}
	case <-time.After(time.Second):
		t.Fatal("wildcard subscriber should receive all messages")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := NewMessageBus()
	sub := b.Subscribe("unsub-test", TopicAll)

	// Unsubscribe closes the channel
	b.Unsubscribe(sub.ID)

	b.Publish(Message{Topic: "test.msg", Type: "test"})

	// Channel is closed — reads return zero value immediately, no new messages delivered.
	_, ok := <-sub.Ch
	if ok {
		t.Fatal("unsubscribed subscriber channel should be closed")
	}
}

func TestPublishDropsOnFullChannel(t *testing.T) {
	b := NewMessageBus()
	sub := b.Subscribe("slow-sub", TopicAll)

	if b.Dropped() != 0 {
		t.Fatalf("initial dropped = %d, want 0", b.Dropped())
	}

	// Fill the subscriber channel (capacity = 64)
	for range 64 {
		b.Publish(Message{Topic: "fill", Type: "test"})
	}

	// Next publish should be dropped
	b.Publish(Message{Topic: "dropped", Type: "test"})

	if got := b.Dropped(); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}

	// Drain one message and verify we can receive again
	<-sub.Ch
	b.Publish(Message{Topic: "after-drain", Type: "test"})

	// Drain remaining to find the new message
	found := false
outer:
	for range 65 {
		select {
		case msg := <-sub.Ch:
			if msg.Topic == "after-drain" {
				found = true
				break outer
			}
		case <-time.After(100 * time.Millisecond):
			break outer
		}
	}
	if !found {
		t.Fatal("should receive message after draining channel")
	}
}

func TestSeqMonotonicallyIncreases(t *testing.T) {
	b := NewMessageBus()
	sub := b.Subscribe("seq-test", TopicAll)

	for range 10 {
		b.Publish(Message{Topic: "seq", Type: "test"})
	}

	var prevSeq int64
	for range 10 {
		msg := <-sub.Ch
		if msg.Seq <= prevSeq {
			t.Fatalf("seq %d <= prev %d: not monotonically increasing", msg.Seq, prevSeq)
		}
		prevSeq = msg.Seq
	}

	if got := b.Seq(); got != 10 {
		t.Fatalf("Seq() = %d, want 10", got)
	}
}

func TestMatchTopicPrefixAndExact(t *testing.T) {
	tests := []struct {
		filter string
		topic  string
		want   bool
	}{
		{TopicAll, "anything", true},
		{TopicAll, "", true},
		{"agent.a0", "agent.a0", true},
		{"agent.a0", "agent.a0.output", true},
		{"agent.a0", "agent.a1", false},
		{"agent.a0", "agent.a0x", false},
		{"system", "system", true},
		{"system", "system.health", true},
		{"system", "systemd", false},
	}

	for _, tt := range tests {
		got := matchTopic(tt.filter, tt.topic)
		if got != tt.want {
			t.Errorf("matchTopic(%q, %q) = %v, want %v", tt.filter, tt.topic, got, tt.want)
		}
	}
}

func TestSubscriberCount(t *testing.T) {
	b := NewMessageBus()
	if b.SubscriberCount() != 0 {
		t.Fatalf("initial count = %d, want 0", b.SubscriberCount())
	}

	s1 := b.Subscribe("s1", TopicAll)
	_ = b.Subscribe("s2", "agent")
	if b.SubscriberCount() != 2 {
		t.Fatalf("count = %d, want 2", b.SubscriberCount())
	}

	b.Unsubscribe(s1.ID)
	if b.SubscriberCount() != 1 {
		t.Fatalf("count after unsub = %d, want 1", b.SubscriberCount())
	}
}

func TestSetOnPublishCallback(t *testing.T) {
	b := NewMessageBus()
	var callbackMsg Message
	b.SetOnPublish(func(msg Message) {
		callbackMsg = msg
	})

	b.Publish(Message{Topic: "callback.test", Type: "test"})

	if callbackMsg.Topic != "callback.test" {
		t.Fatalf("callback topic = %q, want %q", callbackMsg.Topic, "callback.test")
	}
}

func TestTimestampPreservedWhenSet(t *testing.T) {
	b := NewMessageBus()
	sub := b.Subscribe("ts-test", TopicAll)

	fixedTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	b.Publish(Message{Topic: "ts", Type: "test", Timestamp: fixedTime})

	msg := <-sub.Ch
	if !msg.Timestamp.Equal(fixedTime) {
		t.Fatalf("timestamp = %v, want %v", msg.Timestamp, fixedTime)
	}
}
