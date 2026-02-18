package mcp

import (
	"context"
	"testing"
)

func TestNormalizeToolLimit(t *testing.T) {
	if got := normalizeToolLimit(0); got != 100 {
		t.Fatalf("normalizeToolLimit(0) = %d, want 100", got)
	}
	if got := normalizeToolLimit(600); got != 100 {
		t.Fatalf("normalizeToolLimit(600) = %d, want 100", got)
	}
	if got := normalizeToolLimit(25); got != 25 {
		t.Fatalf("normalizeToolLimit(25) = %d, want 25", got)
	}
}

func TestHandleToolUnknown(t *testing.T) {
	server := NewServer(&Stores{})
	_, err := server.HandleTool(context.Background(), "unknown_tool", nil)
	if err == nil {
		t.Fatal("expected unknown tool error")
	}
}
