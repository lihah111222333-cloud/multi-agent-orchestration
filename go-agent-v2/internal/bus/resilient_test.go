package bus

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// captureLog 将 pkg/logger 默认日志器重定向到 buffer, 返回 buffer 和恢复函数。
func captureLog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	prev := logger.Get()
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger.SetForTest(slog.New(h))
	return &buf, func() { logger.SetForTest(prev) }
}

// errStore 是一个 LoadPending 总是失败的 FallbackStore mock。
type errStore struct{}

func (errStore) SavePending(_ context.Context, _ Message) error { return nil }
func (errStore) LoadPending(_ context.Context, _ int) ([]Message, error) {
	return nil, errors.New("db connection lost")
}
func (errStore) DeletePending(_ context.Context, _ int64) error { return nil }

// ========================================
// Fix 2: recoverPending LoadPending 失败必须记日志
// ========================================

func TestRecoverPending_LoadError_LogsWarn(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	bus := NewMessageBus()
	rp := NewResilientPublisher(bus, errStore{})

	rp.recoverPending(context.Background())

	logOutput := buf.String()
	if !strings.Contains(logOutput, "load pending failed") {
		t.Fatalf("expected 'load pending failed' in log, got:\n%s", logOutput)
	}
}

// ========================================
// P1-1: PublishTo / PublishFrom 共享 publishInternal
// ========================================

// TestPublishTo_DeliversCorrectTopicAndFrom 验证 PublishTo 构造正确的 topic 和 from。
func TestPublishTo_DeliversCorrectTopicAndFrom(t *testing.T) {
	bus := NewMessageBus()
	sub := bus.Subscribe("test-sub", "*")
	rp := NewResilientPublisher(bus, errStore{})

	type payload struct {
		Key string `json:"key"`
	}
	rp.PublishTo(TopicDAG, "run-1", MsgDAGNodeStart, payload{Key: "v1"})

	select {
	case msg := <-sub.Ch:
		if msg.Topic != "dag.run-1" {
			t.Errorf("topic = %q, want %q", msg.Topic, "dag.run-1")
		}
		if msg.From != "system" {
			t.Errorf("from = %q, want %q", msg.From, "system")
		}
		if msg.Type != MsgDAGNodeStart {
			t.Errorf("type = %q, want %q", msg.Type, MsgDAGNodeStart)
		}
	case <-timeoutCh():
		t.Fatal("timeout waiting for PublishTo message")
	}
}

// TestPublishFrom_DeliversCorrectFrom 验证 PublishFrom 设置正确的 from 字段。
func TestPublishFrom_DeliversCorrectFrom(t *testing.T) {
	bus := NewMessageBus()
	sub := bus.Subscribe("test-sub", "*")
	rp := NewResilientPublisher(bus, errStore{})

	rp.PublishFrom(TopicApproval, "req-1", "agent-007", MsgApprovalRequest, map[string]string{"action": "deploy"})

	select {
	case msg := <-sub.Ch:
		if msg.Topic != "approval.req-1" {
			t.Errorf("topic = %q, want %q", msg.Topic, "approval.req-1")
		}
		if msg.From != "agent-007" {
			t.Errorf("from = %q, want %q", msg.From, "agent-007")
		}
		if msg.Type != MsgApprovalRequest {
			t.Errorf("type = %q, want %q", msg.Type, MsgApprovalRequest)
		}
	case <-timeoutCh():
		t.Fatal("timeout waiting for PublishFrom message")
	}
}

// TestPublishTo_NilPayload_DoesNotPanic 验证 nil payload 不 panic。
func TestPublishTo_NilPayload_DoesNotPanic(t *testing.T) {
	bus := NewMessageBus()
	sub := bus.Subscribe("test-sub", "*")
	rp := NewResilientPublisher(bus, errStore{})

	// nil payload 应该不 panic
	rp.PublishTo(TopicTask, "t1", MsgTaskComplete, nil)

	select {
	case msg := <-sub.Ch:
		if msg.Topic != "task.t1" {
			t.Errorf("topic = %q, want %q", msg.Topic, "task.t1")
		}
	case <-timeoutCh():
		t.Fatal("timeout waiting for nil payload message")
	}
}

func timeoutCh() <-chan time.Time {
	return time.After(200 * time.Millisecond)
}
