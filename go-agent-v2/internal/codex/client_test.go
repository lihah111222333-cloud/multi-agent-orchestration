package codex

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitForKnownPort_ContextCanceled(t *testing.T) {
	client := NewClient(45678, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.waitForKnownPort(ctx, time.Now().Add(5*time.Second))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForKnownPort error = %v, want context.Canceled", err)
	}
}

func TestDiscoverPort_ContextCanceled(t *testing.T) {
	client := NewClient(0, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.discoverPort(ctx, time.Now().Add(5*time.Second), &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("discoverPort error = %v, want context.Canceled", err)
	}
}
