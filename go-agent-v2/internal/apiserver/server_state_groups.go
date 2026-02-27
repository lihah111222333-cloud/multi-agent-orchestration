package apiserver

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/dashboard"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
)

type connManagerState struct {
	mu     sync.RWMutex
	conns  map[string]*connEntry
	nextID atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan *Response
	nextReqID atomic.Int64
}

func (s *connManagerState) connectionCount() int {
	if s == nil { return 0 }; s.mu.RLock(); defer s.mu.RUnlock(); return len(s.conns)
}

func (s *connManagerState) allocConnID() string {
	if s == nil { return "" }; return fmt.Sprintf("conn-%d", s.nextID.Add(1))
}

func (s *connManagerState) firstConnID() string {
	if s == nil { return "" }
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id := range s.conns {
		return id
	}
	return ""
}

func (s *connManagerState) connsSnapshot() map[string]*connEntry {
	if s == nil { return nil }
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := make(map[string]*connEntry, len(s.conns))
	maps.Copy(snapshot, s.conns)
	return snapshot
}

func (s *connManagerState) getConn(connID string) (*connEntry, bool) {
	if s == nil { return nil, false }
	s.mu.RLock(); defer s.mu.RUnlock()
	entry, ok := s.conns[connID]
	return entry, ok
}

func (s *connManagerState) addConn(connID string, entry *connEntry) {
	if s == nil || connID == "" || entry == nil { return }
	s.mu.Lock()
	if s.conns == nil { s.conns = make(map[string]*connEntry) }
	s.conns[connID] = entry
	s.mu.Unlock()
}

func (s *connManagerState) removeConn(connID string) (*connEntry, bool) {
	if s == nil || connID == "" { return nil, false }
	s.mu.Lock()
	entry, ok := s.conns[connID]
	if ok {
		delete(s.conns, connID)
	}
	s.mu.Unlock()
	return entry, ok
}

func (s *connManagerState) allocPendingRequest() (reqID int64, ch <-chan *Response, cleanup func()) {
	if s == nil { return 0, nil, func() {} }
	id := s.nextReqID.Add(1)
	respCh := make(chan *Response, 1)
	s.pendingMu.Lock()
	if s.pending == nil {
		s.pending = make(map[int64]chan *Response)
	}
	s.pending[id] = respCh
	s.pendingMu.Unlock()
	cleanup = func() {
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
	}
	return id, respCh, cleanup
}

func (s *connManagerState) pendingResponseChannel(reqID int64) (chan *Response, bool) {
	if s == nil { return nil, false }
	s.pendingMu.Lock(); ch, ok := s.pending[reqID]; s.pendingMu.Unlock()
	return ch, ok
}

func (s *connManagerState) deliverPendingResponse(reqID int64, resp *Response) (found bool, delivered bool) {
	ch, ok := s.pendingResponseChannel(reqID)
	if !ok {
		return false, false
	}
	select {
	case ch <- resp:
		return true, true
	default:
		return true, false
	}
}

type diagnosticsCacheState struct {
	diagMu    sync.RWMutex
	diagCache map[string][]lsp.Diagnostic
}

type codeRunState struct {
	codeRunMu      sync.Mutex
	activeCodeRuns map[string]map[string]context.CancelFunc
	codeRunSeq     atomic.Int64

	agentWorkDirMu sync.RWMutex
	agentWorkDirs  map[string]string
}

func (s *codeRunState) normalizeAgentID(agentID string) string {
	if s == nil { return "" }
	return strings.TrimSpace(agentID)
}

func (s *codeRunState) setAgentWorkDir(agentID, cwd string) {
	id := s.normalizeAgentID(agentID)
	if id == "" { return }
	normalized := tools.NormalizeAgentWorkDir(cwd)
	if normalized == "" { return }
	s.agentWorkDirMu.Lock()
	if s.agentWorkDirs == nil {
		s.agentWorkDirs = make(map[string]string)
	}
	s.agentWorkDirs[id] = normalized
	s.agentWorkDirMu.Unlock()
}

