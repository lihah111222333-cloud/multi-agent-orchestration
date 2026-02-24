package codexadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestThreadExistsInHistorySkipsStoreLookupForLikelyThreadID(t *testing.T) {
	t.Helper()

	calls := 0
	got := ThreadExistsInHistory(context.Background(), ThreadExistsInHistoryOptions{
		ThreadID: "thread-1",
		IsLikelyCodexThreadID: func(_ string) bool {
			return true
		},
		FindBindingByAgentID: func(context.Context, string) (bool, error) {
			calls++
			return false, nil
		},
	})
	if !got {
		t.Fatalf("ThreadExistsInHistory() = false, want true")
	}
	if calls != 0 {
		t.Fatalf("store lookup calls = %d, want 0", calls)
	}
}

func TestThreadExistsInHistoryChecksSourcesInOrder(t *testing.T) {
	t.Helper()

	loadArchiveCalls := 0
	got := ThreadExistsInHistory(context.Background(), ThreadExistsInHistoryOptions{
		ThreadID: "agent-a",
		IsLikelyCodexThreadID: func(_ string) bool {
			return false
		},
		FindBindingByAgentID: func(context.Context, string) (bool, error) {
			return false, nil
		},
		GetAgentStatusByID: func(context.Context, string) (bool, error) {
			return true, nil
		},
		LoadThreadArchiveMap: func(context.Context) (map[string]int64, error) {
			loadArchiveCalls++
			return map[string]int64{"agent-a": 1}, nil
		},
	})
	if !got {
		t.Fatalf("ThreadExistsInHistory() = false, want true")
	}
	if loadArchiveCalls != 0 {
		t.Fatalf("archive lookup calls = %d, want 0", loadArchiveCalls)
	}
}

func TestThreadExistsInHistoryReturnsFalseOnLookupErrors(t *testing.T) {
	t.Helper()

	got := ThreadExistsInHistory(context.Background(), ThreadExistsInHistoryOptions{
		ThreadID: "agent-a",
		IsLikelyCodexThreadID: func(_ string) bool {
			return false
		},
		FindBindingByAgentID: func(context.Context, string) (bool, error) {
			return false, errors.New("binding lookup failed")
		},
		GetAgentStatusByID: func(context.Context, string) (bool, error) {
			return false, errors.New("status lookup failed")
		},
		LoadThreadArchiveMap: func(context.Context) (map[string]int64, error) {
			return nil, errors.New("archive lookup failed")
		},
	})
	if got {
		t.Fatalf("ThreadExistsInHistory() = true, want false")
	}
}

func TestResolveCodexThreadCandidatesDefaultAppendUnique(t *testing.T) {
	t.Helper()

	got := ResolveCodexThreadCandidates(context.Background(), ResolveCodexThreadCandidatesOptions{
		AgentID: "agent-a",
		FindBindingCodexThreadID: func(context.Context, string) (string, error) {
			return "codex-1", nil
		},
		FindStatusSessionID: func(context.Context, string) (string, error) {
			return "codex-1", nil
		},
		PreviewCandidates: func(ids []string, _ int) []string {
			return ids
		},
	})
	want := []string{"agent-a", "codex-1"}
	if len(got) != len(want) {
		t.Fatalf("len(candidates) = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestResolveCodexThreadCandidatesUsesInjectedAppendUnique(t *testing.T) {
	t.Helper()

	appendUUIDOnly := func(dst []string, seen map[string]struct{}, candidate string) []string {
		value := strings.TrimSpace(strings.ToLower(candidate))
		value = strings.TrimPrefix(value, "urn:uuid:")
		if len(value) != 36 || strings.Count(value, "-") != 4 {
			return dst
		}
		if _, ok := seen[value]; ok {
			return dst
		}
		seen[value] = struct{}{}
		return append(dst, value)
	}

	got := ResolveCodexThreadCandidates(context.Background(), ResolveCodexThreadCandidatesOptions{
		AgentID:              "agent-a",
		AppendUniqueThreadID: appendUUIDOnly,
		FindBindingCodexThreadID: func(context.Context, string) (string, error) {
			return "URN:UUID:550e8400-e29b-41d4-a716-446655440000", nil
		},
		FindStatusSessionID: func(context.Context, string) (string, error) {
			return "550e8400-E29B-41D4-a716-446655440000", nil
		},
		PreviewCandidates: func(ids []string, _ int) []string {
			return ids
		},
	})
	if len(got) != 1 || got[0] != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("candidates = %v, want one normalized uuid", got)
	}
}
