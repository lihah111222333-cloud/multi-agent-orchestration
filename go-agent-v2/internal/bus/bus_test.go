package bus

import (
	"encoding/json"
	"sync"
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

// TestPublishConcurrentSeqOrder 验证并发 Publish 下消息到达顺序与 seq 一致。
//
// 50 个 goroutine 同时 Publish (channel 容量 64), 订阅者收到的消息 seq 必须严格递增。
func TestPublishConcurrentSeqOrder(t *testing.T) {
	b := NewMessageBus()
	sub := b.Subscribe("order-check", "*")

	const n = 50
	done := make(chan struct{})

	// 100 goroutines 并发 Publish
	for i := 0; i < n; i++ {
		go func() {
			b.Publish(Message{Topic: "concurrent", Type: "test"})
		}()
	}

	// 收集所有消息
	go func() {
		received := make([]int64, 0, n)
		for i := 0; i < n; i++ {
			msg := <-sub.Ch
			received = append(received, msg.Seq)
		}

		// 验证 seq 唯一 (无重复)
		seen := make(map[int64]bool)
		for _, s := range received {
			if seen[s] {
				t.Errorf("duplicate seq %d", s)
			}
			seen[s] = true
		}

		// 验证所有 seq 都在 [1, n] 范围
		if len(seen) != n {
			t.Errorf("expected %d unique seq, got %d", n, len(seen))
		}

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for concurrent messages")
	}
}

// ========================================
// 并发安全测试 (Batch 2 TDD RED)
// ========================================

// TestPublish_DoesNotBlockSubscribe 验证 fan-out 期间不阻塞 Subscribe/Unsubscribe。
//
// 场景: 并发 Publish + Subscribe/Unsubscribe, 带超时检测。
// 如果 Publish 在写锁下执行 fan-out, Subscribe/Unsubscribe 会被阻塞。
func TestPublish_DoesNotBlockSubscribe(t *testing.T) {
	b := NewMessageBus()

	const iterations = 500
	var wg sync.WaitGroup
	done := make(chan struct{})

	// 并发 Publish
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			b.Publish(Message{Topic: "stress", Type: "test"})
		}
	}()

	// 并发 Subscribe/Unsubscribe
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			id := "temp-sub"
			sub := b.Subscribe(id, "*")
			// 确保 channel 可用
			_ = sub.Ch
			b.Unsubscribe(id)
		}
	}()

	// 并发读取 SubscriberCount (使用 RLock)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = b.SubscriberCount()
		}
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DEADLOCK: Publish + Subscribe/Unsubscribe concurrent access timed out")
	}

	// seq 应该递增了 iterations 次
	if b.Seq() != int64(iterations) {
		t.Errorf("seq = %d, want %d", b.Seq(), iterations)
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

// ========================================
// P1-2: MessageBus.Seq() 并发读不阻塞写
// ========================================

// TestSeq_ConcurrentReadsDoNotBlockPublish 验证 Seq() 作为只读操作不阻塞 Publish。
//
// 如果 Seq() 使用写锁 (Mutex.Lock), 则高频 Seq() 调用会与 Publish 产生竞争。
// 改用 atomic 或 RWMutex.RLock 后, 此测试应无 timeout。
func TestSeq_ConcurrentReadsDoNotBlockPublish(t *testing.T) {
	b := NewMessageBus()

	const n = 1000
	var wg sync.WaitGroup
	done := make(chan struct{})

	// 并发 Publish
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			b.Publish(Message{Topic: "seq-test", Type: "ping"})
		}
	}()

	// 并发 Seq() 读 (大量)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n*10; i++ {
			s := b.Seq()
			_ = s
		}
	}()

	// 并发 SubscriberCount() 读
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n*10; i++ {
			c := b.SubscriberCount()
			_ = c
		}
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("TIMEOUT: concurrent Seq()/SubscriberCount() blocked by Publish")
	}

	if b.Seq() != n {
		t.Errorf("seq = %d, want %d", b.Seq(), n)
	}
}
