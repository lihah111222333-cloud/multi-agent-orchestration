package codex

import (
	"testing"
	"time"
)

func TestSetAppServerReadIdleTimeout(t *testing.T) {
	original := GetAppServerReadIdleTimeout()
	defer setAppServerReadIdleTimeout(original)

	SetAppServerReadIdleTimeout(800 * time.Second)
	if got := GetAppServerReadIdleTimeout(); got != 800*time.Second {
		t.Fatalf("GetAppServerReadIdleTimeout() = %v, want %v", got, 800*time.Second)
	}

	// non-positive input should be ignored.
	SetAppServerReadIdleTimeout(0)
	if got := GetAppServerReadIdleTimeout(); got != 800*time.Second {
		t.Fatalf("GetAppServerReadIdleTimeout() after zero update = %v, want %v", got, 800*time.Second)
	}
}
