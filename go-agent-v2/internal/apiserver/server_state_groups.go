package apiserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/dashboard"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
)

// connManagerState 聚合 WebSocket 连接与 Server->Client 请求跟踪状态。
type connManagerState struct {
	// 连接管理 (支持多 IDE 同时连接)
	mu     sync.RWMutex
	conns  map[string]*connEntry // connID -> entry
	nextID atomic.Int64

	// Server -> Client 请求: 服务端发起请求, 等待客户端响应。
	pendingMu sync.Mutex
	pending   map[int64]chan *Response // requestID -> response channel
	nextReqID atomic.Int64
}

func (s *connManagerState) connectionCount() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns)
}

func (s *connManagerState) allocConnID() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("conn-%d", s.nextID.Add(1))
}

func (s *connManagerState) firstConnID() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id := range s.conns {
		return id
	}
	return ""
}

func (s *connManagerState) connsSnapshot() map[string]*connEntry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := make(map[string]*connEntry, len(s.conns))
	for id, entry := range s.conns {
		snapshot[id] = entry
	}
	return snapshot
}

func (s *connManagerState) getConn(connID string) (*connEntry, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.conns[connID]
	return entry, ok
}

func (s *connManagerState) addConn(connID string, entry *connEntry) {
	if s == nil || connID == "" || entry == nil {
		return
	}
	s.mu.Lock()
	if s.conns == nil {
		s.conns = make(map[string]*connEntry)
	}
	s.conns[connID] = entry
	s.mu.Unlock()
}

func (s *connManagerState) removeConn(connID string) (*connEntry, bool) {
	if s == nil || connID == "" {
		return nil, false
	}
	s.mu.Lock()
	entry, ok := s.conns[connID]
	if ok {
		delete(s.conns, connID)
	}
	s.mu.Unlock()
	return entry, ok
}

