package apiserver

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestComposeUserTimelineTextForTurn_HiddenModeReturnsOriginalPrompt(t *testing.T) {
	got := composeUserTimelineTextForTurn(
		"用户问题",
		"用户问题\n[skill:demo] 摘要: ...",
		"已注入 LSP 工具",
		false,
	)
	if got != "用户问题" {
		t.Fatalf("got %q, want original prompt", got)
	}
}

func TestComposeUserTimelineTextForTurn_ShowModeIncludesSubmitPromptAndHint(t *testing.T) {
	got := composeUserTimelineTextForTurn(
		"用户问题",
		"用户问题\n[skill:demo] 摘要: ...",
		"已注入 LSP/Playwright/json-render/code_run 工具。",
		true,
	)
	want := "用户问题\n[skill:demo] 摘要: ...\n已注入 LSP/Playwright/json-render/code_run 工具。"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestComposeUserTimelineTextForTurn_ShowModeAvoidsDuplicateHint(t *testing.T) {
	submitPrompt := "用户问题\n已注入 LSP/Playwright/json-render/code_run 工具。"
	got := composeUserTimelineTextForTurn(
		"用户问题",
		submitPrompt,
		"已注入 LSP/Playwright/json-render/code_run 工具。",
		true,
	)
	if got != submitPrompt {
		t.Fatalf("got %q, want unchanged submit prompt", got)
	}
}

func TestComposeUserTimelineTextForTurn_ShowModeWithoutHintUsesSubmitPrompt(t *testing.T) {
	submitPrompt := "用户问题\n[skill:demo] 摘要: ..."
	got := composeUserTimelineTextForTurn(
		"用户问题",
		submitPrompt,
		"",
		true,
	)
	if got != submitPrompt {
		t.Fatalf("got %q, want submit prompt", got)
	}
}

func TestThreadTimelineAlreadyShowsInjectedPrompt(t *testing.T) {
	srv := &Server{uiRuntime: uistate.NewRuntimeManager()}
	threadID := "thread-injected-marker"

	if srv.threadTimelineAlreadyShowsInjectedPrompt(threadID) {
		t.Fatal("empty timeline should not be detected as injected")
	}

	srv.uiRuntime.AppendUserMessage(threadID, "普通问题", nil)
	if srv.threadTimelineAlreadyShowsInjectedPrompt(threadID) {
		t.Fatal("plain user message should not be detected as injected")
	}

	srv.uiRuntime.AppendUserMessage(threadID, "问题本体\n已注入 LSP/Playwright/json-render/code_run 工具。", nil)
	if !srv.threadTimelineAlreadyShowsInjectedPrompt(threadID) {
		t.Fatal("injected marker should be detected")
	}
}
