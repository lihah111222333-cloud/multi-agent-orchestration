package codexadapter

import (
	"errors"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/runner"
)

// ========================================
// E1: withThreadTyped — 泛型 WithThread 包装器
// ========================================

type fakeResult struct {
	Value string
}

func TestWithThreadTyped_Success(t *testing.T) {
	mockWithThread := func(id string, fn func(*runner.AgentProcess) (any, error)) (any, error) {
		return fn(nil) // proc not needed for test
	}
	result, err := withThreadTyped("thread-1", mockWithThread, "Test.success",
		func(proc *runner.AgentProcess) (fakeResult, error) {
			return fakeResult{Value: "ok"}, nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value != "ok" {
		t.Errorf("got %q, want %q", result.Value, "ok")
	}
}

func TestWithThreadTyped_EmptyThreadID(t *testing.T) {
	mockWithThread := func(id string, fn func(*runner.AgentProcess) (any, error)) (any, error) {
		t.Fatal("should not be called")
		return nil, nil
	}
	_, err := withThreadTyped("", mockWithThread, "Test.empty",
		func(proc *runner.AgentProcess) (fakeResult, error) {
			return fakeResult{}, nil
		})
	if err == nil {
		t.Fatal("expected error for empty threadID")
	}
	if got := err.Error(); !containsSubstr(got, "threadId is required") {
		t.Errorf("error should mention 'threadId is required', got: %s", got)
	}
}

func TestWithThreadTyped_WhitespaceThreadID(t *testing.T) {
	_, err := withThreadTyped("   ", nil, "Test.ws",
		func(proc *runner.AgentProcess) (fakeResult, error) {
			return fakeResult{}, nil
		})
	if err == nil {
		t.Fatal("expected error for whitespace-only threadID")
	}
}

func TestWithThreadTyped_NilWithThread(t *testing.T) {
	_, err := withThreadTyped("thread-1", nil, "Test.nil",
		func(proc *runner.AgentProcess) (fakeResult, error) {
			return fakeResult{}, nil
		})
	if err == nil {
		t.Fatal("expected error for nil WithThread")
	}
	if got := err.Error(); !containsSubstr(got, "thread resolver is not configured") {
		t.Errorf("error should mention 'thread resolver is not configured', got: %s", got)
	}
}

func TestWithThreadTyped_FnError(t *testing.T) {
	mockWithThread := func(id string, fn func(*runner.AgentProcess) (any, error)) (any, error) {
		return fn(nil)
	}
	_, err := withThreadTyped("thread-1", mockWithThread, "Test.fnErr",
		func(proc *runner.AgentProcess) (fakeResult, error) {
			return fakeResult{}, errors.New("inner error")
		})
	if err == nil {
		t.Fatal("expected error from inner function")
	}
	if got := err.Error(); !containsSubstr(got, "inner error") {
		t.Errorf("error should propagate, got: %s", got)
	}
}

func TestWithThreadTyped_MapResult(t *testing.T) {
	mockWithThread := func(id string, fn func(*runner.AgentProcess) (any, error)) (any, error) {
		return fn(nil)
	}
	result, err := withThreadTyped("thread-1", mockWithThread, "Test.map",
		func(proc *runner.AgentProcess) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["ok"] != true {
		t.Errorf("got %v, want map with ok=true", result)
	}
}

func TestWithThreadTyped_TrimsThreadID(t *testing.T) {
	var receivedID string
	mockWithThread := func(id string, fn func(*runner.AgentProcess) (any, error)) (any, error) {
		receivedID = id
		return fn(nil)
	}
	_, _ = withThreadTyped("  thread-1  ", mockWithThread, "Test.trim",
		func(proc *runner.AgentProcess) (fakeResult, error) {
			return fakeResult{}, nil
		})
	if receivedID != "thread-1" {
		t.Errorf("threadID not trimmed: got %q, want %q", receivedID, "thread-1")
	}
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