func (s *codeRunState) getAgentWorkDir(agentID string) string {
	id := s.normalizeAgentID(agentID)
	if id == "" { return "" }
	s.agentWorkDirMu.RLock(); cwd := s.agentWorkDirs[id]; s.agentWorkDirMu.RUnlock()
	return cwd
}

func (s *codeRunState) clearAgentWorkDir(agentID string) {
	id := s.normalizeAgentID(agentID)
	if id == "" { return }
	s.agentWorkDirMu.Lock(); delete(s.agentWorkDirs, id); s.agentWorkDirMu.Unlock()
}

func (s *codeRunState) clearAllAgentWorkDirs() {
	if s == nil { return }
	s.agentWorkDirMu.Lock(); clear(s.agentWorkDirs); s.agentWorkDirMu.Unlock()
}

func (s *codeRunState) withCodeRunsByAgentID(id string, create bool, fn func(runs map[string]context.CancelFunc)) {
	s.codeRunMu.Lock()
	defer s.codeRunMu.Unlock()
	if s.activeCodeRuns == nil {
		if !create { return }
		s.activeCodeRuns = make(map[string]map[string]context.CancelFunc)
	}
	runs := s.activeCodeRuns[id]
	if runs == nil {
		if !create { return }
		runs = make(map[string]context.CancelFunc)
		s.activeCodeRuns[id] = runs
	}
	fn(runs)
	if !create && len(runs) == 0 {
		delete(s.activeCodeRuns, id)
	}
}

func (s *codeRunState) takeCodeRunsByAgentID(id string) map[string]context.CancelFunc {
	s.codeRunMu.Lock(); defer s.codeRunMu.Unlock()
	runs := s.activeCodeRuns[id]; delete(s.activeCodeRuns, id)
	return runs
}

func (s *codeRunState) registerCodeRunCancel(agentID, callID string, cancel context.CancelFunc) string {
	if cancel == nil { return "" }
	id := s.normalizeAgentID(agentID)
	if id == "" { return "" }
	key := fmt.Sprintf("%s#%d", strings.TrimSpace(callID), s.codeRunSeq.Add(1))
	s.withCodeRunsByAgentID(id, true, func(runs map[string]context.CancelFunc) {
		runs[key] = cancel
	})
	return key
}

func (s *codeRunState) unregisterCodeRunCancel(agentID, runKey string) {
	id, key := s.normalizeAgentID(agentID), strings.TrimSpace(runKey)
	if id == "" || key == "" { return }
	s.withCodeRunsByAgentID(id, false, func(runs map[string]context.CancelFunc) {
		delete(runs, key)
	})
}

func (s *codeRunState) cancelCodeRuns(agentID string) int {
	id := s.normalizeAgentID(agentID)
	if id == "" { return 0 }
	runs := s.takeCodeRunsByAgentID(id)
	if len(runs) == 0 { return 0 }
	for _, cancel := range runs {
		cancel()
	}
	return len(runs)
}

func (s *codeRunState) cancelAllCodeRuns() int {
	if s == nil { return 0 }
	s.codeRunMu.Lock()
	all := s.activeCodeRuns
	s.activeCodeRuns = make(map[string]map[string]context.CancelFunc)
	s.codeRunMu.Unlock()
	total := 0
	for _, runs := range all {
		for _, cancel := range runs {
			cancel()
			total++
		}
	}
	return total
}

type turnTrackingState struct {
	threadSeq atomic.Int64

	fileChangeMu       sync.Mutex
	fileChangeByThread map[string][]string

	orchestrationReportMu       sync.Mutex
	orchestrationPendingReports map[string]map[string]time.Time
	orchestrationReportTTL      time.Duration
}

func (s *turnTrackingState) normalizeThreadID(threadID string) string {
	if s == nil { return "" }
	return strings.TrimSpace(threadID)
}

