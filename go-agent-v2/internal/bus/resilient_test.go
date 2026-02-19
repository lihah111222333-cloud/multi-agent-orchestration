package bus

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

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
