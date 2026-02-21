package apiserver

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/config"
)

func TestMainPathMethods_StillRegisteredWhenOffline52Enabled(t *testing.T) {
	srv := New(Deps{Config: &config.Config{DisableOffline52Methods: true}})
	mainPath := []string{
		"thread/start", "thread/list", "thread/messages", "thread/name/set",
		"turn/start", "turn/interrupt",
		"ui/state/get", "ui/dashboard/get", "ui/preferences/get", "ui/preferences/set",
		"ui/projects/get", "ui/projects/setActive",
		"skills/local/read", "skills/local/importDir", "skills/config/read", "skills/config/write", "skills/match/preview",
		"config/lspPromptHint/read", "config/lspPromptHint/write",
		"config/jsonRenderPrompt/read", "config/jsonRenderPrompt/write",
	}
	for _, method := range mainPath {
		if _, ok := srv.methods[method]; !ok {
			t.Fatalf("main path method missing: %s", method)
		}
	}
}
