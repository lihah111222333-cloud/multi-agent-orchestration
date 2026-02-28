package dashboard

import (
	"encoding/json"
	"maps"
	"strconv"
	"strings"
)

const (
	PrefThreadAliases            = "threads.aliases"
	PrefThreadArchivesChat       = "threadArchives.chat"
	PrefShowInjectedPromptInChat = "settings.showInjectedPromptInChat"
	PrefMainAgentID              = "mainAgentId"
	PrefActiveThreadID           = "activeThreadId"
	PrefActiveCmdThreadID        = "activeCmdThreadId"
)

type PreferenceSideEffects struct {
	MainAgentID              string
	StallThresholdSec        int
	StallHeartbeatSec        int
	ShowInjectedPromptInChat *bool
}

func ResolvePreferenceSideEffects(key string, value any) PreferenceSideEffects {
	switch strings.TrimSpace(key) {
	case PrefMainAgentID:
		return PreferenceSideEffects{MainAgentID: AsString(value)}
	case "stallThresholdSec":
		return PreferenceSideEffects{StallThresholdSec: AsPositiveInt(value, 30)}
	case "stallHeartbeatSec":
		return PreferenceSideEffects{StallHeartbeatSec: AsPositiveInt(value, 10)}
	case PrefShowInjectedPromptInChat:
		show := AsBool(value, false)
		return PreferenceSideEffects{ShowInjectedPromptInChat: &show}
	}
	return PreferenceSideEffects{}
}

type StateThread struct {
	ID   string
	Name string
}

type StateAgentMeta struct {
	Alias  string
	IsMain bool
}

type StateResolutionInput struct {
	Threads       []StateThread
	AgentMetaByID map[string]StateAgentMeta
	Prefs         map[string]any
}

type StateResolution struct {
	Aliases                map[string]string
	ResolvedMainAgentID    string
	ResolvedActiveThreadID string
	ResolvedActiveCmdID    string
}

func ResolveState(input StateResolutionInput) StateResolution {
	aliases := NormalizeThreadAliases(input.Prefs[PrefThreadAliases])
	threads := make([]StateThread, 0, len(input.Threads))
	for _, thread := range input.Threads {
		t := StateThread{ID: strings.TrimSpace(thread.ID), Name: thread.Name}
		if alias := strings.TrimSpace(aliases[t.ID]); alias != "" {
			t.Name = alias
		}
		threads = append(threads, t)
	}
	meta := maps.Clone(input.AgentMetaByID)
	if meta == nil {
		meta = map[string]StateAgentMeta{}
	}
	for id, alias := range aliases {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(alias) == "" {
			continue
		}
		item := meta[id]
		item.Alias = alias
		meta[id] = item
	}

	resolvedMain := resolveMainAgentPreference(threads, meta, AsString(input.Prefs[PrefMainAgentID]))
	resolvedActive := resolvePreferredThreadID(threads, AsString(input.Prefs[PrefActiveThreadID]))
	resolvedActiveCmd := resolvePreferredCmdThreadID(threads, resolvedMain, AsString(input.Prefs[PrefActiveCmdThreadID]))

	return StateResolution{
		Aliases:                aliases,
		ResolvedMainAgentID:    resolvedMain,
		ResolvedActiveThreadID: resolvedActive,
		ResolvedActiveCmdID:    resolvedActiveCmd,
	}
}

type AgentRuntimeItem struct {
	ID            string
	State         string
	Port          int
	CodexThreadID string
}

func BuildAgentRuntimeByID(items []AgentRuntimeItem) map[string]map[string]any {
	result := map[string]map[string]any{}
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		entry := map[string]any{"state": item.State}
		if item.Port > 0 {
			entry["port"] = item.Port
		}
		if codexThreadID := strings.TrimSpace(item.CodexThreadID); codexThreadID != "" {
			entry["codexThreadId"] = codexThreadID
		}
		result[id] = entry
	}
	return result
}

type UIStateResultInput struct {
	Threads               any
	Statuses              map[string]string
	InterruptibleByThread map[string]bool
	StatusHeadersByThread map[string]string
	StatusDetailsByThread map[string]string
	TimelinesByThread     map[string]any
	DiffTextByThread      map[string]string
	TokenUsageByThread    any
	AgentMetaByID         any
	WorkspaceRunsByKey    any
	ActiveThreadID        string
	ActiveCmdThreadID     string
	MainAgentID           string
	ActivityStatsByThread any
	AlertsByThread        any
	AgentRuntimeByID      map[string]map[string]any

	WorkspaceFeatureEnabled *bool
	WorkspaceLastError      string

	ViewPrefsChat      any
	HasViewPrefsChat   bool
	ViewPrefsCmd       any
	HasViewPrefsCmd    bool
	ThreadPinsChat     any
	HasThreadPinsChat  bool
	ThreadArchivesChat any
	HasThreadArchives  bool
	ShowInjectedPrompt any
	HasShowInjected    bool
}

