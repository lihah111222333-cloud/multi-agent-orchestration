package apiserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

func TestHandleClientResponse(t *testing.T) {
	ch := make(chan *Response, 1)
	s := &Server{
		pending: map[int64]chan *Response{
			42: ch,
		},
	}
	env := rpcEnvelope{
		ID:     json.RawMessage("42"),
		Result: json.RawMessage(`{"ok":true}`),
	}

	if !s.handleClientResponse(env) {
		t.Fatal("expected client response to be handled")
	}
	select {
	case resp := <-ch:
		if resp.ID != int64(42) {
			t.Fatalf("response id = %v, want 42", resp.ID)
		}
	default:
		t.Fatal("expected response sent to pending channel")
	}
}

func TestExtractToolFilePath(t *testing.T) {
	if got := extractToolFilePath(map[string]any{"path": "a.go"}); got != "a.go" {
		t.Fatalf("extractToolFilePath(path) = %q, want a.go", got)
	}
	if got := extractToolFilePath(map[string]any{}); got != "" {
		t.Fatalf("extractToolFilePath(empty) = %q, want empty", got)
	}
}

func TestBuildToolNotifyPayload(t *testing.T) {
	call := codex.DynamicToolCallData{Tool: "lsp_hover", CallID: "c1"}
	result := strings.Repeat("x", 600)
	payload := buildToolNotifyPayload("agent-1", call, map[string]any{"path": "a.go"}, "a.go", true, 3, 2*time.Second, result)

	if payload["tool"] != "lsp_hover" {
		t.Fatalf("tool = %v, want lsp_hover", payload["tool"])
	}
	preview, _ := payload["resultPreview"].(string)
	if len(preview) != 500 {
		t.Fatalf("resultPreview len = %d, want 500", len(preview))
	}
}

func TestCalculateHydrationLoadLimit(t *testing.T) {
	tests := []struct {
		name         string
		initialCount int
		total        int64
		want         int
	}{
		{name: "keep initial", initialCount: 300, total: 120, want: 300},
		{name: "use total", initialCount: 300, total: 800, want: 800},
		{name: "cap max", initialCount: 300, total: 99999, want: threadMessageHydrationMaxRecords},
		{name: "negative initial", initialCount: -1, total: 10, want: 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := calculateHydrationLoadLimit(tc.initialCount, tc.total)
			if got != tc.want {
				t.Fatalf("calculateHydrationLoadLimit(%d,%d)=%d, want %d", tc.initialCount, tc.total, got, tc.want)
			}
		})
	}
}
