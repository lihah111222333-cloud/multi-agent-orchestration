package apiserver

import (
	"context"
	"os"
	"path/filepath"
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

func TestLoadAllThreadMessagesFromCodexRollout_RespectsInjectedPromptVisibility(t *testing.T) {
	ctx := context.Background()
	threadID := "11111111-1111-1111-1111-111111111111"
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	today := time.Now()
	sessionsDir := filepath.Join(homeDir, ".codex", "sessions", today.Format("2006"), today.Format("01"), today.Format("02"))
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	rolloutPath := filepath.Join(sessionsDir, "rollout-test-"+threadID+".jsonl")
	rollout := `{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"问题本体\n已注入 LSP context"}]}}
`
	if err := os.WriteFile(rolloutPath, []byte(rollout), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
	}

	trimmedMsgs, err := srv.loadAllThreadMessagesFromCodexRollout(ctx, threadID)
	if err != nil {
		t.Fatalf("load trimmed rollout: %v", err)
	}
	if len(trimmedMsgs) != 1 {
		t.Fatalf("trimmed messages len=%d, want 1", len(trimmedMsgs))
	}
	if trimmedMsgs[0].Content != "问题本体" {
		t.Fatalf("trimmed content=%q, want %q", trimmedMsgs[0].Content, "问题本体")
	}

	if err := srv.prefManager.Set(ctx, prefKeyShowInjectedPromptInChat, true); err != nil {
		t.Fatalf("set %s: %v", prefKeyShowInjectedPromptInChat, err)
	}
	rawMsgs, err := srv.loadAllThreadMessagesFromCodexRollout(ctx, threadID)
	if err != nil {
		t.Fatalf("load raw rollout: %v", err)
	}
	if len(rawMsgs) != 1 {
		t.Fatalf("raw messages len=%d, want 1", len(rawMsgs))
	}
	if rawMsgs[0].Content != "问题本体\n已注入 LSP context" {
		t.Fatalf("raw content=%q, want full injected text", rawMsgs[0].Content)
	}
}
