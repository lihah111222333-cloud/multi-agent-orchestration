package uistate

import (
	"context"
	"testing"
)

func TestPreferenceManager_FallbackMemory(t *testing.T) {
	manager := NewPreferenceManager(nil)
	ctx := context.Background()

	initial, err := manager.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("Get missing: %v", err)
	}
	if initial != nil {
		t.Fatalf("want nil for missing key, got %v", initial)
	}

	if err := manager.Set(ctx, "activeThreadId", "thread-1"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := manager.Get(ctx, "activeThreadId")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "thread-1" {
		t.Fatalf("want thread-1, got %v", got)
	}

	all, err := manager.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if all["activeThreadId"] != "thread-1" {
		t.Fatalf("GetAll missing activeThreadId")
	}
}
