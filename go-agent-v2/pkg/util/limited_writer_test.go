package util

import (
	"bytes"
	"strings"
	"testing"
)

func TestLimitedWriter_WritesUpToLimit(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLimitedWriter(&buf, 10)

	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected n=5, got %d", n)
	}
	if buf.String() != "hello" {
		t.Fatalf("expected 'hello', got %q", buf.String())
	}
}

func TestLimitedWriter_TruncatesAtLimit(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLimitedWriter(&buf, 10)

	// Write 12 bytes, only 10 should be kept
	n, err := lw.Write([]byte("123456789012"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Returns len(p) even if truncated, so caller (exec.Cmd) doesn't see error
	if n != 10 {
		t.Fatalf("expected n=10, got %d", n)
	}
	if buf.String() != "1234567890" {
		t.Fatalf("expected '1234567890', got %q", buf.String())
	}
}

func TestLimitedWriter_SilentlyDiscardsAfterLimit(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLimitedWriter(&buf, 5)

	lw.Write([]byte("hello"))
	// Second write should be silently discarded
	n, err := lw.Write([]byte("world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Returns len(p) to avoid confusing callers
	if n != 5 {
		t.Fatalf("expected n=5 (silently discarded), got %d", n)
	}
	if buf.String() != "hello" {
		t.Fatalf("expected 'hello', got %q", buf.String())
	}
}

func TestLimitedWriter_MultipleWritesSplitAtBoundary(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLimitedWriter(&buf, 8)

	lw.Write([]byte("12345")) // 5 bytes, 3 remain
	n, err := lw.Write([]byte("67890"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only 3 bytes should be written (truncated)
	if n != 3 {
		t.Fatalf("expected n=3, got %d", n)
	}
	if buf.String() != "12345678" {
		t.Fatalf("expected '12345678', got %q", buf.String())
	}

	// Further writes fully discarded
	n, err = lw.Write([]byte("abc"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected n=3 (discarded), got %d", n)
	}
}

func TestLimitedWriter_WorksWithStringsBuilder(t *testing.T) {
	var sb strings.Builder
	lw := NewLimitedWriter(&sb, 6)

	lw.Write([]byte("hello world"))
	if sb.String() != "hello " {
		t.Fatalf("expected 'hello ', got %q", sb.String())
	}
}

func TestLimitedWriter_ZeroLimitDiscardsAll(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLimitedWriter(&buf, 0)

	n, err := lw.Write([]byte("anything"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 8 {
		t.Fatalf("expected n=8 (discarded), got %d", n)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty buffer, got %q", buf.String())
	}
}

func TestLimitedWriter_Overflow_ExactLimitWithoutDiscard(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLimitedWriter(&buf, 5)

	if _, err := lw.Write([]byte("hello")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lw.Overflow() {
		t.Fatal("expected overflow=false when output exactly hits limit without discard")
	}
}

func TestLimitedWriter_Overflow_TrueAfterDiscard(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLimitedWriter(&buf, 5)

	if _, err := lw.Write([]byte("hello")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := lw.Write([]byte("!")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !lw.Overflow() {
		t.Fatal("expected overflow=true after data is discarded")
	}
}
