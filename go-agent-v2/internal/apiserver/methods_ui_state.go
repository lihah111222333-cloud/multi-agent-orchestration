package apiserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
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

	switch p.Key {
	case "stallThresholdSec":
		if sec := asPositiveInt(p.Value, 30); sec > 0 {
			if s.codexAdapter != nil {
				s.codexAdapter.SetStallThreshold(time.Duration(sec) * time.Second)
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
	if value, ok := prefs[prefThreadArchivesChat]; ok {
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

// normalizeThreadAliases parses supported preference encodings into alias map.
func normalizeThreadAliases(value any) map[string]string {
	aliases := map[string]string{}

	switch typed := value.(type) {
	case map[string]string:
		for threadID, alias := range typed {
			addNormalizedThreadAlias(aliases, threadID, alias)
		}
	case map[string]any:
		for threadID, alias := range typed {
			addNormalizedThreadAlias(aliases, threadID, alias)
		}
	case string:
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &decoded); err == nil {
			for threadID, alias := range decoded {
				addNormalizedThreadAlias(aliases, threadID, alias)
			}
		}
	case json.RawMessage:
		decoded := map[string]any{}
		if err := json.Unmarshal(typed, &decoded); err == nil {
			for threadID, alias := range decoded {
				addNormalizedThreadAlias(aliases, threadID, alias)
			}
		}
	}
	return aliases
}

func addNormalizedThreadAlias(aliases map[string]string, threadID string, alias any) {
	if aliases == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	name := strings.TrimSpace(threadAliasStringValue(alias))
	if name == "" || name == id {
		return
	}
	aliases[id] = name
}

func threadAliasStringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
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

const (
	prefProjectsList   = "projects.list"
	prefProjectsActive = "projects.active"
)

type uiProjectsAddParams struct {
	Path string `json:"path"`
}

type uiProjectsRemoveParams struct {
	Path string `json:"path"`
}

type uiProjectsSetActiveParams struct {
	Path string `json:"path"`
}

func normalizeProjectPath(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	if value != "/" && !isWindowsDriveRoot(value) {
		value = strings.TrimRight(value, "\\/")
	}
	return value
}

func isWindowsDriveRoot(path string) bool {
	if len(path) == 2 {
		return isASCIILetter(path[0]) && path[1] == ':'
	}
	if len(path) == 3 {
		return isASCIILetter(path[0]) && path[1] == ':' && (path[2] == '/' || path[2] == '\\')
	}
	return false
}

func isASCIILetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func containsProject(projects []string, target string) bool {
	for _, item := range projects {
		if item == target {
			return true
		}
	}
	return false
}

func appendUniqueNormalizedProject(projects *[]string, path string) {
	if projects == nil {
		return
	}
	normalized := normalizeProjectPath(path)
	if normalized == "" || normalized == "." {
		return
	}
	if containsProject(*projects, normalized) {
		return
	}
	*projects = append(*projects, normalized)
}

func parseProjectsList(value any) []string {
	projects := []string{}

	switch list := value.(type) {
	case []string:
		for _, item := range list {
			appendUniqueNormalizedProject(&projects, item)
		}
	case []any:
		for _, item := range list {
			appendUniqueNormalizedProject(&projects, asString(item))
		}
	}

	return projects
}

func readProjectsState(s *Server, ctx context.Context) ([]string, string, error) {
	if s.prefManager == nil {
		return []string{}, ".", nil
	}

	prefs, err := s.prefManager.GetAll(ctx)
	if err != nil {
		return nil, "", err
	}
	projects := parseProjectsList(prefs[prefProjectsList])
	active := normalizeProjectPath(asString(prefs[prefProjectsActive]))
	if active == "" {
		active = "."
	}
	if active != "." && !containsProject(projects, active) {
		active = "."
	}
	return projects, active, nil
}

func writeProjectsState(s *Server, ctx context.Context, projects []string, active string) error {
	if s.prefManager == nil {
		return nil
	}

	normalizedProjects := parseProjectsList(projects)
	normalizedActive := normalizeProjectPath(active)
	if normalizedActive == "" {
		normalizedActive = "."
	}
	if normalizedActive != "." && !containsProject(normalizedProjects, normalizedActive) {
		normalizedActive = "."
	}

	if err := s.prefManager.Set(ctx, prefProjectsList, normalizedProjects); err != nil {
		return err
	}
	if err := s.prefManager.Set(ctx, prefProjectsActive, normalizedActive); err != nil {
		return err
	}
	return nil
}

func uiProjectsGet(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	projects, active, err := readProjectsState(s, ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": projects,
		"active":   active,
	}, nil
}

func uiProjectsAdd(s *Server, ctx context.Context, p uiProjectsAddParams) (any, error) {
	projects, _, err := readProjectsState(s, ctx)
	if err != nil {
		return nil, err
	}
	next := normalizeProjectPath(p.Path)
	if next == "" || next == "." {
		return map[string]any{
			"projects": projects,
			"active":   ".",
		}, nil
	}
	if !containsProject(projects, next) {
		projects = append(projects, next)
	}
	if err := writeProjectsState(s, ctx, projects, next); err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": projects,
		"active":   next,
	}, nil
}

func uiProjectsRemove(s *Server, ctx context.Context, p uiProjectsRemoveParams) (any, error) {
	projects, active, err := readProjectsState(s, ctx)
	if err != nil {
		return nil, err
	}
	target := normalizeProjectPath(p.Path)
	next := make([]string, 0, len(projects))
	for _, item := range projects {
		if item == target {
			continue
		}
		next = append(next, item)
	}
	if active == target {
		active = "."
	}
	if err := writeProjectsState(s, ctx, next, active); err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": next,
		"active":   active,
	}, nil
}

func uiProjectsSetActive(s *Server, ctx context.Context, p uiProjectsSetActiveParams) (any, error) {
	projects, _, err := readProjectsState(s, ctx)
	if err != nil {
		return nil, err
	}
	next := normalizeProjectPath(p.Path)
	if next == "" || (next != "." && !containsProject(projects, next)) {
		next = "."
	}
	if err := writeProjectsState(s, ctx, projects, next); err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": projects,
		"active":   next,
	}, nil
}

