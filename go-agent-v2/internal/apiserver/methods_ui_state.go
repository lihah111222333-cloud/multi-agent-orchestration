// methods_ui_state.go — UI 偏好/状态管理 JSON-RPC 方法 (preferences, state, thread aliases)。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	prefThreadAliases      = "threads.aliases"
	prefThreadArchivesChat = "threadArchives.chat"
)

type uiPrefGetParams struct {
	Key string `json:"key"`
}

func uiPreferencesGet(s *Server, ctx context.Context, p uiPrefGetParams) (any, error) {
	if s == nil || s.prefManager == nil {
		return nil, nil
	}
	return s.prefManager.Get(ctx, p.Key)
}

type uiPrefSetParams struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func uiPreferencesSet(s *Server, ctx context.Context, p uiPrefSetParams) (any, error) {
	if s == nil || s.prefManager == nil {
		return nil, nil
	}
	if err := s.prefManager.Set(ctx, p.Key, p.Value); err != nil {
		return nil, err
	}
	if s.uiRuntime != nil {
		if p.Key == "mainAgentId" {
			s.uiRuntime.SetMainAgent(asString(p.Value))
		}
	}
	// stall 参数运行时热调
	switch p.Key {
	case "stallThresholdSec":
		if sec := asPositiveInt(p.Value, 30); sec > 0 {
			if s.codexAdapter != nil {
				s.codexAdapter.SetStallThreshold(time.Duration(sec) * time.Second)
				s.codexAdapter.SetStreamReadIdleTimeout(time.Duration(sec) * time.Second)
			}
			logger.Info("stall threshold updated via ui/preferences/set", "seconds", sec)
		}
	case "stallHeartbeatSec":
		if sec := asPositiveInt(p.Value, 10); sec > 0 {
			if s.codexAdapter != nil {
				s.codexAdapter.SetStallHeartbeat(time.Duration(sec) * time.Second)
			}
			logger.Info("stall heartbeat updated via ui/preferences/set", "seconds", sec)
		}
	case prefKeyShowInjectedPromptInChat:
		if s.uiRuntime != nil {
			show := asBool(p.Value, false)
			s.uiRuntime.SetSanitizeInjectedUserMessage(!show)
			logger.Info("chat injected prompt visibility updated via ui/preferences/set", "show", show)
		}
	}
	return map[string]any{"ok": true}, nil
}

func uiPreferencesGetAll(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	if s == nil || s.prefManager == nil {
		return map[string]any{}, nil
	}
	return s.prefManager.GetAll(ctx)
}

