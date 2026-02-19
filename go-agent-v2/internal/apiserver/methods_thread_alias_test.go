package apiserver

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestNormalizeThreadAliases(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  map[string]string
	}{
		{
			name: "map any",
			value: map[string]any{
				" thread-1 ": " Alpha ",
				"thread-2":   "",
				"thread-3":   "thread-3",
				"":           "ignored",
			},
			want: map[string]string{
				"thread-1": "Alpha",
			},
		},
		{
			name:  "json string",
			value: `{"thread-1":"主Agent","thread-2":"  ","thread-3":"thread-3"}`,
			want: map[string]string{
				"thread-1": "主Agent",
			},
		},
		{
			name: "map string",
			value: map[string]string{
				"thread-1": "Alpha",
				"thread-2": "thread-2",
			},
			want: map[string]string{
				"thread-1": "Alpha",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeThreadAliases(tc.value)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeThreadAliases(%#v) = %#v, want %#v", tc.value, got, tc.want)
			}
		})
	}
}

func TestPersistThreadAliasPreference(t *testing.T) {
	ctx := context.Background()
	manager := uistate.NewPreferenceManager(nil)

	if err := manager.Set(ctx, prefThreadAliases, map[string]any{
		"thread-1": "old",
		"thread-2": "Beta",
	}); err != nil {
		t.Fatalf("set seed aliases: %v", err)
	}

	if err := persistThreadAliasPreference(ctx, manager, "thread-1", " Alpha "); err != nil {
		t.Fatalf("persist thread-1 alias: %v", err)
	}

	raw, _ := manager.Get(ctx, prefThreadAliases)
	aliases := normalizeThreadAliases(raw)
	want := map[string]string{
		"thread-1": "Alpha",
		"thread-2": "Beta",
	}
	if !reflect.DeepEqual(aliases, want) {
		t.Fatalf("aliases after update = %#v, want %#v", aliases, want)
	}

	if err := persistThreadAliasPreference(ctx, manager, "thread-1", ""); err != nil {
		t.Fatalf("clear thread-1 alias: %v", err)
	}
	raw, _ = manager.Get(ctx, prefThreadAliases)
	aliases = normalizeThreadAliases(raw)
	want = map[string]string{
		"thread-2": "Beta",
	}
	if !reflect.DeepEqual(aliases, want) {
		t.Fatalf("aliases after clear = %#v, want %#v", aliases, want)
	}
}

func TestPersistThreadAliasConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
	}

	var wg sync.WaitGroup
	wg.Add(2)

	errCh := make(chan error, 2)
	go func() {
		defer wg.Done()
		errCh <- srv.persistThreadAlias(ctx, "thread-1", "Alpha")
	}()
	go func() {
		defer wg.Done()
		errCh <- srv.persistThreadAlias(ctx, "thread-2", "Beta")
	}()

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("persistThreadAlias concurrent write failed: %v", err)
		}
	}

	raw, _ := srv.prefManager.Get(ctx, prefThreadAliases)
	aliases := normalizeThreadAliases(raw)
	want := map[string]string{
		"thread-1": "Alpha",
		"thread-2": "Beta",
	}
	if !reflect.DeepEqual(aliases, want) {
		t.Fatalf("aliases after concurrent write = %#v, want %#v", aliases, want)
	}
}

func TestApplyThreadAliases(t *testing.T) {
	threads := []threadListItem{
		{ID: "thread-1", Name: "thread-1", State: "idle"},
		{ID: "thread-2", Name: "thread-2", State: "idle"},
	}
	applyThreadAliases(threads, map[string]string{
		"thread-1": "主Agent",
	})

	if threads[0].Name != "主Agent" {
		t.Fatalf("thread-1 name = %q, want 主Agent", threads[0].Name)
	}
	if threads[1].Name != "thread-2" {
		t.Fatalf("thread-2 name = %q, want thread-2", threads[1].Name)
	}
}

func TestUIStateGetAppliesPersistedThreadAliases(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
		uiRuntime:   uistate.NewRuntimeManager(),
	}

	srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
		{ID: "thread-1", Name: "thread-1", State: "idle"},
		{ID: "thread-2", Name: "thread-2", State: "idle"},
	})

	ctx := context.Background()
	if err := srv.prefManager.Set(ctx, prefThreadAliases, map[string]any{
		"thread-1": "主Agent",
	}); err != nil {
		t.Fatalf("set aliases: %v", err)
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
	if len(threads) < 1 || threads[0].Name != "主Agent" {
		t.Fatalf("threads = %#v, want first thread named 主Agent", threads)
	}

	metaByID, ok := resp["agentMetaById"].(map[string]uistate.AgentMeta)
	if !ok {
		t.Fatalf("agentMetaById type = %T, want map[string]uistate.AgentMeta", resp["agentMetaById"])
	}
	if alias := metaByID["thread-1"].Alias; alias != "主Agent" {
		t.Fatalf("agentMetaById[thread-1].alias = %q, want 主Agent", alias)
	}
	if got := resp["mainAgentId"]; got != "thread-1" {
		t.Fatalf("mainAgentId = %#v, want thread-1", got)
	}
}

func TestThreadNameSetTypedPersistsAliasWithoutLoadedThread(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
		uiRuntime:   uistate.NewRuntimeManager(),
	}
	srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
		{ID: "thread-1", Name: "thread-1", State: "idle"},
	})

	if _, err := srv.threadNameSetTyped(context.Background(), threadNameSetParams{
		ThreadID: "thread-1",
		Name:     "主Agent",
	}); err != nil {
		t.Fatalf("threadNameSetTyped error: %v", err)
	}

	raw, _ := srv.prefManager.Get(context.Background(), prefThreadAliases)
	aliases := normalizeThreadAliases(raw)
	if aliases["thread-1"] != "主Agent" {
		t.Fatalf("persisted alias = %#v, want 主Agent", aliases["thread-1"])
	}

	snapshot := srv.uiRuntime.SnapshotLight()
	if len(snapshot.Threads) < 1 || snapshot.Threads[0].Name != "主Agent" {
		t.Fatalf("snapshot threads = %#v, want first thread named 主Agent", snapshot.Threads)
	}
	if got := snapshot.AgentMetaByID["thread-1"].Alias; got != "主Agent" {
		t.Fatalf("snapshot alias = %q, want 主Agent", got)
	}
}

func TestThreadNameSetTypedRejectsUnknownThread(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
		uiRuntime:   uistate.NewRuntimeManager(),
	}

	if _, err := srv.threadNameSetTyped(context.Background(), threadNameSetParams{
		ThreadID: "missing-thread",
		Name:     "ghost",
	}); err == nil {
		t.Fatal("threadNameSetTyped should fail for unknown thread")
	}

	raw, _ := srv.prefManager.Get(context.Background(), prefThreadAliases)
	aliases := normalizeThreadAliases(raw)
	if len(aliases) != 0 {
		t.Fatalf("aliases should stay empty for unknown thread rename, got %#v", aliases)
	}
}
