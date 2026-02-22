package apiserver

import (
	"context"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestThreadExistsInHistoryFromArchivePreference(t *testing.T) {
	ctx := context.Background()
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
	}

	if err := srv.prefManager.Set(ctx, prefThreadArchivesChat, map[string]any{
		"thread-archived": time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("set archive pref: %v", err)
	}

	if !srv.threadExistsInHistory(ctx, "thread-archived") {
		t.Fatal("threadExistsInHistory(thread-archived)=false, want true")
	}
	if srv.threadExistsInHistory(ctx, "thread-missing") {
		t.Fatal("threadExistsInHistory(thread-missing)=true, want false")
	}
}
