package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
		if recovered {
			t.Fatal("recovered = true, want false")
		}
		if !strings.Contains(err.Error(), "initialize") {
			t.Fatalf("error = %q, want contains initialize", err.Error())
		}
		if calls != 1 {
			t.Fatalf("rpc calls = %d, want 1", calls)
		}
	})

	t.Run("retry_failure_not_recovered", func(t *testing.T) {
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
			return nil, testError("rpc error: timeout")
		}
		initializeFn := func() error {
			initCalls++
			return nil
		}

		_, recovered, err := callWithNotInitializedRecovery(rpcCall, initializeFn, "turn/interrupt", nil, time.Second)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if recovered {
			t.Fatal("recovered = true, want false")
		}
		if calls != 2 {
			t.Fatalf("rpc calls = %d, want 2", calls)
		}
		if initCalls != 1 {
			t.Fatalf("initialize calls = %d, want 1", initCalls)
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

func TestSubmitTurnStartNotInitializedRecovery(t *testing.T) {
	var (
		methodsMu sync.Mutex
		methods   []string
	)
	errCh := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("upgrade failed: %w", err):
			default:
			}
			return
		}
		defer conn.Close()

		initialized := false
		writeResponse := func(msg jsonRPCMessage, result any, rpcErr *jsonRPCError) bool {
			if rpcErr != nil {
				resp := struct {
					JSONRPC string        `json:"jsonrpc"`
					ID      jsonRPCID     `json:"id"`
					Error   *jsonRPCError `json:"error"`
				}{
					JSONRPC: "2.0",
					ID:      msg.ID.clone(),
					Error:   rpcErr,
				}
				if err := conn.WriteJSON(resp); err != nil {
					select {
					case errCh <- fmt.Errorf("write error response failed: %w", err):
					default:
					}
					return false
				}
				return true
			}

			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      msg.ID.clone(),
				Result:  result,
			}
			if err := conn.WriteJSON(resp); err != nil {
				select {
				case errCh <- fmt.Errorf("write result response failed: %w", err):
				default:
				}
				return false
			}
			return true
		}

		for {
			var msg jsonRPCMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.ID == nil || strings.TrimSpace(msg.Method) == "" {
				continue
			}
			methodsMu.Lock()
			methods = append(methods, msg.Method)
			methodsMu.Unlock()

			switch msg.Method {
			case "turn/start":
				if !initialized {
					if !writeResponse(msg, nil, &jsonRPCError{Code: -32600, Message: "Not initialized"}) {
						return
					}
					continue
				}
				if !writeResponse(msg, map[string]any{
					"turn": map[string]any{
						"id": "turn-submit-1",
					},
				}, nil) {
					return
				}
			case "initialize":
				initialized = true
				if !writeResponse(msg, map[string]any{}, nil) {
					return
				}
			default:
				if !writeResponse(msg, map[string]any{}, nil) {
					return
				}
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &AppServerClient{
		AgentID:  "agent-1",
		ThreadID: "thread-1",
		ws:       conn,
		wsDone:   make(chan struct{}),
		ctx:      ctx,
	}

	readDone := make(chan struct{})
	go func() {
		c.readLoop()
		close(readDone)
	}()

	if err := c.Submit("hello", nil, nil, nil); err != nil {
		t.Fatalf("Submit() err = %v", err)
	}
	if got := c.getActiveTurnID(); got != "turn-submit-1" {
		t.Fatalf("active turn id = %q, want turn-submit-1", got)
	}

	methodsMu.Lock()
	gotMethods := append([]string(nil), methods...)
	methodsMu.Unlock()
	wantMethods := []string{"turn/start", "initialize", "turn/start"}
	if !reflect.DeepEqual(gotMethods, wantMethods) {
		t.Fatalf("rpc methods = %#v, want %#v", gotMethods, wantMethods)
	}

	select {
	case serverErr := <-errCh:
		t.Fatalf("server error: %v", serverErr)
	default:
	}

	c.stopped.Store(true)
	cancel()
	_ = conn.Close()
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit")
	}
}

type testError string

func (e testError) Error() string { return string(e) }
