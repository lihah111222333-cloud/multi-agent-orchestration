package codexadapter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSendSlashCommandFromRawParamsRequireThreadID(t *testing.T) {
	a := New(Deps{})
	_, err := a.SendSlashCommandFromRawParamsRequireThreadID(context.Background(), json.RawMessage(`{}`), "/compact")
	if err == nil {
		t.Fatalf("SendSlashCommandFromRawParamsRequireThreadID() err = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "threadId is required") {
		t.Fatalf("SendSlashCommandFromRawParamsRequireThreadID() err = %v, want threadId is required", err)
	}
}
