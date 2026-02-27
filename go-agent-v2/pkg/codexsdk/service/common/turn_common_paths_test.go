package common

import (
	"strings"
	"testing"
)

func TestCollectTrimmedUniqueValues(t *testing.T) {
	got := CollectTrimmedUniqueValues([]string{"  alpha  ", "", "alpha", " beta", "beta "}, nil)
	want := []string{"alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q (got=%v)", i, got[i], want[i], got)
		}
	}
}

func TestCollectTrimmedUniqueValuesWithKeyFn(t *testing.T) {
	got := CollectTrimmedUniqueValues([]string{" Alice ", "ALICE", "bob", " BOB "}, strings.ToLower)
	want := []string{"Alice", "bob"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q (got=%v)", i, got[i], want[i], got)
		}
	}
}

func TestCollectTrimmedUniqueValuesReturnsNilForEmptyOutput(t *testing.T) {
	if got := CollectTrimmedUniqueValues([]string{" ", "\t"}, nil); got != nil {
		t.Fatalf("CollectTrimmedUniqueValues() = %v, want nil", got)
	}
}

func TestRequireThreadID(t *testing.T) {
	if _, err := RequireThreadID("caller", "   "); err == nil {
		t.Fatalf("RequireThreadID() err = nil, want non-nil")
	}
	got, err := RequireThreadID("caller", "  thread-1  ")
	if err != nil {
		t.Fatalf("RequireThreadID() err = %v, want nil", err)
	}
	if got != "thread-1" {
		t.Fatalf("RequireThreadID() = %q, want thread-1", got)
	}
}

func TestAppendUniqueThreadIDFallback(t *testing.T) {
	seen := map[string]struct{}{"existing": {}}
	got := AppendUniqueThreadIDFallback([]string{"existing"}, seen, "  next-id  ")
	if len(got) != 2 || got[1] != "next-id" {
		t.Fatalf("AppendUniqueThreadIDFallback() = %v, want appended next-id", got)
	}
	got = AppendUniqueThreadIDFallback(got, seen, "next-id")
	if len(got) != 2 {
		t.Fatalf("AppendUniqueThreadIDFallback() duplicate append, got=%v", got)
	}
	got = AppendUniqueThreadIDFallback(got, nil, " ")
	if len(got) != 2 {
		t.Fatalf("AppendUniqueThreadIDFallback() empty candidate changed result, got=%v", got)
	}
}