const (
	defaultCodeOpenContextLines = 90
	maxCodeOpenContextLines     = 180
	maxCodeOpenFileBytes        = 64 << 20 // 64MB
	maxInlineImageDataURLBytes  = 8 << 20  // 8MB
	binaryProbeBytes            = 8 << 10  // 8KB
)

type uiCodeOpenParams struct {
	FilePath string   `json:"filePath"`
	Line     int      `json:"line"`
	Column   int      `json:"column"`
	Context  int      `json:"context"`
	Project  string   `json:"project"`
	Projects []string `json:"projects"`
}

func normalizeCodeReferencePath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = strings.Trim(value, `"'`)
	if parsed, err := url.Parse(value); err == nil && strings.EqualFold(parsed.Scheme, "file") {
		value = filepath.FromSlash(parsed.Path)
	}
	return strings.TrimSpace(value)
}

func appendNormalizedProjectRoot(roots *[]string, seen map[string]struct{}, raw string) {
	if roots == nil {
		return
	}
	normalized := normalizeProjectPath(raw)
	if normalized == "" || normalized == "." {
		return
	}
	key := strings.ToLower(filepath.Clean(normalized))
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	*roots = append(*roots, normalized)
}

func normalizeProjectRoots(project string, projects []string) []string {
	seen := map[string]struct{}{}
	roots := make([]string, 0, len(projects)+2)
	appendNormalizedProjectRoot(&roots, seen, project)
	for _, item := range projects {
		appendNormalizedProjectRoot(&roots, seen, item)
	}
	return roots
}

