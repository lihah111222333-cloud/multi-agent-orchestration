package rollout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
)

func TestParseRolloutTimestamp(t *testing.T) {
	t.Run("rfc3339nano", func(t *testing.T) {
		got := ParseRolloutTimestamp("2026-02-27T01:02:03.123456789Z")
		if got.IsZero() {
			t.Fatalf("ParseRolloutTimestamp() = zero, want parsed value")
		}
		if got.UTC().Format(time.RFC3339Nano) != "2026-02-27T01:02:03.123456789Z" {
			t.Fatalf("ParseRolloutTimestamp() = %s, want exact RFC3339Nano", got.UTC().Format(time.RFC3339Nano))
		}
	})

	t.Run("invalid returns zero", func(t *testing.T) {
		if got := ParseRolloutTimestamp("not-a-time"); !got.IsZero() {
			t.Fatalf("ParseRolloutTimestamp() = %v, want zero", got)
		}
	})
}

func TestPaginateRolloutMessages(t *testing.T) {
	all := []ThreadHistoryMessage{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}

	got := PaginateRolloutMessages(all, 2, 0)
	if len(got) != 2 || got[0].ID != 4 || got[1].ID != 3 {
		t.Fatalf("PaginateRolloutMessages(limit=2,before=0) = %v, want IDs [4,3]", got)
	}

	got = PaginateRolloutMessages(all, 1000, 4)
	if len(got) != 3 || got[0].ID != 3 || got[2].ID != 1 {
		t.Fatalf("PaginateRolloutMessages(limit=1000,before=4) = %v, want IDs [3,2,1]", got)
	}
}

func TestRunningCodexThreadIDFromManager(t *testing.T) {
	if got := RunningCodexThreadIDFromManager("thread-1", nil, nil); got != "" {
		t.Fatalf("RunningCodexThreadIDFromManager(nil,nil) = %q, want empty", got)
	}
	if got := RunningCodexThreadIDFromManager("thread-1", func(string) any { return nil }, func(any) string { return "x" }); got != "" {
		t.Fatalf("RunningCodexThreadIDFromManager(nil process) = %q, want empty", got)
	}
	got := RunningCodexThreadIDFromManager("thread-1", func(string) any { return struct{}{} }, func(any) string { return "codex-123" })
	if got != "codex-123" {
		t.Fatalf("RunningCodexThreadIDFromManager() = %q, want codex-123", got)
	}
}

func TestResolveRolloutHistorySource(t *testing.T) {
	t.Run("prefer running thread id", func(t *testing.T) {
		id, path := ResolveRolloutHistorySource(
			context.Background(),
			" agent-a ",
			func(string) string { return " running-id " },
			nil,
			nil,
			nil,
		)
		if id != "running-id" || path != "" {
			t.Fatalf("ResolveRolloutHistorySource() = (%q,%q), want running-id with empty path", id, path)
		}
	})

	t.Run("fallback binding then status then thread id", func(t *testing.T) {
		id, path := ResolveRolloutHistorySource(
			context.Background(),
			"agent-b",
			func(string) string { return "" },
			func(context.Context, string) (string, string, error) { return "", "", errors.New("binding down") },
			func(context.Context, string) (string, error) { return " status-id ", nil },
			func(v string) string { return v },
		)
		if id != " status-id " || path != "" {
			t.Fatalf("ResolveRolloutHistorySource() = (%q,%q), want status-id with empty path", id, path)
		}

		id, path = ResolveRolloutHistorySource(
			context.Background(),
			"  agent-c  ",
			func(string) string { return "" },
			func(context.Context, string) (string, string, error) { return "", "", nil },
			func(context.Context, string) (string, error) { return "", nil },
			nil,
		)
		if id != "agent-c" || path != "" {
			t.Fatalf("ResolveRolloutHistorySource() fallback = (%q,%q), want agent-c with empty path", id, path)
		}
	})
}

func TestLoadAllThreadMessagesFromCodexRollout(t *testing.T) {
	t.Run("load and convert rollout messages", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "rollout.jsonl")
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write rollout file: %v", err)
		}

		trimInjectedArg := false
		got, err := LoadAllThreadMessagesFromCodexRollout(
			context.Background(),
			"thread-1",
			func(context.Context, string) (string, string) { return " codex-1 ", path },
			func(s string) string { return s },
			nil,
			nil,
			func(_ string, trimInjected bool) ([]codex.RolloutMessage, error) {
				trimInjectedArg = trimInjected
				return []codex.RolloutMessage{
					{Role: "user", Content: "u1", Timestamp: "2026-02-27T01:00:00Z"},
					{Role: "assistant", Content: "a1", Timestamp: "2026-02-27T01:01:00Z"},
					{Role: "system", Content: "skip", Timestamp: "2026-02-27T01:02:00Z"},
				}, nil
			},
			false,
		)
		if err != nil {
			t.Fatalf("LoadAllThreadMessagesFromCodexRollout() err = %v, want nil", err)
		}
		if !trimInjectedArg {
			t.Fatalf("readRollout trimInjected arg = false, want true when showInjectedPromptInChat=false")
		}
		if len(got) != 2 {
			t.Fatalf("len(messages) = %d, want 2 (got=%v)", len(got), got)
		}
		if got[0].ID != 1 || got[0].AgentID != "thread-1" || got[0].Role != "user" {
			t.Fatalf("messages[0] = %+v, want user message mapped from rollout", got[0])
		}
		if got[1].EventType != agentcore.EventAgentMessage {
			t.Fatalf("messages[1].EventType = %q, want %q", got[1].EventType, agentcore.EventAgentMessage)
		}
	})

	t.Run("find rollout failure returns empty without error", func(t *testing.T) {
		readCalled := false
		got, err := LoadAllThreadMessagesFromCodexRollout(
			context.Background(),
			"thread-2",
			func(context.Context, string) (string, string) { return "codex-2", "" },
			nil,
			func(string) (string, error) { return "", errors.New("not found") },
			nil,
			func(string, bool) ([]codex.RolloutMessage, error) {
				readCalled = true
				return nil, nil
			},
			true,
		)
		if err != nil {
			t.Fatalf("LoadAllThreadMessagesFromCodexRollout() err = %v, want nil", err)
		}
		if len(got) != 0 {
			t.Fatalf("LoadAllThreadMessagesFromCodexRollout() len = %d, want 0", len(got))
		}
		if readCalled {
			t.Fatalf("readRolloutMessagesWithTrim should not be called when findRolloutPath fails")
		}
	})
}
