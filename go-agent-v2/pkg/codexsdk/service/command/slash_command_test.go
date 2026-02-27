package command

import (
	"context"
	"strings"
	"testing"
)

func TestResolveThreadForSlashCommandLogicRequireThreadID(t *testing.T) {
	_, err := ResolveThreadForSlashCommandLogic(context.Background(), "", true, nil)
	if err == nil {
		t.Fatalf("ResolveThreadForSlashCommandLogic() err = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "threadId is required") {
		t.Fatalf("ResolveThreadForSlashCommandLogic() err = %v, want threadId is required", err)
	}
}