func resolveCodeReferenceFilePath(rawPath, project string, projects []string) (string, error) {
	path := normalizeCodeReferencePath(rawPath)
	if path == "" {
		return "", apperrors.New("Server.uiCodeOpen", "filePath is required")
	}

	if filepath.IsAbs(path) && util.FileExists(path) {
		return path, nil
	}

	candidates := []string{path}
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		candidates = append(candidates, strings.TrimSpace(path[2:]))
	}

	roots := normalizeProjectRoots(project, projects)
	for _, relPath := range candidates {
		for _, root := range roots {
			joined := filepath.Join(root, relPath)
			if util.FileExists(joined) {
				return filepath.Clean(joined), nil
			}
		}
	}

	for _, relPath := range candidates {
		abs, err := filepath.Abs(relPath)
		if err != nil {
			continue
		}
		if util.FileExists(abs) {
			return abs, nil
		}
	}
	return "", apperrors.Newf("Server.uiCodeOpen", "file not found: %s", path)
}

func clampCodeContextLines(value int) int {
	if value <= 0 {
		return defaultCodeOpenContextLines
	}
	if value > maxCodeOpenContextLines {
		return maxCodeOpenContextLines
	}
	return value
}

func clampLine(value, total int) int {
	if total <= 0 {
		return 1
	}
	if value <= 0 {
		return 1
	}
	if value > total {
		return total
	}
	return value
}

func clampColumn(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func codePathToURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	normalized := filepath.ToSlash(abs)
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return (&url.URL{Scheme: "file", Path: normalized}).String()
}

func gatherCodeDiagnostics(s *Server, filePath string, startLine, endLine int) []map[string]any {
	if s == nil {
		return []map[string]any{}
	}
	uri := codePathToURI(filePath)
	diags := getDiagnostics(s, uri)
	if len(diags) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(diags))
	for _, diag := range diags {
		line := diag.Range.Start.Line + 1
		column := diag.Range.Start.Character + 1
		if line < startLine || line > endLine {
			continue
		}
		result = append(result, map[string]any{
			"line":     line,
			"column":   column,
			"severity": diag.Severity.String(),
			"message":  diag.Message,
		})
	}
	return result
}

func buildCodeSnippet(lines []string, startLine, endLine int) []map[string]any {
	if startLine <= 0 || endLine < startLine {
		return []map[string]any{}
	}
	snippet := make([]map[string]any, 0, endLine-startLine+1)
	for line := startLine; line <= endLine; line++ {
		text := ""
		idx := line - 1
		if idx >= 0 && idx < len(lines) {
			text = lines[idx]
		}
		snippet = append(snippet, map[string]any{
			"line": line,
			"text": text,
		})
	}
	return snippet
}

func looksLikeBinaryContent(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	sample := content
	if len(sample) > binaryProbeBytes {
		sample = sample[:binaryProbeBytes]
	}
	nonTextBytes := 0
	for _, b := range sample {
		switch b {
		case 0:
			return true
		case '\n', '\r', '\t':
			continue
		}
		if b < 0x20 || b == 0x7f {
			nonTextBytes++
		}
	}

	return nonTextBytes*100 >= len(sample)*15
}

func detectMediaType(path string, content []byte) string {

	if byExt := mediaTypeByExtension(path); byExt != "" {
		return byExt
	}
	if len(content) > 0 {
		sniff := content
		if len(sniff) > 512 {
			sniff = sniff[:512]
		}
		detected := strings.TrimSpace(http.DetectContentType(sniff))
		if detected != "" && detected != "application/octet-stream" {
			return detected
		}
	}
	return "application/octet-stream"
}

func mediaTypeByExtension(path string) string {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	default:
		return ""
	}
}

func isImagePreviewExtension(path string) bool {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(filepath.Ext(path)), ".")) {
	case "png", "jpg", "jpeg", "svg":
		return true
	default:
		return false
	}
}

