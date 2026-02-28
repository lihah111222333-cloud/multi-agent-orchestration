package codexadapter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	lifecyclesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/lifecycle"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func defaultAllSchemaProvider() []codexsdk.DynamicTool { return nil }
func defaultNowUnixMilliProvider() int64               { return time.Now().UnixMilli() }
func defaultSetAgentWorkDirProvider(string, string)    {}
func defaultCancelCodeRunsProvider(string) int         { return 0 }
func defaultReadSkillContentProvider(string) (string, error) {
	return "", appErrors.New("codexadapter.readSkillContent", "server context is not configured")
}
func defaultListSkillMatchCandidatesProvider() ([]contracts.SkillMatchCandidate, error) {
	return nil, appErrors.New("codexadapter.listSkillMatchCandidates", "server context is not configured")
}
func defaultGetAgentSkillsProvider(string) []string { return nil }
func defaultNotifyProvider(string, any)             {}

func requireThreadID(caller, threadID string) (string, error) {
	if id := strings.TrimSpace(threadID); id != "" {
		return id, nil
	}
	return "", appErrors.New(caller, "threadId is required")
}

func threadLogFields(threadID string) []any {
	threadID = strings.TrimSpace(threadID)
	return []any{logger.FieldAgentID, threadID, logger.FieldThreadID, threadID}
}

var errNoProcess = errors.New("codexadapter: agent process not found")

func withClientE[T any](proc *codexsdk.AgentProcess, fn func(codexsdk.Client) (T, error)) (zero T, err error) {
	if proc == nil || proc.Client == nil {
		return zero, errNoProcess
	}
	return fn(proc.Client)
}

func withClient(proc *codexsdk.AgentProcess, fn func(codexsdk.Client) error) error {
	_, err := withClientE(proc, func(c codexsdk.Client) (struct{}, error) { return struct{}{}, fn(c) })
	return err
}

func toLifecycleAgentInfos(items []codexsdk.AgentInfo) []lifecyclesvc.AgentInfo {
	return mapSlice(items, func(item codexsdk.AgentInfo) lifecyclesvc.AgentInfo {
		return lifecyclesvc.AgentInfo{ID: item.ID, Name: item.Name, State: string(item.State), Port: item.Port, ThreadID: item.ThreadID}
	})
}

func toRuntimeThreadSnapshots(items []lifecyclesvc.AgentInfo) []uistate.ThreadSnapshot {
	return mapSlice(items, func(item lifecyclesvc.AgentInfo) uistate.ThreadSnapshot {
		return uistate.ThreadSnapshot{ID: item.ID, Name: item.Name, State: item.State}
	})
}

func threadExistsInRuntime(threadID string, runtime *uistate.RuntimeManager) bool {
	if runtime == nil {
		return false
	}
	return lifecyclesvc.ThreadExistsInRuntimeSnapshots(threadID, mapSlice(runtime.SnapshotLight().Threads, func(item uistate.ThreadSnapshot) lifecyclesvc.ThreadSnapshot {
		return lifecyclesvc.ThreadSnapshot{ID: item.ID}
	}))
}

func fuzzyFileSearch(query string, roots []string, fuzzyMatch func(text, pattern string) bool) []map[string]any {
	pattern := strings.ToLower(strings.TrimSpace(query))
	var results []map[string]any
	if pattern == "" || fuzzyMatch == nil {
		return results
	}
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				base := filepath.Base(path)
				if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if fuzzyMatch(strings.ToLower(rel), pattern) {
				results = append(results, map[string]any{"root": root, "path": rel, "fileName": info.Name()})
				if len(results) >= 100 {
					return filepath.SkipAll
				}
			}
			return nil
		})
	}
	return results
}

func mapSlice[T any, R any](src []T, mapper func(T) R) []R {
	if len(src) == 0 {
		return nil
	}
	out := make([]R, len(src))
	for i, item := range src {
		out[i] = mapper(item)
	}
	return out
}
