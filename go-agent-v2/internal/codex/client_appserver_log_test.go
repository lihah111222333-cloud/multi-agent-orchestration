package codex

import (
	"errors"
	"testing"
)

// ── Fix 2: readLoop shutdown 时 readError 应降级为 DEBUG ──

func TestIsShutdownReadError_ClosedConnection(t *testing.T) {
	err := errors.New("read tcp 127.0.0.1:64133->127.0.0.1:63909: use of closed network connection")
	if !isShutdownReadError(err) {
		t.Fatal("expected 'use of closed network connection' to be recognized as shutdown read error")
	}
}

func TestIsShutdownReadError_NormalError(t *testing.T) {
	err := errors.New("read tcp broken pipe")
	if isShutdownReadError(err) {
		t.Fatal("normal read error should not be recognized as shutdown read error")
	}
}

func TestIsShutdownReadError_Nil(t *testing.T) {
	if isShutdownReadError(nil) {
		t.Fatal("nil error should not be recognized as shutdown read error")
	}
}

// ── Fix 3: legacy mirror 日志应降级为 DEBUG + 采样 ──

func TestShouldLogLegacyMirrorDrop_FirstOccurrence(t *testing.T) {
	// 第 1 次应记录（采样策略: 首次 + 每 N 次）
	if !shouldLogLegacyMirrorDrop(1) {
		t.Fatal("first legacy mirror drop should be logged")
	}
}

func TestShouldLogLegacyMirrorDrop_MiddleOccurrences(t *testing.T) {
	// 中间普通计数应不记录
	if shouldLogLegacyMirrorDrop(50) {
		t.Fatal("middle occurrence should not be logged")
	}
}

func TestShouldLogLegacyMirrorDrop_SampledOccurrence(t *testing.T) {
	// 每 100 次应记录
	if !shouldLogLegacyMirrorDrop(100) {
		t.Fatal("every 100th occurrence should be logged")
	}
	if !shouldLogLegacyMirrorDrop(200) {
		t.Fatal("every 100th occurrence should be logged")
	}
}

// ── Bug 7 (TDD): truncateString UTF-8 安全截断 ──

func TestTruncateString_UTF8Safety(t *testing.T) {
	// 5 个中文字符 = 15 字节, 截断到 3 rune 应保持完整
	input := "你好世界啊"
	got := truncateString(input, 3)
	want := "你好世...(truncated)"
	if got != want {
		t.Errorf("truncateString(%q, 3) = %q, want %q", input, got, want)
	}
}

func TestTruncateString_NoTruncateWhenShort(t *testing.T) {
	input := "hello"
	got := truncateString(input, 10)
	if got != input {
		t.Errorf("truncateString(%q, 10) = %q, want %q", input, got, input)
	}
}

func TestTruncateString_ZeroMaxReturnsOriginal(t *testing.T) {
	input := "hello"
	got := truncateString(input, 0)
	if got != input {
		t.Errorf("truncateString(%q, 0) = %q, want %q", input, got, input)
	}
}
