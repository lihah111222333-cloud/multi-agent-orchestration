package codex

import "testing"

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

type testError string

func (e testError) Error() string { return string(e) }