func uiStateGet(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	if s == nil {
		return map[string]any{}, nil
	}
	if s.uiRuntime == nil {
		return map[string]any{}, nil
	}
	snapshot := s.uiRuntime.SnapshotLight()
	prefs := map[string]any{}
	if s.prefManager != nil {
		loaded, err := s.prefManager.GetAll(ctx)
		if err != nil {
			logger.Warn("ui/state/get: load preferences failed", logger.FieldError, err)
		} else {
			prefs = loaded
		}
	}
	applyThreadAliasesSnapshot(&snapshot, loadThreadAliasesFromPrefs(prefs))

	resolvedMain := resolveMainAgentPreference(snapshot, prefs)
	if resolvedMain != asString(prefs["mainAgentId"]) {
		s.uiRuntime.SetMainAgent(resolvedMain)
		snapshot = s.uiRuntime.SnapshotLight()
		applyThreadAliasesSnapshot(&snapshot, loadThreadAliasesFromPrefs(prefs))
		pm := s.prefManager
		prev := prefs["mainAgentId"]
		util.SafeGo(func() { persistResolvedUIPreference(context.Background(), pm, "mainAgentId", resolvedMain, prev) })
		prefs["mainAgentId"] = resolvedMain
	}

	resolvedActiveThreadID := resolvePreferredThreadID(snapshot.Threads, asString(prefs["activeThreadId"]))
	prevActive := prefs["activeThreadId"]
	util.SafeGo(func() {
		persistResolvedUIPreference(context.Background(), s.prefManager, "activeThreadId", resolvedActiveThreadID, prevActive)
	})
	prefs["activeThreadId"] = resolvedActiveThreadID

	resolvedActiveCmdThreadID := resolvePreferredCmdThreadID(snapshot.Threads, resolvedMain, asString(prefs["activeCmdThreadId"]))
	prevCmd := prefs["activeCmdThreadId"]
	util.SafeGo(func() {
		persistResolvedUIPreference(context.Background(), s.prefManager, "activeCmdThreadId", resolvedActiveCmdThreadID, prevCmd)
	})
	prefs["activeCmdThreadId"] = resolvedActiveCmdThreadID

	// 返回所有已 hydrate 线程的 timeline/diff — 避免切换会话时的竞态丢失。
	timelinesByThread, diffTextByThread := s.uiRuntime.AllTimelinesAndDiffs()

	result := map[string]any{
		"threads":               snapshot.Threads,
		"statuses":              snapshot.Statuses,
		"interruptibleByThread": snapshot.InterruptibleByThread,
		"statusHeadersByThread": snapshot.StatusHeadersByThread,
		"statusDetailsByThread": snapshot.StatusDetailsByThread,
		"timelinesByThread":     timelinesByThread,
		"diffTextByThread":      diffTextByThread,
		"tokenUsageByThread":    snapshot.TokenUsageByThread,
		"agentMetaById":         snapshot.AgentMetaByID,
		"workspaceRunsByKey":    snapshot.WorkspaceRunsByKey,
		"activeThreadId":        resolvedActiveThreadID,
		"activeCmdThreadId":     resolvedActiveCmdThreadID,
		"mainAgentId":           resolvedMain,
		"activityStatsByThread": snapshot.ActivityStatsByThread,
		"alertsByThread":        snapshot.AlertsByThread,
	}
	agentRuntimeByID := map[string]map[string]any{}
	if s.mgr != nil {
		for _, info := range s.mgr.List() {
			id := strings.TrimSpace(info.ID)
			if id == "" {
				continue
			}
			item := map[string]any{
				"state": string(info.State),
			}
			if port := info.Port; port > 0 {
				item["port"] = port
			}
			if codexThreadID := strings.TrimSpace(info.ThreadID); codexThreadID != "" {
				item["codexThreadId"] = codexThreadID
			}
			agentRuntimeByID[id] = item
		}
	}
	result["agentRuntimeById"] = agentRuntimeByID
	if snapshot.WorkspaceFeatureEnabled != nil {
		result["workspaceFeatureEnabled"] = *snapshot.WorkspaceFeatureEnabled
	}
	if snapshot.WorkspaceLastError != "" {
		result["workspaceLastError"] = snapshot.WorkspaceLastError
	}
	if value, ok := prefs["viewPrefs.chat"]; ok {
		result["viewPrefs.chat"] = value
	}
	if value, ok := prefs["viewPrefs.cmd"]; ok {
		result["viewPrefs.cmd"] = value
	}
	if value, ok := prefs["threadPins.chat"]; ok {
		result["threadPins.chat"] = value
	}
	if s != nil && s.codexAdapter != nil {
		if archivedMap, err := s.codexAdapter.ThreadArchiveMap(ctx); err == nil {
			result[prefThreadArchivesChat] = archivedMap
		} else {
			logger.Warn("ui/state/get: load thread archives failed", logger.FieldError, err)
			if value, ok := prefs[prefThreadArchivesChat]; ok {
				result[prefThreadArchivesChat] = value
			}
		}
	} else if value, ok := prefs[prefThreadArchivesChat]; ok {
		result[prefThreadArchivesChat] = value
	}
	if value, ok := prefs[prefKeyShowInjectedPromptInChat]; ok {
		result[prefKeyShowInjectedPromptInChat] = value
	}
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

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

// asPositiveInt 从 any 提取正整数，低于 minVal 返回 0。
func asPositiveInt(value any, minVal int) int {
	var n int
	switch v := value.(type) {
	case float64:
		n = int(v)
	case int:
		n = v
	case int64:
		n = int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			n = int(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			n = i
		}
	default:
		return 0
	}
	if n < minVal {
		return 0
	}
	return n
}

func asBool(value any, fallback bool) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i != 0
		}
		if f, err := v.Float64(); err == nil {
			return f != 0
		}
	}
	return fallback
}

func showInjectedPromptInChat(s *Server, ctx context.Context) bool {
	if s.prefManager == nil {
		return false
	}
	value, err := s.prefManager.Get(ctx, prefKeyShowInjectedPromptInChat)
	if err != nil {
		logger.Warn("ui preferences: load injected prompt visibility failed", logger.FieldError, err)
		return false
	}
	return asBool(value, false)
}

func applyInjectedPromptVisibilityPreference(s *Server, ctx context.Context) {
	if s.uiRuntime == nil {
		return
	}
	show := showInjectedPromptInChat(s, ctx)
	s.uiRuntime.SetSanitizeInjectedUserMessage(!show)
}

func persistThreadAlias(s *Server, ctx context.Context, threadID, alias string) error {
	if s == nil {
		return nil
	}
	var persistErr error
	withThreadAliasLock(s, func() {
		persistErr = persistThreadAliasPreference(ctx, s.prefManager, threadID, alias)
	})
	return persistErr
}

func persistThreadAliasPreference(ctx context.Context, manager *uistate.PreferenceManager, threadID, alias string) error {
	if manager == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}

	value, err := manager.Get(ctx, prefThreadAliases)
	if err != nil {
		return err
	}
	aliases := normalizeThreadAliases(value)
	nextAlias := strings.TrimSpace(alias)
	if nextAlias == "" || nextAlias == id {
		delete(aliases, id)
	} else {
		aliases[id] = nextAlias
	}
	return manager.Set(ctx, prefThreadAliases, aliases)
}