func (s *turnTrackingState) nextThreadSeq() int64 {
	if s == nil { return 0 }
	return s.threadSeq.Add(1)
}

func (s *turnTrackingState) rememberFileChanges(threadID string, files []string) {
	id := s.normalizeThreadID(threadID)
	if id == "" || len(files) == 0 { return }
	copied := append([]string(nil), files...)
	s.fileChangeMu.Lock()
	if s.fileChangeByThread == nil { s.fileChangeByThread = make(map[string][]string) }
	s.fileChangeByThread[id] = copied
	s.fileChangeMu.Unlock()
}

func (s *turnTrackingState) consumeFileChanges(threadID string) []string {
	id := s.normalizeThreadID(threadID)
	if id == "" { return nil }
	s.fileChangeMu.Lock(); files := s.fileChangeByThread[id]; delete(s.fileChangeByThread, id); s.fileChangeMu.Unlock()
	return append([]string(nil), files...)
}

func (s *turnTrackingState) ensureOrchestrationReportStateLocked() {
	if s == nil { return }
	if s.orchestrationPendingReports == nil {
		s.orchestrationPendingReports = make(map[string]map[string]time.Time)
	}
	if s.orchestrationReportTTL <= 0 {
		s.orchestrationReportTTL = defaultOrchestrationReportTTL
	}
}

func (s *turnTrackingState) pruneOrchestrationReportRequestsLocked(now time.Time) {
	if s == nil { return }
	ttl := dashboard.NormalizeDurationOrDefault(s.orchestrationReportTTL, defaultOrchestrationReportTTL)
	if ttl != s.orchestrationReportTTL {
		s.orchestrationReportTTL = ttl
	}
	dashboard.PruneOrchestrationPendingReports(s.orchestrationPendingReports, now, ttl)
}

func (s *turnTrackingState) withOrchestrationReportState(now time.Time, fn func()) {
	if s == nil || fn == nil { return }
	s.orchestrationReportMu.Lock(); defer s.orchestrationReportMu.Unlock()
	s.ensureOrchestrationReportStateLocked()
	s.pruneOrchestrationReportRequestsLocked(now)
	fn()
}

func (s *turnTrackingState) rememberReportRequester(workerID, requesterID string, now time.Time) int {
	target := strings.TrimSpace(workerID)
	requester := strings.TrimSpace(requesterID)
	if target == "" || requester == "" { return 0 }
	result := 0
	s.withOrchestrationReportState(now, func() {
		result = dashboard.RememberOrchestrationRequester(s.orchestrationPendingReports, target, requester, now)
	})
	return result
}

func (s *turnTrackingState) takeReportRequesters(workerID string, now time.Time) []string {
	target := strings.TrimSpace(workerID)
	if target == "" { return nil }
	var requesters []string
	s.withOrchestrationReportState(now, func() {
		requesters = dashboard.TakeOrchestrationRequesters(s.orchestrationPendingReports, target)
	})
	return requesters
}

type uiThrottleState struct {
	uiThrottleMu      sync.Mutex
	uiThrottleEntries map[string]*uiStateThrottleEntry
}

func (s *uiThrottleState) stageUIStateChanged(key string, payload map[string]any, now time.Time, interval time.Duration, onFlush func()) (map[string]any, bool) {
	if s == nil { return nil, false }
	k := strings.TrimSpace(key)
	if k == "" { return nil, false }
	s.uiThrottleMu.Lock()
	defer s.uiThrottleMu.Unlock()
	if s.uiThrottleEntries == nil {
		s.uiThrottleEntries = make(map[string]*uiStateThrottleEntry)
	}
	entry := s.uiThrottleEntries[k]
	if entry == nil {
		entry = &uiStateThrottleEntry{}
		s.uiThrottleEntries[k] = entry
	}
	entry.pending = payload
	if now.Sub(entry.lastEmit) < interval {
		if entry.timer == nil && onFlush != nil {
			entry.timer = time.AfterFunc(interval, onFlush)
		}
		return nil, false
	}
	entry.lastEmit = now
	pending := entry.pending
	entry.pending = nil
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	return pending, true
}