func imageDataURL(mediaType string, content []byte) string {
	if strings.TrimSpace(mediaType) == "" || len(content) == 0 {
		return ""
	}
	if len(content) > maxInlineImageDataURLBytes {
		return ""
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(content)
}

func resolveCodeOpenRelativePath(resolvedPath, project string, projects []string) string {
	relativePath := resolvedPath
	for _, root := range normalizeProjectRoots(project, projects) {
		rel, relErr := filepath.Rel(root, resolvedPath)
		if relErr != nil {
			continue
		}
		rel = filepath.Clean(rel)
		if rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		relativePath = filepath.ToSlash(rel)
		break
	}
	return relativePath
}

func fileLanguageByPath(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return "text"
	}
	switch ext {
	case "go":
		return "go"
	case "rs":
		return "rust"
	case "ts", "tsx":
		return "typescript"
	case "js", "jsx":
		return "javascript"
	case "py":
		return "python"
	case "c", "h", "hpp", "cpp", "cc":
		return "c"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "md":
		return "markdown"
	case "markdown":
		return "markdown"
	case "css":
		return "css"
	case "html":
		return "html"
	case "java":
		return "java"
	case "kt":
		return "kotlin"
	case "swift":
		return "swift"
	default:
		return ext
	}
}

func isMarkdownFilePath(path string) bool {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	return strings.EqualFold(ext, "md") || strings.EqualFold(ext, "markdown")
}

