// methods_ui_state.go — UI 偏好/状态管理 JSON-RPC 方法 (preferences, state, thread aliases)。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const prefThreadAliases = "threads.aliases"

type uiPrefGetParams struct {
	Key string `json:"key"`
}

func (s *Server) uiPreferencesGet(ctx context.Context, p uiPrefGetParams) (any, error) {
	return s.prefManager.Get(ctx, p.Key)
}

type uiPrefSetParams struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func (s *Server) uiPreferencesSet(ctx context.Context, p uiPrefSetParams) (any, error) {
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
			s.stallThreshold = time.Duration(sec) * time.Second
			logger.Info("stall threshold updated via ui/preferences/set", "seconds", sec)
		}
	case "stallHeartbeatSec":
		if sec := asPositiveInt(p.Value, 10); sec > 0 {
			s.stallHeartbeat = time.Duration(sec) * time.Second
			logger.Info("stall heartbeat updated via ui/preferences/set", "seconds", sec)
		}
	}
	return map[string]any{"ok": true}, nil
}

func (s *Server) uiPreferencesGetAll(ctx context.Context, _ json.RawMessage) (any, error) {
	return s.prefManager.GetAll(ctx)
}

func (s *Server) uiStateGet(ctx context.Context, _ json.RawMessage) (any, error) {
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

	// 按需获取活跃线程的 timeline/diff, 避免深拷贝所有线程
	timelinesByThread := map[string][]uistate.TimelineItem{}
	diffTextByThread := map[string]string{}
	activeIDs := []string{resolvedActiveThreadID, resolvedActiveCmdThreadID}
	for _, tid := range activeIDs {
		tid = strings.TrimSpace(tid)
		if tid == "" {
			continue
		}
		if _, ok := timelinesByThread[tid]; ok {
			continue
		}
		timelinesByThread[tid] = s.uiRuntime.ThreadTimeline(tid)
		diffTextByThread[tid] = s.uiRuntime.ThreadDiff(tid)
	}

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

func (s *Server) persistThreadAlias(ctx context.Context, threadID, alias string) error {
	s.threadAliasMu.Lock()
	defer s.threadAliasMu.Unlock()
	return persistThreadAliasPreference(ctx, s.prefManager, threadID, alias)
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

func (s *Server) loadThreadAliases(ctx context.Context) map[string]string {
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

func normalizeThreadAliases(value any) map[string]string {
	aliases := map[string]string{}
	addAlias := func(threadID string, alias any) {
		id := strings.TrimSpace(threadID)
		if id == "" {
			return
		}
		name := strings.TrimSpace(asString(alias))
		if name == "" || name == id {
			return
		}
		aliases[id] = name
	}

	switch typed := value.(type) {
	case map[string]string:
		for threadID, alias := range typed {
			addAlias(threadID, alias)
		}
	case map[string]any:
		for threadID, alias := range typed {
			addAlias(threadID, alias)
		}
	case string:
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &decoded); err == nil {
			for threadID, alias := range decoded {
				addAlias(threadID, alias)
			}
		}
	case json.RawMessage:
		decoded := map[string]any{}
		if err := json.Unmarshal(typed, &decoded); err == nil {
			for threadID, alias := range decoded {
				addAlias(threadID, alias)
			}
		}
	}

	return aliases
}

func applyThreadAliases(threads []threadListItem, aliases map[string]string) {
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
