package apiserver

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestUIStateGetIncludesPreferences(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
		uiRuntime:   uistate.NewRuntimeManager(),
	}

	srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
		{ID: "thread-1", Name: "thread-1", State: "idle"},
		{ID: "thread-2", Name: "thread-2", State: "idle"},
	})

	ctx := context.Background()
	if err := srv.prefManager.Set(ctx, "activeThreadId", "thread-1"); err != nil {
		t.Fatalf("set activeThreadId: %v", err)
	}
	if err := srv.prefManager.Set(ctx, "activeCmdThreadId", "thread-2"); err != nil {
		t.Fatalf("set activeCmdThreadId: %v", err)
	}
	if err := srv.prefManager.Set(ctx, "mainAgentId", "thread-1"); err != nil {
		t.Fatalf("set mainAgentId: %v", err)
	}
	viewChat := map[string]any{"layout": "focus", "splitRatio": 64}
	viewCmd := map[string]any{"layout": "mix", "splitRatio": 56, "cardCols": 3}
	if err := srv.prefManager.Set(ctx, "viewPrefs.chat", viewChat); err != nil {
		t.Fatalf("set viewPrefs.chat: %v", err)
	}
	if err := srv.prefManager.Set(ctx, "viewPrefs.cmd", viewCmd); err != nil {
		t.Fatalf("set viewPrefs.cmd: %v", err)
	}

	raw, err := srv.uiStateGet(ctx, nil)
	if err != nil {
		t.Fatalf("uiStateGet error: %v", err)
	}

	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("uiStateGet type = %T, want map[string]any", raw)
	}

	threads, ok := resp["threads"].([]uistate.ThreadSnapshot)
	if !ok {
		t.Fatalf("threads type = %T, want []uistate.ThreadSnapshot", resp["threads"])
	}
	if len(threads) < 2 || threads[0].ID != "thread-1" || threads[1].ID != "thread-2" {
		t.Fatalf("unexpected threads payload: %#v", threads)
	}

	if got := resp["activeThreadId"]; got != "thread-1" {
		t.Fatalf("activeThreadId = %#v, want thread-1", got)
	}
	if got := resp["activeCmdThreadId"]; got != "thread-2" {
		t.Fatalf("activeCmdThreadId = %#v, want thread-2", got)
	}
	if got := resp["mainAgentId"]; got != "thread-1" {
		t.Fatalf("mainAgentId = %#v, want thread-1", got)
	}
	if got := resp["viewPrefs.chat"]; !reflect.DeepEqual(got, viewChat) {
		t.Fatalf("viewPrefs.chat = %#v, want %#v", got, viewChat)
	}
	if got := resp["viewPrefs.cmd"]; !reflect.DeepEqual(got, viewCmd) {
		t.Fatalf("viewPrefs.cmd = %#v, want %#v", got, viewCmd)
	}
}

func TestUIStateGetResolvesAndPersistsActivePreferences(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
		uiRuntime:   uistate.NewRuntimeManager(),
	}

	srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
		{ID: "main-1", Name: "主Agent", State: "idle"},
		{ID: "worker-1", Name: "worker-1", State: "idle"},
	})

	ctx := context.Background()
	if err := srv.prefManager.Set(ctx, "activeThreadId", "missing-chat"); err != nil {
		t.Fatalf("set activeThreadId: %v", err)
	}
	if err := srv.prefManager.Set(ctx, "activeCmdThreadId", "missing-cmd"); err != nil {
		t.Fatalf("set activeCmdThreadId: %v", err)
	}
	if err := srv.prefManager.Set(ctx, "mainAgentId", "missing-main"); err != nil {
		t.Fatalf("set mainAgentId: %v", err)
	}

	raw, err := srv.uiStateGet(ctx, nil)
	if err != nil {
		t.Fatalf("uiStateGet error: %v", err)
	}

	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("uiStateGet type = %T, want map[string]any", raw)
	}

	if got := resp["mainAgentId"]; got != "main-1" {
		t.Fatalf("mainAgentId = %#v, want main-1", got)
	}
	if got := resp["activeThreadId"]; got != "main-1" {
		t.Fatalf("activeThreadId = %#v, want main-1", got)
	}
	if got := resp["activeCmdThreadId"]; got != "worker-1" {
		t.Fatalf("activeCmdThreadId = %#v, want worker-1", got)
	}

	time.Sleep(50 * time.Millisecond) // wait for async persist goroutines
	persistedMain, _ := srv.prefManager.Get(ctx, "mainAgentId")
	if persistedMain != "main-1" {
		t.Fatalf("persisted mainAgentId = %#v, want main-1", persistedMain)
	}
	persistedActive, _ := srv.prefManager.Get(ctx, "activeThreadId")
	if persistedActive != "main-1" {
		t.Fatalf("persisted activeThreadId = %#v, want main-1", persistedActive)
	}
	persistedCmd, _ := srv.prefManager.Get(ctx, "activeCmdThreadId")
	if persistedCmd != "worker-1" {
		t.Fatalf("persisted activeCmdThreadId = %#v, want worker-1", persistedCmd)
	}
}