func loadThreadAliases(s *Server, ctx context.Context) map[string]string {
	if s.prefManager == nil {
		return map[string]string{}
	}
	value, err := s.prefManager.Get(ctx, prefThreadAliases)
	if err != nil {
		logger.Warn("thread aliases: load preference failed", logger.FieldError, err)
		return map[string]string{}
	}
	return normalizeThreadAliases(value)
}

func loadThreadAliasesFromPrefs(prefs map[string]any) map[string]string {
	if prefs == nil {
		return map[string]string{}
	}
	return normalizeThreadAliases(prefs[prefThreadAliases])
}

// normalizeThreadAliases converts any value into map[string]string for thread alias usage.
func normalizeThreadAliases(value any) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	switch m := value.(type) {
	case map[string]string:
		return m
	case map[string]any:
		result := make(map[string]string, len(m))
		for k, v := range m {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				result[strings.TrimSpace(k)] = s
			}
		}
		return result
	default:
		return map[string]string{}
	}
}

func applyThreadAliases(threads []contracts.ThreadListItem, aliases map[string]string) {
	if len(threads) == 0 || len(aliases) == 0 {
		return
	}
	for i := range threads {
		id := strings.TrimSpace(threads[i].ID)
		if id == "" {
			continue
		}
		alias := strings.TrimSpace(aliases[id])
		if alias == "" {
			continue
		}
		threads[i].Name = alias
	}
}

func applyThreadAliasesSnapshot(snapshot *uistate.RuntimeSnapshot, aliases map[string]string) {
	if snapshot == nil || len(snapshot.Threads) == 0 || len(aliases) == 0 {
		return
	}
	for i := range snapshot.Threads {
		id := strings.TrimSpace(snapshot.Threads[i].ID)
		if id == "" {
			continue
		}
		alias := strings.TrimSpace(aliases[id])
		if alias == "" {
			continue
		}
		snapshot.Threads[i].Name = alias
		meta := snapshot.AgentMetaByID[id]
		meta.Alias = alias
		snapshot.AgentMetaByID[id] = meta
	}
}

func persistResolvedUIPreference(ctx context.Context, manager *uistate.PreferenceManager, key, resolved string, original any) {
	if manager == nil {
		return
	}
	if resolved == asString(original) {
		return
	}
	if err := manager.Set(ctx, key, resolved); err != nil {
		logger.Warn("ui/state/get: persist resolved preference failed",
			logger.FieldKey, key,
			logger.FieldError, err,
		)
	}
}

func resolveMainAgentPreference(snapshot uistate.RuntimeSnapshot, prefs map[string]any) string {
	preferred := strings.TrimSpace(asString(prefs["mainAgentId"]))
	if hasThread(snapshot.Threads, preferred) {
		return preferred
	}

	for _, thread := range snapshot.Threads {
		id := strings.TrimSpace(thread.ID)
		if id == "" {
			continue
		}
		meta := snapshot.AgentMetaByID[id]
		if meta.IsMain {
			return id
		}
	}

	for _, thread := range snapshot.Threads {
		id := strings.TrimSpace(thread.ID)
		if id == "" {
			continue
		}
		meta := snapshot.AgentMetaByID[id]
		if looksLikeMainAgent(thread.Name) || looksLikeMainAgent(meta.Alias) {
			return id
		}
	}
	return ""
}

func resolvePreferredThreadID(threads []uistate.ThreadSnapshot, preferred string) string {
	id := strings.TrimSpace(preferred)
	if hasThread(threads, id) {
		return id
	}
	return firstThreadID(threads)
}

func resolvePreferredCmdThreadID(threads []uistate.ThreadSnapshot, mainAgentID, preferred string) string {
	mainID := strings.TrimSpace(mainAgentID)
	candidates := make([]uistate.ThreadSnapshot, 0, len(threads))
	for _, thread := range threads {
		id := strings.TrimSpace(thread.ID)
		if id == "" {
			continue
		}
		if mainID != "" && id == mainID {
			continue
		}
		candidates = append(candidates, thread)
	}
	if len(candidates) == 0 {
		candidates = threads
	}
	return resolvePreferredThreadID(candidates, preferred)
}

func hasThread(threads []uistate.ThreadSnapshot, id string) bool {
	target := strings.TrimSpace(id)
	if target == "" {
		return false
	}
	for _, thread := range threads {
		if strings.TrimSpace(thread.ID) == target {
			return true
		}
	}
	return false
}

func firstThreadID(threads []uistate.ThreadSnapshot) string {
	for _, thread := range threads {
		id := strings.TrimSpace(thread.ID)
		if id != "" {
			return id
		}
	}
	return ""
}

func looksLikeMainAgent(name string) bool {
	value := strings.ToLower(strings.TrimSpace(name))
	if value == "" {
		return false
	}
	return strings.Contains(value, "主agent") ||
		strings.Contains(value, "主 agent") ||
		strings.Contains(value, "main agent") ||
		value == "main"
}
