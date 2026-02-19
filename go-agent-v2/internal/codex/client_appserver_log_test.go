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
