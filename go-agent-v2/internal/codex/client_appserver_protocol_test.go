package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildTurnInterruptParams(t *testing.T) {
	t.Run("with_turn_id", func(t *testing.T) {
		params := buildTurnInterruptParams("thread-1", "turn-1", "with_turn_id")

		threadID, ok := params["threadId"].(string)
		if !ok || threadID != "thread-1" {
			t.Fatalf("threadId mismatch: %#v", params["threadId"])
		}
		turnID, ok := params["turnId"].(string)
		if !ok || turnID != "turn-1" {
			t.Fatalf("turnId mismatch: %#v", params["turnId"])
		}
	})

	t.Run("thread_scoped_always_includes_turn_id", func(t *testing.T) {
		params := buildTurnInterruptParams("thread-1", "turn-1", "thread_scoped")

		turnID, ok := params["turnId"].(string)
		if !ok {
			t.Fatalf("turnId missing in params: %#v", params)
		}
		if turnID != "" {
			t.Fatalf("expected empty turnId in thread_scoped mode, got %q", turnID)
		}
	})
}

func TestIsRPCTimeoutError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "timeout_with_method", err: testError("turn/interrupt timeout"), want: true},
		{name: "suffix_timeout", err: testError("timeout"), want: true},
		{name: "other", err: testError("code -32601 method not found"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRPCTimeoutError(tc.err); got != tc.want {
				t.Fatalf("isRPCTimeoutError()=%v, want %v (err=%v)", got, tc.want, tc.err)
			}
		})
	}
}

func TestEnsureListenerWithAutoInitializeRetriesAfterNotInitialized(t *testing.T) {
	calls := 0
	initCalls := 0
	rpcCall := func(method string, params any, timeout time.Duration) (json.RawMessage, error) {
		_ = method
		_ = params
		_ = timeout
		calls++
		if calls == 1 {
			return nil, testError("rpc error: Not initialized (code -32600)")
		}
		return json.RawMessage(`{"thread":{"id":"thread-1"}}`), nil
	}
	initFn := func() error {
		initCalls++
		return nil
	}

	resolvedID, retriedAfterInit, err := ensureListenerWithAutoInitialize("thread-1", rpcCall, initFn)
	if err != nil {
		t.Fatalf("ensureListenerWithAutoInitialize() err = %v", err)
	}
	if resolvedID != "thread-1" {
		t.Fatalf("resolvedID = %q, want thread-1", resolvedID)
	}
	if !retriedAfterInit {
		t.Fatal("retriedAfterInit = false, want true")
	}
	if calls != 2 {
		t.Fatalf("rpc calls = %d, want 2", calls)
	}
	if initCalls != 1 {
		t.Fatalf("initialize calls = %d, want 1", initCalls)
	}
}

func TestEnsureListenerWithAutoInitializeInitFailure(t *testing.T) {
	calls := 0
	rpcCall := func(method string, params any, timeout time.Duration) (json.RawMessage, error) {
		_ = method
		_ = params
		_ = timeout
		calls++
		return nil, testError("rpc error: Not initialized (code -32600)")
	}
	initFn := func() error { return testError("init failed") }

	_, retriedAfterInit, err := ensureListenerWithAutoInitialize("thread-1", rpcCall, initFn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !retriedAfterInit {
		t.Fatal("retriedAfterInit = false, want true")
	}
	if !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("error = %q, want contains initialize", err.Error())
	}
	if calls != 1 {
		t.Fatalf("rpc calls = %d, want 1", calls)
	}
}

func TestCallWithNotInitializedRecovery(t *testing.T) {
	t.Run("retry_after_initialize", func(t *testing.T) {
		calls := 0
		initCalls := 0
		rpcCall := func(method string, params any, timeout time.Duration) (json.RawMessage, error) {
			_ = method
			_ = params
			_ = timeout
			calls++
			if calls == 1 {
				return nil, testError("rpc error: Not initialized (code -32600)")
			}
			return json.RawMessage(`{"ok":true}`), nil
		}
		initializeFn := func() error {
			initCalls++
			return nil
		}

		result, recovered, err := callWithNotInitializedRecovery(rpcCall, initializeFn, "turn/interrupt", nil, time.Second)
		if err != nil {
			t.Fatalf("callWithNotInitializedRecovery() err = %v", err)
		}
		if !recovered {
			t.Fatal("recovered = false, want true")
		}
		if string(result) != `{"ok":true}` {
			t.Fatalf("unexpected result: %s", string(result))
		}
		if calls != 2 {
			t.Fatalf("rpc calls = %d, want 2", calls)
		}
		if initCalls != 1 {
			t.Fatalf("initialize calls = %d, want 1", initCalls)
		}
	})

	t.Run("initialize_failure", func(t *testing.T) {
		calls := 0
		rpcCall := func(method string, params any, timeout time.Duration) (json.RawMessage, error) {
			_ = method
			_ = params
			_ = timeout
			calls++
			return nil, testError("rpc error: Not initialized (code -32600)")
		}
		initializeFn := func() error { return testError("init failed") }

		_, recovered, err := callWithNotInitializedRecovery(rpcCall, initializeFn, "turn/interrupt", nil, time.Second)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !recovered {
			t.Fatal("recovered = false, want true")
		}
		if !strings.Contains(err.Error(), "initialize") {
			t.Fatalf("error = %q, want contains initialize", err.Error())
		}
		if calls != 1 {
			t.Fatalf("rpc calls = %d, want 1", calls)
		}
	})

	t.Run("non_not_initialized_no_retry", func(t *testing.T) {
		calls := 0
		rpcCall := func(method string, params any, timeout time.Duration) (json.RawMessage, error) {
			_ = method
			_ = params
			_ = timeout
			calls++
			return nil, testError("rpc error: code -32601 method not found")
		}
		initializeFn := func() error {
			t.Fatal("initialize should not be called")
			return nil
		}

		_, recovered, err := callWithNotInitializedRecovery(rpcCall, initializeFn, "turn/interrupt", nil, time.Second)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if recovered {
			t.Fatal("recovered = true, want false")
		}
		if calls != 1 {
			t.Fatalf("rpc calls = %d, want 1", calls)
		}
	})
}

type testError string

func (e testError) Error() string { return string(e) }
