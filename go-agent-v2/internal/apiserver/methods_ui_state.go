// methods_ui_state.go — UI 偏好/状态管理 JSON-RPC 方法 (preferences, state, thread aliases)。
package apiserver

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/dashboard"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type uiPrefGetParams struct{ Key string `json:"key"` }

func uiPreferencesGet(s *Server, ctx context.Context, p uiPrefGetParams) (any, error) {
	if s == nil || s.prefManager == nil { return nil, nil }
	return s.prefManager.Get(ctx, p.Key)
}

type uiPrefSetParams struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func uiPreferencesSet(s *Server, ctx context.Context, p uiPrefSetParams) (any, error) {
	if s == nil || s.prefManager == nil { return nil, nil }
	if err := s.prefManager.Set(ctx, p.Key, p.Value); err != nil { return nil, err }

	effects := dashboard.ResolvePreferenceSideEffects(p.Key, p.Value)
	if s.uiRuntime != nil && effects.MainAgentID != "" { s.uiRuntime.SetMainAgent(effects.MainAgentID) }
	if sec := effects.StallThresholdSec; sec > 0 {
		if s.codexAdapter != nil {
			s.codexAdapter.SetStallThreshold(time.Duration(sec) * time.Second)
			s.codexAdapter.SetStreamReadIdleTimeout(time.Duration(sec) * time.Second)
		}
		logger.Info("stall threshold updated via ui/preferences/set", "seconds", sec)
	}
	if sec := effects.StallHeartbeatSec; sec > 0 {
		if s.codexAdapter != nil { s.codexAdapter.SetStallHeartbeat(time.Duration(sec) * time.Second) }
		logger.Info("stall heartbeat updated via ui/preferences/set", "seconds", sec)
	}
	if show := effects.ShowInjectedPromptInChat; show != nil && s.uiRuntime != nil {
		s.uiRuntime.SetSanitizeInjectedUserMessage(!*show)
		logger.Info("chat injected prompt visibility updated via ui/preferences/set", "show", *show)
	}
	return map[string]any{"ok": true}, nil
}

func uiPreferencesGetAll(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	if s == nil || s.prefManager == nil { return map[string]any{}, nil }
	return s.prefManager.GetAll(ctx)
}

func uiStateGet(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	if s == nil || s.uiRuntime == nil { return map[string]any{}, nil }

	snapshot := s.uiRuntime.SnapshotLight()
	prefs := map[string]any{}
	if s.prefManager != nil {
		if loaded, err := s.prefManager.GetAll(ctx); err != nil {
			logger.Warn("ui/state/get: load preferences failed", logger.FieldError, err)
		} else {
			prefs = loaded
		}
	}

	resolution := dashboard.ResolveState(buildStateResolutionInput(snapshot, prefs))
	applyThreadAliasesSnapshot(&snapshot, resolution.Aliases)

	resolvedMain := resolution.ResolvedMainAgentID
	if resolvedMain != dashboard.AsString(prefs[dashboard.PrefMainAgentID]) {
		s.uiRuntime.SetMainAgent(resolvedMain)
		snapshot = s.uiRuntime.SnapshotLight()
		resolution = dashboard.ResolveState(buildStateResolutionInput(snapshot, prefs))
		applyThreadAliasesSnapshot(&snapshot, resolution.Aliases)
		pm, prev := s.prefManager, prefs[dashboard.PrefMainAgentID]
		util.SafeGo(func() { persistResolvedUIPreference(context.Background(), pm, dashboard.PrefMainAgentID, resolvedMain, prev) })
		prefs[dashboard.PrefMainAgentID] = resolvedMain
	}

	resolvedActiveThreadID, prevActive := resolution.ResolvedActiveThreadID, prefs[dashboard.PrefActiveThreadID]
	util.SafeGo(func() { persistResolvedUIPreference(context.Background(), s.prefManager, dashboard.PrefActiveThreadID, resolvedActiveThreadID, prevActive) })
	prefs[dashboard.PrefActiveThreadID] = resolvedActiveThreadID

	resolvedActiveCmdThreadID, prevCmd := resolution.ResolvedActiveCmdID, prefs[dashboard.PrefActiveCmdThreadID]
	util.SafeGo(func() { persistResolvedUIPreference(context.Background(), s.prefManager, dashboard.PrefActiveCmdThreadID, resolvedActiveCmdThreadID, prevCmd) })
	prefs[dashboard.PrefActiveCmdThreadID] = resolvedActiveCmdThreadID

	timelinesByThread, diffTextByThread := s.uiRuntime.AllTimelinesAndDiffs()
	runtimeItems := make([]dashboard.AgentRuntimeItem, 0)
	if s.mgr != nil {
		for _, info := range s.mgr.List() {
			runtimeItems = append(runtimeItems, dashboard.AgentRuntimeItem{ID: strings.TrimSpace(info.ID), State: string(info.State), Port: info.Port, CodexThreadID: strings.TrimSpace(info.ThreadID)})
		}
	}

	archivesValue, hasArchives := resolveThreadArchivesForState(s, ctx, prefs)
	viewPrefsChat, hasViewPrefsChat := prefs["viewPrefs.chat"]
	viewPrefsCmd, hasViewPrefsCmd := prefs["viewPrefs.cmd"]
	threadPinsChat, hasThreadPinsChat := prefs["threadPins.chat"]
	showInjected, hasShowInjected := prefs[prefKeyShowInjectedPromptInChat]

	result := dashboard.BuildUIStateResult(dashboard.UIStateResultInput{
		Threads:                  snapshot.Threads,
		Statuses:                 snapshot.Statuses,
		InterruptibleByThread:    snapshot.InterruptibleByThread,
		StatusHeadersByThread:    snapshot.StatusHeadersByThread,
		StatusDetailsByThread:    snapshot.StatusDetailsByThread,
		TimelinesByThread:        toAnyMap(timelinesByThread),
		DiffTextByThread:         diffTextByThread,
		TokenUsageByThread:       snapshot.TokenUsageByThread,
		AgentMetaByID:            snapshot.AgentMetaByID,
		WorkspaceRunsByKey:       snapshot.WorkspaceRunsByKey,
		ActiveThreadID:           resolvedActiveThreadID,
		ActiveCmdThreadID:        resolvedActiveCmdThreadID,
		MainAgentID:              resolvedMain,
		ActivityStatsByThread:    snapshot.ActivityStatsByThread,
		AlertsByThread:           snapshot.AlertsByThread,
		AgentRuntimeByID:         dashboard.BuildAgentRuntimeByID(runtimeItems),
		WorkspaceFeatureEnabled:  snapshot.WorkspaceFeatureEnabled,
		WorkspaceLastError:       snapshot.WorkspaceLastError,
		ViewPrefsChat:            viewPrefsChat,
		HasViewPrefsChat:         hasViewPrefsChat,
		ViewPrefsCmd:             viewPrefsCmd,
		HasViewPrefsCmd:          hasViewPrefsCmd,
		ThreadPinsChat:           threadPinsChat,
		HasThreadPinsChat:        hasThreadPinsChat,
		ThreadArchivesChat:       archivesValue,
		HasThreadArchives:        hasArchives,
		ShowInjectedPrompt:       showInjected,
		HasShowInjected:          hasShowInjected,
	})

	logger.Debug("ui/state/get: snapshot prepared",
		"threads_count", len(snapshot.Threads),
		"active_thread_id", resolvedActiveThreadID,
		"active_cmd_thread_id", resolvedActiveCmdThreadID,
		"timeline_threads", len(timelinesByThread),
		"diff_threads", len(diffTextByThread),
		"active_thread_diff_len", len(diffTextByThread[resolvedActiveThreadID]),
		"active_cmd_thread_diff_len", len(diffTextByThread[resolvedActiveCmdThreadID]),
	)
	return result, nil
}