func TestUIStateGetResolvesToEmptyWhenNoThreads(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
		uiRuntime:   uistate.NewRuntimeManager(),
	}

	ctx := context.Background()
	if err := srv.prefManager.Set(ctx, "activeThreadId", "missing-chat"); err != nil {
		t.Fatalf("set activeThreadId: %v", err)
	}
	if err := srv.prefManager.Set(ctx, "activeCmdThreadId", "missing-cmd"); err != nil {
		t.Fatalf("set activeCmdThreadId: %v", err)
	}
	if err := srv.prefManager.Set(ctx, "mainAgentId", "missing-main"); err != nil {
		t.Fatalf("set mainAgentId: %v", err)
	}

	raw, err := srv.uiStateGet(ctx, nil)
	if err != nil {
		t.Fatalf("uiStateGet error: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("uiStateGet type = %T, want map[string]any", raw)
	}

	if got := resp["mainAgentId"]; got != "" {
		t.Fatalf("mainAgentId = %#v, want empty", got)
	}
	if got := resp["activeThreadId"]; got != "" {
		t.Fatalf("activeThreadId = %#v, want empty", got)
	}
	if got := resp["activeCmdThreadId"]; got != "" {
		t.Fatalf("activeCmdThreadId = %#v, want empty", got)
	}

	time.Sleep(50 * time.Millisecond) // wait for async persist goroutines
	persistedMain, _ := srv.prefManager.Get(ctx, "mainAgentId")
	if persistedMain != "" {
		t.Fatalf("persisted mainAgentId = %#v, want empty", persistedMain)
	}
	persistedActive, _ := srv.prefManager.Get(ctx, "activeThreadId")
	if persistedActive != "" {
		t.Fatalf("persisted activeThreadId = %#v, want empty", persistedActive)
	}
	persistedCmd, _ := srv.prefManager.Get(ctx, "activeCmdThreadId")
	if persistedCmd != "" {
		t.Fatalf("persisted activeCmdThreadId = %#v, want empty", persistedCmd)
	}
}

func TestUIStateGetCmdFallsBackToMainWhenOnlyMainThread(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
		uiRuntime:   uistate.NewRuntimeManager(),
	}

	srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
		{ID: "main-1", Name: "main", State: "idle"},
	})

	ctx := context.Background()
	if err := srv.prefManager.Set(ctx, "activeThreadId", "missing-chat"); err != nil {
		t.Fatalf("set activeThreadId: %v", err)
	}
	if err := srv.prefManager.Set(ctx, "activeCmdThreadId", "missing-cmd"); err != nil {
		t.Fatalf("set activeCmdThreadId: %v", err)
	}
	if err := srv.prefManager.Set(ctx, "mainAgentId", "missing-main"); err != nil {
		t.Fatalf("set mainAgentId: %v", err)
	}

	raw, err := srv.uiStateGet(ctx, nil)
	if err != nil {
		t.Fatalf("uiStateGet error: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("uiStateGet type = %T, want map[string]any", raw)
	}

	if got := resp["mainAgentId"]; got != "main-1" {
		t.Fatalf("mainAgentId = %#v, want main-1", got)
	}
	if got := resp["activeThreadId"]; got != "main-1" {
		t.Fatalf("activeThreadId = %#v, want main-1", got)
	}
	if got := resp["activeCmdThreadId"]; got != "main-1" {
		t.Fatalf("activeCmdThreadId = %#v, want main-1", got)
	}
}