func BuildUIStateResult(input UIStateResultInput) map[string]any {
	result := map[string]any{
		"threads":               input.Threads,
		"statuses":              input.Statuses,
		"interruptibleByThread": input.InterruptibleByThread,
		"statusHeadersByThread": input.StatusHeadersByThread,
		"statusDetailsByThread": input.StatusDetailsByThread,
		"timelinesByThread":     input.TimelinesByThread,
		"diffTextByThread":      input.DiffTextByThread,
		"tokenUsageByThread":    input.TokenUsageByThread,
		"agentMetaById":         input.AgentMetaByID,
		"workspaceRunsByKey":    input.WorkspaceRunsByKey,
		"activeThreadId":        input.ActiveThreadID,
		"activeCmdThreadId":     input.ActiveCmdThreadID,
		"mainAgentId":           input.MainAgentID,
		"activityStatsByThread": input.ActivityStatsByThread,
		"alertsByThread":        input.AlertsByThread,
		"agentRuntimeById":      input.AgentRuntimeByID,
	}
	if input.WorkspaceFeatureEnabled != nil {
		result["workspaceFeatureEnabled"] = *input.WorkspaceFeatureEnabled
	}
	if input.WorkspaceLastError != "" {
		result["workspaceLastError"] = input.WorkspaceLastError
	}
	if input.HasViewPrefsChat {
		result["viewPrefs.chat"] = input.ViewPrefsChat
	}
	if input.HasViewPrefsCmd {
		result["viewPrefs.cmd"] = input.ViewPrefsCmd
	}
	if input.HasThreadPinsChat {
		result["threadPins.chat"] = input.ThreadPinsChat
	}
	if input.HasThreadArchives {
		result[PrefThreadArchivesChat] = input.ThreadArchivesChat
	}
	if input.HasShowInjected {
		result[PrefShowInjectedPromptInChat] = input.ShowInjectedPrompt
	}
	return result
}

func resolveMainAgentPreference(threads []StateThread, meta map[string]StateAgentMeta, preferred string) string {
	pref := strings.TrimSpace(preferred)
	if pref != "" {
		for _, thread := range threads {
			if strings.TrimSpace(thread.ID) == pref {
				return pref
			}
		}
	}
	fallback := ""
	for _, thread := range threads {
		id := strings.TrimSpace(thread.ID)
		if id == "" {
			continue
		}
		item := meta[id]
		if item.IsMain {
			return id
		}
		if fallback == "" && (looksLikeMainAgent(thread.Name) || looksLikeMainAgent(item.Alias)) {
			fallback = id
		}
	}
	return fallback
}

func resolvePreferredThreadID(threads []StateThread, preferred string) string {
	pref := strings.TrimSpace(preferred)
	first := ""
	for _, thread := range threads {
		id := strings.TrimSpace(thread.ID)
		if id == "" {
			continue
		}
		if first == "" {
			first = id
		}
		if id == pref {
			return id
		}
	}
	return first
}

func resolvePreferredCmdThreadID(threads []StateThread, mainAgentID, preferred string) string {
	mainID := strings.TrimSpace(mainAgentID)
	candidates := make([]StateThread, 0, len(threads))
	for _, thread := range threads {
		id := strings.TrimSpace(thread.ID)
		if id == "" || (mainID != "" && id == mainID) {
			continue
		}
		candidates = append(candidates, thread)
	}
	if len(candidates) == 0 {
		candidates = threads
	}
	return resolvePreferredThreadID(candidates, preferred)
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

func AsString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	}
	return ""
}

func AsPositiveInt(value any, minVal int) int {
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

func AsBool(value any, fallback bool) bool {
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

func NormalizeThreadAliases(value any) map[string]string {
	switch m := value.(type) {
	case map[string]string:
		return m
	case map[string]any:
		result := make(map[string]string, len(m))
		for k, v := range m {
			id := strings.TrimSpace(k)
			alias := strings.TrimSpace(AsString(v))
			if id == "" || alias == "" {
				continue
			}
			result[id] = alias
		}
		return result
	default:
		return map[string]string{}
	}
}