func resolveThreadArchivesForState(s *Server, ctx context.Context, prefs map[string]any) (any, bool) {
	if s != nil && s.codexAdapter != nil {
		archivedMap, err := s.codexAdapter.ThreadArchiveMap(ctx)
		if err == nil {
			return archivedMap, true
		}
		logger.Warn("ui/state/get: load thread archives failed", logger.FieldError, err)
	}
	value, ok := prefs[dashboard.PrefThreadArchivesChat]
	return value, ok
}

func toAnyMap[T any](in map[string]T) map[string]any {
	if len(in) == 0 { return map[string]any{} }
	out := make(map[string]any, len(in))
	for k, v := range in { out[k] = v }
	return out
}

func buildStateResolutionInput(snapshot uistate.RuntimeSnapshot, prefs map[string]any) dashboard.StateResolutionInput {
	threads := make([]dashboard.StateThread, 0, len(snapshot.Threads))
	for _, thread := range snapshot.Threads { threads = append(threads, dashboard.StateThread{ID: thread.ID, Name: thread.Name}) }
	meta := make(map[string]dashboard.StateAgentMeta, len(snapshot.AgentMetaByID))
	for id, item := range snapshot.AgentMetaByID { meta[id] = dashboard.StateAgentMeta{Alias: item.Alias, IsMain: item.IsMain} }
	return dashboard.StateResolutionInput{Threads: threads, AgentMetaByID: meta, Prefs: prefs}
}

func applyThreadAliasesSnapshot(snapshot *uistate.RuntimeSnapshot, aliases map[string]string) {
	if snapshot == nil || len(snapshot.Threads) == 0 || len(aliases) == 0 { return }
	for i := range snapshot.Threads {
		id := strings.TrimSpace(snapshot.Threads[i].ID)
		if id == "" { continue }
		alias := strings.TrimSpace(aliases[id])
		if alias == "" { continue }
		snapshot.Threads[i].Name = alias
		meta := snapshot.AgentMetaByID[id]
		meta.Alias = alias
		snapshot.AgentMetaByID[id] = meta
	}
}

func persistResolvedUIPreference(ctx context.Context, manager *uistate.PreferenceManager, key, resolved string, original any) {
	if manager == nil || resolved == dashboard.AsString(original) { return }
	if err := manager.Set(ctx, key, resolved); err != nil {
		logger.Warn("ui/state/get: persist resolved preference failed", logger.FieldKey, key, logger.FieldError, err)
	}
}

func showInjectedPromptInChat(s *Server, ctx context.Context) bool {
	if s == nil || s.prefManager == nil { return false }
	value, err := s.prefManager.Get(ctx, prefKeyShowInjectedPromptInChat)
	if err != nil {
		logger.Warn("ui preferences: load injected prompt visibility failed", logger.FieldError, err)
		return false
	}
	return dashboard.AsBool(value, false)
}

func applyInjectedPromptVisibilityPreference(s *Server, ctx context.Context) {
	if s == nil || s.uiRuntime == nil { return }
	s.uiRuntime.SetSanitizeInjectedUserMessage(!showInjectedPromptInChat(s, ctx))
}

func asString(value any) string { return dashboard.AsString(value) }