func uiCodeOpenTyped(s *Server, _ context.Context, p uiCodeOpenParams) (any, error) {
	logger.Info("ui/code/open: begin",
		"file_path", strings.TrimSpace(p.FilePath),
		"line", p.Line,
		"column", p.Column,
		"project", strings.TrimSpace(p.Project),
		"projects_count", len(p.Projects),
	)

	resolvedPath, err := resolveCodeReferenceFilePath(p.FilePath, p.Project, p.Projects)
	if err != nil {
		logger.Warn("ui/code/open: resolve path failed",
			"file_path", strings.TrimSpace(p.FilePath),
			"project", strings.TrimSpace(p.Project),
			"projects_count", len(p.Projects),
			logger.FieldError, err,
		)
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		logger.Warn("ui/code/open: stat failed",
			"resolved_path", resolvedPath,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrap(err, "Server.uiCodeOpen", "stat file")
	}
	if info.IsDir() {
		logger.Warn("ui/code/open: path is directory",
			"resolved_path", resolvedPath,
		)
		return nil, apperrors.Newf("Server.uiCodeOpen", "path is directory: %s", resolvedPath)
	}
	lspSupported := supportsLSPFileType(resolvedPath)
	if info.Size() > maxCodeOpenFileBytes {
		logger.Warn("ui/code/open: file too large",
			"resolved_path", resolvedPath,
			"size_bytes", info.Size(),
			"max_bytes", maxCodeOpenFileBytes,
		)
		return nil, apperrors.Newf("Server.uiCodeOpen", "file too large: %d bytes", info.Size())
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		logger.Warn("ui/code/open: read failed",
			"resolved_path", resolvedPath,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrap(err, "Server.uiCodeOpen", "read file")
	}
	relativePath := resolveCodeOpenRelativePath(resolvedPath, p.Project, p.Projects)
	if isImagePreviewExtension(resolvedPath) {
		mediaType := detectMediaType(resolvedPath, content)
		targetLine := 1
		if p.Line > 0 {
			targetLine = p.Line
		}
		fileURL := codePathToURI(resolvedPath)
		previewURL := fileURL
		thumbnailURL := fileURL
		if inlineURL := imageDataURL(mediaType, content); inlineURL != "" {
			previewURL = inlineURL
			thumbnailURL = inlineURL
		}
		logger.Info("ui/code/open: image parser applied",
			"resolved_path", resolvedPath,
			"relative_path", relativePath,
			"media_type", mediaType,
			"size_bytes", len(content),
		)
		return map[string]any{
			"ok":           true,
			"filePath":     resolvedPath,
			"relative":     relativePath,
			"line":         targetLine,
			"column":       clampColumn(p.Column),
			"startLine":    1,
			"endLine":      1,
			"totalLines":   1,
			"language":     fileLanguageByPath(resolvedPath),
			"context":      0,
			"snippet":      []map[string]any{{"line": 1, "text": fmt.Sprintf("[image preview: %s, %d bytes]", mediaType, len(content))}},
			"diagnostics":  []map[string]any{},
			"lspOpened":    false,
			"binary":       looksLikeBinaryContent(content),
			"mediaType":    mediaType,
			"sizeBytes":    len(content),
			"image":        true,
			"plugin":       "image-parser",
			"previewURL":   previewURL,
			"thumbnailURL": thumbnailURL,
		}, nil
	}

	if looksLikeBinaryContent(content) {
		mediaType := detectMediaType(resolvedPath, content)
		targetLine := 1
		if p.Line > 0 {
			targetLine = p.Line
		}
		logger.Info("ui/code/open: binary content detected",
			"resolved_path", resolvedPath,
			"relative_path", relativePath,
			"media_type", mediaType,
			"size_bytes", len(content),
		)
		return map[string]any{
			"ok":         true,
			"filePath":   resolvedPath,
			"relative":   relativePath,
			"line":       targetLine,
			"column":     clampColumn(p.Column),
			"startLine":  1,
			"endLine":    1,
			"totalLines": 1,
			"language":   fileLanguageByPath(resolvedPath),
			"context":    0,
			"snippet": []map[string]any{
				{
					"line": 1,
					"text": fmt.Sprintf("[binary file omitted: %s, %d bytes]", mediaType, len(content)),
				},
			},
			"diagnostics": []map[string]any{},
			"lspOpened":   false,
			"binary":      true,
			"mediaType":   mediaType,
			"sizeBytes":   len(content),
		}, nil
	}

	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}

	targetLine := clampLine(p.Line, len(lines))
	targetColumn := clampColumn(p.Column)
	contextLines := clampCodeContextLines(p.Context)
	startLine := targetLine - contextLines
	if startLine < 1 {
		startLine = 1
	}
	endLine := targetLine + contextLines
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if isMarkdownFilePath(resolvedPath) {
		startLine = 1
		endLine = len(lines)
		contextLines = len(lines)
	}

	lspOpened := false
	if s.lsp != nil && lspSupported {
		_ = s.lsp.OpenFile(resolvedPath, string(content))
		lspOpened = true
	}
	diagnostics := gatherCodeDiagnostics(s, resolvedPath, startLine, endLine)

	result := map[string]any{
		"ok":          true,
		"filePath":    resolvedPath,
		"relative":    relativePath,
		"line":        targetLine,
		"column":      targetColumn,
		"startLine":   startLine,
		"endLine":     endLine,
		"totalLines":  len(lines),
		"language":    fileLanguageByPath(resolvedPath),
		"context":     contextLines,
		"snippet":     buildCodeSnippet(lines, startLine, endLine),
		"diagnostics": diagnostics,
		"lspOpened":   lspOpened,
	}

	logger.Info("ui/code/open: success",
		"resolved_path", resolvedPath,
		"relative_path", relativePath,
		"line", targetLine,
		"column", targetColumn,
		"snippet_lines", endLine-startLine+1,
		"diagnostics_count", len(diagnostics),
		"lsp_opened", lspOpened,
	)

	return result, nil
}

func supportsLSPFileType(path string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return false
	}
	if ext == "md" || ext == "markdown" {
		return true
	}
	for _, item := range lsp.DefaultServers {
		for _, supportedExt := range item.Extensions {
			if supportedExt == ext {
				return true
			}
		}
	}
	return false
}
