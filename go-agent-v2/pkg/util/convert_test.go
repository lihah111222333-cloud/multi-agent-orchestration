package util

import (
	"fmt"
	"testing"
)

// ========================================
// D1: AsString — 统一 any→string 转换
// ========================================

func TestAsString_String(t *testing.T) {
	if got := AsString("hello"); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestAsString_StringWithSpaces(t *testing.T) {
	if got := AsString("  hello  "); got != "hello" {
		t.Errorf("got %q, want %q — should trim", got, "hello")
	}
}

func TestAsString_Nil(t *testing.T) {
	if got := AsString(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestAsString_Int(t *testing.T) {
	// Non-string types should return ""
	if got := AsString(42); got != "" {
		t.Errorf("got %q, want empty for non-string", got)
	}
}

type testStringer struct{}

func (t testStringer) String() string { return "stringer-output" }

func TestAsString_Stringer(t *testing.T) {
	if got := AsString(testStringer{}); got != "stringer-output" {
		t.Errorf("got %q, want %q", got, "stringer-output")
	}
}

// ========================================
// D1: AsStringSlice — 统一 any→[]string 转换
// ========================================

func TestAsStringSlice_StringSlice(t *testing.T) {
	input := []string{"a", "b", "c"}
	got := AsStringSlice(input)
	assertStrSlice(t, got, []string{"a", "b", "c"})
}

func TestAsStringSlice_StringSliceTrimsEmpty(t *testing.T) {
	input := []string{"a", "", "  ", "b"}
	got := AsStringSlice(input)
	assertStrSlice(t, got, []string{"a", "b"})
}

func TestAsStringSlice_AnySlice(t *testing.T) {
	input := []any{"x", "y", 42, "z"}
	got := AsStringSlice(input)
	// Non-string items skipped
	assertStrSlice(t, got, []string{"x", "y", "z"})
}

func TestAsStringSlice_AnySliceTrims(t *testing.T) {
	input := []any{"  hello  ", "", "world"}
	got := AsStringSlice(input)
	assertStrSlice(t, got, []string{"hello", "world"})
}

func TestAsStringSlice_Nil(t *testing.T) {
	got := AsStringSlice(nil)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestAsStringSlice_NonSlice(t *testing.T) {
	got := AsStringSlice("not a slice")
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestAsStringSlice_EmptySlice(t *testing.T) {
	got := AsStringSlice([]any{})
	// Empty input → empty (not nil) slice
	if got == nil || len(got) != 0 {
		t.Errorf("got %v, want empty non-nil slice", got)
	}
}

// assertStrSlice is a test helper.
func assertStrSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

// Ensure fmt import is used (Stringer test).
var _ = fmt.Sprintf
