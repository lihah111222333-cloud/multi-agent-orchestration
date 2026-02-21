package apiserver

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/runner"
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

// ========================================
// Fix 1: broadcastNotification marshal 失败必须记日志
// ========================================

// unmarshalable 是一个 json.Marshal 总是失败的类型。
type unmarshalable struct{}

func (unmarshalable) MarshalJSON() ([]byte, error) {
	return nil, &json.MarshalerError{Type: nil, Err: nil}
}

func TestBroadcastNotification_MarshalError_LogsError(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	s := &Server{
		conns: map[string]*connEntry{},
	}

	// 用一个无法序列化的 params 触发 marshal 失败
	s.broadcastNotification("test/method", unmarshalable{})

	logOutput := buf.String()
	if !strings.Contains(logOutput, "marshal notification failed") {
		t.Fatalf("expected 'marshal notification failed' in log, got:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "test/method") {
		t.Fatalf("expected method name in log, got:\n%s", logOutput)
	}
}

// ========================================
// Fix 3: sendSlashCommandWithArgs threadId unmarshal 失败必须返回错误
// ========================================

func TestSendSlashCommandWithArgs_BadThreadID_ReturnsError(t *testing.T) {
	s := &Server{
		mgr: runner.NewAgentManager(),
	}

	// threadId 字段包含无效 JSON 值 (数字代替字符串)
	params := json.RawMessage(`{"threadId": 12345}`)
	_, err := s.sendSlashCommandWithArgs(params, "/test", "args")
	if err == nil {
		t.Fatal("expected error when threadId unmarshal fails, got nil")
	}
	if !strings.Contains(err.Error(), "threadId") {
		t.Fatalf("expected error mentioning 'threadId', got: %s", err.Error())
	}
}

// ========================================
// Fix 4: DenyFunc/RespondFunc 失败必须记日志
// ========================================

func TestHandleApprovalRequest_DenyFuncError_LogsWarn(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	s := &Server{
		mgr:     nil, // mgr==nil → 走 deny 路径
		conns:   map[string]*connEntry{},
		pending: make(map[int64]chan *Response), // Wails 模式需要
	}

	event := codex.Event{
		Type: "exec_approval_request",
		DenyFunc: func() error {
			return &json.MarshalerError{} // 模拟 deny 失败
		},
	}

	s.handleApprovalRequest("agent-1", "item/commandExecution/requestApproval", nil, event)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "deny") {
		t.Fatalf("expected 'deny' related log when DenyFunc fails, got:\n%s", logOutput)
	}
}