func (s *connManagerState) allocPendingRequest() (reqID int64, ch <-chan *Response, cleanup func()) {
	if s == nil {
		return 0, nil, func() {}
	}
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
	if s == nil {
		return nil, false
	}
	s.pendingMu.Lock()
	ch, ok := s.pending[reqID]
	s.pendingMu.Unlock()
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

// diagnosticsCacheState 聚合 LSP 诊断缓存。
type diagnosticsCacheState struct {
	diagMu    sync.RWMutex
	diagCache map[string][]lsp.Diagnostic // uri -> diagnostics
}

func (s *diagnosticsCacheState) setDiagnostics(uri string, diagnostics []lsp.Diagnostic) {
	if s == nil {
		return
	}
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	if s.diagCache == nil {
		s.diagCache = map[string][]lsp.Diagnostic{}
	}
	copied := cloneDiagnostics(diagnostics)
	if len(copied) == 0 {
		delete(s.diagCache, uri)
		return
	}
	s.diagCache[uri] = copied
}

func (s *diagnosticsCacheState) getDiagnostics(uri string) []lsp.Diagnostic {
	if s == nil {
		return nil
	}
	s.diagMu.RLock()
	defer s.diagMu.RUnlock()
	return cloneDiagnostics(s.diagCache[uri])
}

func (s *diagnosticsCacheState) allDiagnostics() map[string][]lsp.Diagnostic {
	if s == nil {
		return map[string][]lsp.Diagnostic{}
	}
	s.diagMu.RLock()
	defer s.diagMu.RUnlock()
	out := make(map[string][]lsp.Diagnostic, len(s.diagCache))
	for uri, diagnostics := range s.diagCache {
		out[uri] = cloneDiagnostics(diagnostics)
	}
	return out
}

// codeRunState 聚合 code_run 执行状态与 agent 工作目录缓存。
type codeRunState struct {
	codeRunMu      sync.Mutex
	activeCodeRuns map[string]map[string]context.CancelFunc // agentID -> runKey -> cancel
	codeRunSeq     atomic.Int64

	agentWorkDirMu sync.RWMutex
	agentWorkDirs  map[string]string // agentID -> abs cwd
}

func (s *codeRunState) normalizeAgentID(agentID string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(agentID)
}

func (s *codeRunState) setAgentWorkDir(agentID, cwd string) {
	id := s.normalizeAgentID(agentID)
	if id == "" {
		return
	}
	normalized := tools.NormalizeAgentWorkDir(cwd)
	if normalized == "" {
		return
	}
	s.agentWorkDirMu.Lock()
	if s.agentWorkDirs == nil {
		s.agentWorkDirs = make(map[string]string)
	}
	s.agentWorkDirs[id] = normalized
	s.agentWorkDirMu.Unlock()
}

func (s *codeRunState) getAgentWorkDir(agentID string) string {
	id := s.normalizeAgentID(agentID)
	if id == "" {
		return ""
	}
	s.agentWorkDirMu.RLock()
	cwd := s.agentWorkDirs[id]
	s.agentWorkDirMu.RUnlock()
	return cwd
}

func (s *codeRunState) clearAgentWorkDir(agentID string) {
	id := s.normalizeAgentID(agentID)
	if id == "" {
		return
	}
	s.agentWorkDirMu.Lock()
	delete(s.agentWorkDirs, id)
	s.agentWorkDirMu.Unlock()
}

func (s *codeRunState) clearAllAgentWorkDirs() {
	if s == nil {
		return
	}
	s.agentWorkDirMu.Lock()
	clear(s.agentWorkDirs)
	s.agentWorkDirMu.Unlock()
}

func (s *codeRunState) registerCodeRunCancel(agentID, callID string, cancel context.CancelFunc) string {
	if cancel == nil {
		return ""
	}
	id := s.normalizeAgentID(agentID)
	if id == "" {
		return ""
	}
	key := fmt.Sprintf("%s#%d", strings.TrimSpace(callID), s.codeRunSeq.Add(1))
	s.codeRunMu.Lock()
	if s.activeCodeRuns == nil {
		s.activeCodeRuns = make(map[string]map[string]context.CancelFunc)
	}
	runs := s.activeCodeRuns[id]
	if runs == nil {
		runs = make(map[string]context.CancelFunc)
		s.activeCodeRuns[id] = runs
	}
	runs[key] = cancel
	s.codeRunMu.Unlock()
	return key
}

func (s *codeRunState) unregisterCodeRunCancel(agentID, runKey string) {
	id, key := s.normalizeAgentID(agentID), strings.TrimSpace(runKey)
	if id == "" || key == "" {
		return
	}
	s.codeRunMu.Lock()
	if runs := s.activeCodeRuns[id]; runs != nil {
		delete(runs, key)
		if len(runs) == 0 {
			delete(s.activeCodeRuns, id)
		}
	}
	s.codeRunMu.Unlock()
}

func (s *codeRunState) cancelCodeRuns(agentID string) int {
	id := s.normalizeAgentID(agentID)
	if id == "" {
		return 0
	}
	s.codeRunMu.Lock()
	runs := s.activeCodeRuns[id]
	delete(s.activeCodeRuns, id)
	s.codeRunMu.Unlock()
	if len(runs) == 0 {
		return 0
	}
	for _, cancel := range runs {
		cancel()
	}
	return len(runs)
}

func (s *codeRunState) cancelAllCodeRuns() int {
	if s == nil {
		return 0
	}
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

// turnTrackingState 聚合 turn 序列与回报/文件变更追踪。
type turnTrackingState struct {
	threadSeq atomic.Int64 // thread/start 唯一序号

	fileChangeMu       sync.Mutex
	fileChangeByThread map[string][]string // threadID -> changed files

	orchestrationReportMu       sync.Mutex
	orchestrationPendingReports map[string]map[string]time.Time // workerID -> requesterID -> createdAt
	orchestrationReportTTL      time.Duration
}

func (s *turnTrackingState) normalizeThreadID(threadID string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(threadID)
}

func (s *turnTrackingState) nextThreadSeq() int64 {
	if s == nil {
		return 0
	}
	return s.threadSeq.Add(1)
}

func (s *turnTrackingState) rememberFileChanges(threadID string, files []string) {
	id := s.normalizeThreadID(threadID)
	if id == "" || len(files) == 0 {
		return
	}
	copied := append([]string(nil), files...)
	s.fileChangeMu.Lock()
	if s.fileChangeByThread == nil {
		s.fileChangeByThread = make(map[string][]string)
	}
	s.fileChangeByThread[id] = copied
	s.fileChangeMu.Unlock()
}

func (s *turnTrackingState) consumeFileChanges(threadID string) []string {
	id := s.normalizeThreadID(threadID)
	if id == "" {
		return nil
	}
	s.fileChangeMu.Lock()
	files := s.fileChangeByThread[id]
	delete(s.fileChangeByThread, id)
	s.fileChangeMu.Unlock()
	return append([]string(nil), files...)
}

func (s *turnTrackingState) ensureOrchestrationReportStateLocked() {
	if s == nil {
		return
	}
	if s.orchestrationPendingReports == nil {
		s.orchestrationPendingReports = make(map[string]map[string]time.Time)
	}
	if s.orchestrationReportTTL <= 0 {
		s.orchestrationReportTTL = defaultOrchestrationReportTTL
	}
}

func (s *turnTrackingState) pruneOrchestrationReportRequestsLocked(now time.Time) {
	if s == nil {
		return
	}
	ttl := dashboard.NormalizeDurationOrDefault(s.orchestrationReportTTL, defaultOrchestrationReportTTL)
	if ttl != s.orchestrationReportTTL {
		s.orchestrationReportTTL = ttl
	}
	dashboard.PruneOrchestrationPendingReports(s.orchestrationPendingReports, now, ttl)
}

func (s *turnTrackingState) withOrchestrationReportState(now time.Time, fn func()) {
	if s == nil || fn == nil {
		return
	}
	s.orchestrationReportMu.Lock()
	defer s.orchestrationReportMu.Unlock()
	s.ensureOrchestrationReportStateLocked()
	s.pruneOrchestrationReportRequestsLocked(now)
	fn()
}

func (s *turnTrackingState) rememberReportRequester(workerID, requesterID string, now time.Time) int {
	target := strings.TrimSpace(workerID)
	requester := strings.TrimSpace(requesterID)
	if target == "" || requester == "" {
		return 0
	}
	result := 0
	s.withOrchestrationReportState(now, func() {
		result = dashboard.RememberOrchestrationRequester(s.orchestrationPendingReports, target, requester, now)
	})
	return result
}

func (s *turnTrackingState) takeReportRequesters(workerID string, now time.Time) []string {
	target := strings.TrimSpace(workerID)
	if target == "" {
		return nil
	}
	var requesters []string
	s.withOrchestrationReportState(now, func() {
		requesters = dashboard.TakeOrchestrationRequesters(s.orchestrationPendingReports, target)
	})
	return requesters
}

// uiThrottleState 聚合 ui/state/changed 节流状态。
type uiThrottleState struct {
	uiThrottleMu      sync.Mutex
	uiThrottleEntries map[string]*uiStateThrottleEntry // key: threadID or agentID
}

func (s *uiThrottleState) stageUIStateChanged(key string, payload map[string]any, now time.Time, interval time.Duration, onFlush func()) (map[string]any, bool) {
	if s == nil {
		return nil, false
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return nil, false
	}
	s.uiThrottleMu.Lock()
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
		s.uiThrottleMu.Unlock()
		return nil, false
	}
	entry.lastEmit = now
	pending := entry.pending
	entry.pending = nil
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	s.uiThrottleMu.Unlock()
	return pending, true
}

func (s *uiThrottleState) flushUIStateChanged(key string, now time.Time) (map[string]any, bool) {
	if s == nil {
		return nil, false
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return nil, false
	}
	s.uiThrottleMu.Lock()
	entry := s.uiThrottleEntries[k]
	if entry == nil || entry.pending == nil {
		if entry != nil {
			entry.timer = nil
		}
		s.uiThrottleMu.Unlock()
		return nil, false
	}
	entry.lastEmit = now
	pending := entry.pending
	entry.pending = nil
	entry.timer = nil
	s.uiThrottleMu.Unlock()
	return pending, true
}

// stopAllTimers 停止所有 uiThrottle 定时器并清空 map, 防止 server 关闭后 timer 泄漏。
func (s *uiThrottleState) stopAllTimers() {
	if s == nil {
		return
	}
	s.uiThrottleMu.Lock()
	defer s.uiThrottleMu.Unlock()
	for _, entry := range s.uiThrottleEntries {
		if entry != nil && entry.timer != nil {
			entry.timer.Stop()
		}
	}
	clear(s.uiThrottleEntries)
}

// toolCallState 聚合动态工具调用计数(可观测性)。
type toolCallState struct {
	toolCallMu    sync.RWMutex
	toolCallCount map[string]int64 // toolName -> count
}

func (s *toolCallState) increment(name string) int64 {
	if s == nil {
		return 0
	}
	toolName := strings.TrimSpace(name)
	if toolName == "" {
		return 0
	}
	s.toolCallMu.Lock()
	defer s.toolCallMu.Unlock()
	if s.toolCallCount == nil {
		s.toolCallCount = make(map[string]int64)
	}
	s.toolCallCount[toolName]++
	return s.toolCallCount[toolName]
}

func (s *toolCallState) get(name string) int64 {
	if s == nil {
		return 0
	}
	toolName := strings.TrimSpace(name)
	if toolName == "" {
		return 0
	}
	s.toolCallMu.RLock()
	defer s.toolCallMu.RUnlock()
	return s.toolCallCount[toolName]
}

// clearAll 清空工具调用计数, 防止无限增长。
func (s *toolCallState) clearAll() {
	if s == nil {
		return
	}
	s.toolCallMu.Lock()
	clear(s.toolCallCount)
	s.toolCallMu.Unlock()
}

// sseState 聚合 SSE 推送客户端集合。
type sseState struct {
	sseMu      sync.RWMutex
	sseClients map[chan []byte]struct{}
}

func (s *sseState) addClient(ch chan []byte) {
	if s == nil || ch == nil {
		return
	}
	s.sseMu.Lock()
	if s.sseClients == nil {
		s.sseClients = make(map[chan []byte]struct{})
	}
	s.sseClients[ch] = struct{}{}
	s.sseMu.Unlock()
}

func (s *sseState) removeClient(ch chan []byte) {
	if s == nil || ch == nil {
		return
	}
	s.sseMu.Lock()
	delete(s.sseClients, ch)
	s.sseMu.Unlock()
}

func (s *sseState) clientCount() int {
	if s == nil {
		return 0
	}
	s.sseMu.RLock()
	defer s.sseMu.RUnlock()
	return len(s.sseClients)
}

func (s *sseState) snapshotClients() []chan []byte {
	if s == nil {
		return nil
	}
	s.sseMu.RLock()
	defer s.sseMu.RUnlock()
	out := make([]chan []byte, 0, len(s.sseClients))
	for ch := range s.sseClients {
		out = append(out, ch)
	}
	return out
}

// notifyHookState 聚合桌面端通知钩子状态。
type notifyHookState struct {
	notifyHookMu sync.RWMutex
	notifyHook   func(method string, params any)
}

func (s *notifyHookState) setHook(h func(method string, params any)) {
	if s == nil {
		return
	}
	s.notifyHookMu.Lock()
	s.notifyHook = h
	s.notifyHookMu.Unlock()
}

func (s *notifyHookState) hook() func(method string, params any) {
	if s == nil {
		return nil
	}
	s.notifyHookMu.RLock()
	h := s.notifyHook
	s.notifyHookMu.RUnlock()
	return h
}

func (s *notifyHookState) hasHook() bool { return s.hook() != nil }

// runtimeGuardState 聚合运行时清理与审批去重状态。
type runtimeGuardState struct {
	approvalInFlight sync.Map // key: "agentID:method"
	cleanupOnce      sync.Once
}

func (s *runtimeGuardState) tryBeginApproval(key string) bool {
	if s == nil {
		return false
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return false
	}
	_, loaded := s.approvalInFlight.LoadOrStore(k, struct{}{})
	return !loaded
}

func (s *runtimeGuardState) endApproval(key string) {
	if s == nil {
		return
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return
	}
	s.approvalInFlight.Delete(k)
}

func (s *runtimeGuardState) doCleanup(fn func()) {
	if s == nil {
		if fn != nil {
			fn()
		}
		return
	}
	s.cleanupOnce.Do(func() {
		if fn != nil {
			fn()
		}
	})
}

// threadAliasState 聚合 thread alias 写入串行化锁。
type threadAliasState struct {
	threadAliasMu sync.Mutex
}

func (s *threadAliasState) withLock(fn func()) {
	if s == nil {
		if fn != nil {
			fn()
		}
		return
	}
	s.threadAliasMu.Lock()
	defer s.threadAliasMu.Unlock()
	if fn != nil {
		fn()
	}
}

// storeBundle 聚合 apiserver 资源/仪表盘相关存储依赖。
type storeBundle struct {
	// 资源 Store (编排工具依赖)
	dagStore          *store.TaskDAGStore
	cmdStore          *store.CommandCardStore
	promptStore       *store.PromptTemplateStore
	fileStore         *store.SharedFileStore
	workspaceRunStore *store.WorkspaceRunStore
	sysLogStore       *store.SystemLogStore

	// Dashboard Store (JSON-RPC dashboard/* 方法)
	agentStatusStore *store.AgentStatusStore
	auditLogStore    *store.AuditLogStore
	aiLogStore       *store.AILogStore
	busLogStore      *store.BusLogStore
	taskAckStore     *store.TaskAckStore
	taskTraceStore   *store.TaskTraceStore

	// Agent <-> Codex Thread 1:1 共生绑定 (根基约束, 不允许绕过)。
	bindingStore *store.AgentCodexBindingStore
}