func (s *uiThrottleState) flushUIStateChanged(key string, now time.Time) (map[string]any, bool) {
	if s == nil { return nil, false }
	k := strings.TrimSpace(key)
	if k == "" { return nil, false }
	s.uiThrottleMu.Lock()
	defer s.uiThrottleMu.Unlock()
	entry := s.uiThrottleEntries[k]
	if entry == nil || entry.pending == nil {
		if entry != nil {
			entry.timer = nil
		}
		return nil, false
	}
	entry.lastEmit = now
	pending := entry.pending
	entry.pending = nil
	entry.timer = nil
	return pending, true
}

func (s *uiThrottleState) stopAllTimers() {
	if s == nil { return }
	s.uiThrottleMu.Lock()
	defer s.uiThrottleMu.Unlock()
	for _, entry := range s.uiThrottleEntries {
		if entry != nil && entry.timer != nil { entry.timer.Stop() }
	}
	clear(s.uiThrottleEntries)
}

type toolCallState struct {
	toolCallMu    sync.RWMutex
	toolCallCount map[string]int64
}

type safeSet[T comparable] struct {
	mu    sync.RWMutex
	items map[T]struct{}
}

func (s *safeSet[T]) add(item T) {
	s.mu.Lock()
	if s.items == nil { s.items = make(map[T]struct{}) }
	s.items[item] = struct{}{}
	s.mu.Unlock()
}

func (s *safeSet[T]) remove(item T) {
	s.mu.Lock(); delete(s.items, item); s.mu.Unlock()
}

func (s *safeSet[T]) count() int {
	s.mu.RLock(); defer s.mu.RUnlock(); return len(s.items)
}

func (s *safeSet[T]) snapshot() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]T, 0, len(s.items))
	for item := range s.items {
		out = append(out, item)
	}
	return out
}

type sseState struct {
	clients safeSet[chan []byte]
}

type notifyHookState struct {
	notifyHookMu sync.RWMutex
	notifyHook   func(method string, params any)
}

type runtimeGuardState struct {
	approvalInFlight sync.Map
	cleanupOnce      sync.Once
}

func (s *runtimeGuardState) tryBeginApproval(key string) bool {
	if s == nil { return false }
	k := strings.TrimSpace(key)
	if k == "" { return false }
	_, loaded := s.approvalInFlight.LoadOrStore(k, struct{}{})
	return !loaded
}

func (s *runtimeGuardState) endApproval(key string) {
	if s == nil { return }
	k := strings.TrimSpace(key)
	if k == "" { return }
	s.approvalInFlight.Delete(k)
}

func (s *runtimeGuardState) doCleanup(fn func()) {
	if fn == nil { return }
	if s == nil {
		fn()
		return
	}
	s.cleanupOnce.Do(fn)
}

type threadAliasState struct {
	threadAliasMu sync.Mutex
}

func (s *threadAliasState) withLock(fn func()) {
	if fn == nil { return }
	if s == nil {
		fn()
		return
	}
	s.threadAliasMu.Lock()
	defer s.threadAliasMu.Unlock()
	fn()
}

type storeBundle struct {
	dagStore          *store.TaskDAGStore
	cmdStore          *store.CommandCardStore
	promptStore       *store.PromptTemplateStore
	fileStore         *store.SharedFileStore
	workspaceRunStore *store.WorkspaceRunStore
	sysLogStore       *store.SystemLogStore

	agentStatusStore *store.AgentStatusStore
	auditLogStore    *store.AuditLogStore
	aiLogStore       *store.AILogStore
	busLogStore      *store.BusLogStore
	taskAckStore     *store.TaskAckStore
	taskTraceStore   *store.TaskTraceStore

	bindingStore *store.AgentCodexBindingStore

	agentThreadStore *store.AgentThreadStore
}
