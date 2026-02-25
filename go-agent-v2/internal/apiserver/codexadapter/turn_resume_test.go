package codexadapter

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildResumeCandidates(t *testing.T) {
	t.Run("normalize thread id first", func(t *testing.T) {
		got := BuildResumeCandidates("  thread-1  ", []string{"legacy-1"}, func(id string) string {
			if strings.TrimSpace(id) == "thread-1" {
				return "normalized-thread-1"
			}
			return ""
		})
		if len(got) != 1 || got[0] != "normalized-thread-1" {
			t.Fatalf("BuildResumeCandidates() = %v, want [normalized-thread-1]", got)
		}
	})

	t.Run("fallback to resolved deduped list", func(t *testing.T) {
		got := BuildResumeCandidates("thread-1", []string{" a ", "a", "b", " "}, nil)
		want := []string{"a", "b"}
		if len(got) != len(want) {
			t.Fatalf("BuildResumeCandidates() len = %d, want %d (got=%v)", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("BuildResumeCandidates()[%d] = %q, want %q (got=%v)", i, got[i], want[i], got)
			}
		}
	})

	t.Run("fallback to thread id when resolved empty", func(t *testing.T) {
		got := BuildResumeCandidates("thread-1", nil, nil)
		if len(got) != 1 || got[0] != "thread-1" {
			t.Fatalf("BuildResumeCandidates() = %v, want [thread-1]", got)
		}
	})
}

func TestTryResumeCandidates(t *testing.T) {
	t.Run("empty candidates", func(t *testing.T) {
		_, err := TryResumeCandidates(nil, "thread-1", func(string) error { return nil }, nil)
		if err == nil || !strings.Contains(err.Error(), "no resume candidates available") {
			t.Fatalf("TryResumeCandidates() err = %v, want no resume candidates", err)
		}
	})

	t.Run("skip candidate error and succeed later", func(t *testing.T) {
		attempts := []string{}
		got, err := TryResumeCandidates(
			[]string{"bad", "good"},
			"thread-1",
			func(id string) error {
				attempts = append(attempts, id)
				if id == "bad" {
					return errors.New("no rollout found for thread id")
				}
				return nil
			},
			nil,
		)
		if err != nil {
			t.Fatalf("TryResumeCandidates() err = %v, want nil", err)
		}
		if got != "good" {
			t.Fatalf("TryResumeCandidates() got = %q, want good", got)
		}
		if len(attempts) != 2 || attempts[0] != "bad" || attempts[1] != "good" {
			t.Fatalf("TryResumeCandidates() attempts = %v, want [bad good]", attempts)
		}
	})

	t.Run("stop on non-candidate error", func(t *testing.T) {
		wantErr := errors.New("permission denied")
		_, err := TryResumeCandidates(
			[]string{"first", "second"},
			"thread-1",
			func(id string) error {
				if id == "first" {
					return wantErr
				}
				return nil
			},
			nil,
		)
		if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
			t.Fatalf("TryResumeCandidates() err = %v, want %v", err, wantErr)
		}
	})
}
