package apiserver

import (
	"context"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
)

type codexAdapterHooks struct {
	server *Server
}

func (h codexAdapterHooks) readSkillContent(skillName string) (string, error) {
	if h.server == nil || h.server.skillSvc == nil {
		return "", pkgerr.New("Server.skillService", "skill service is not initialized")
	}
	return h.server.skillSvc.ReadSkillContent(skillName)
}

func (h codexAdapterHooks) listSkillMatchCandidates() ([]contracts.SkillMatchCandidate, error) {
	return listSkillMatchCandidates(h.server)
}

func (h codexAdapterHooks) getAgentSkills(agentID string) []string {
	return getAgentSkills(h.server, agentID)
}

func (h codexAdapterHooks) allSchemas() []codexsdk.DynamicTool {
	return h.server.AllSchemas()
}

func (h codexAdapterHooks) notify(method string, params any) {
	notify(h.server, method, params)
}

func (h codexAdapterHooks) setAgentWorkDir(agentID, cwd string) {
	setAgentWorkDirState(h.server, agentID, cwd)
}

func (h codexAdapterHooks) cancelCodeRuns(agentID string) int {
	return cancelCodeRunsState(h.server, agentID)
}

func newCodexAdapter(s *Server) *codexadapter.Adapter {
	if s == nil {
		return nil
	}
	hooks := codexAdapterHooks{server: s}
	return codexadapter.New(codexadapter.Deps{
		Manager:                  s.mgr,
		Store:                    s.prefManager,
		BindingStore:             s.bindingStore,
		AgentStatusStore:         s.agentStatusStore,
		UIRuntime:                s.uiRuntime,
		AllSchemas:               hooks.allSchemas,
		SetAgentWorkDir:          hooks.setAgentWorkDir,
		CancelCodeRuns:           hooks.cancelCodeRuns,
		ReadSkillContent:         hooks.readSkillContent,
		ListSkillMatchCandidates: hooks.listSkillMatchCandidates,
		GetAgentSkills:           hooks.getAgentSkills,
		Notify:                   hooks.notify,
	})
}

func registerCodeRunCancelState(s *Server, agentID, callID string, cancel context.CancelFunc) string {
	if s == nil { return "" }
	return s.codeRunState.registerCodeRunCancel(agentID, callID, cancel)
}

func unregisterCodeRunCancelState(s *Server, agentID, runKey string) {
	if s == nil { return }
	s.codeRunState.unregisterCodeRunCancel(agentID, runKey)
}

func cancelCodeRunsState(s *Server, agentID string) int {
	if s == nil { return 0 }
	return s.codeRunState.cancelCodeRuns(agentID)
}

func setAgentWorkDirState(s *Server, agentID, cwd string) {
	if s == nil { return }
	s.codeRunState.setAgentWorkDir(agentID, cwd)
}

func clearAgentWorkDirState(s *Server, agentID string) {
	if s == nil { return }
	s.codeRunState.clearAgentWorkDir(agentID)
}

func getAgentWorkDirState(s *Server, agentID string) string {
	if s == nil { return "" }
	return s.codeRunState.getAgentWorkDir(agentID)
}

func cancelAllCodeRuns(s *Server) int {
	if s == nil { return 0 }
	return s.codeRunState.cancelAllCodeRuns()
}

func notifyHookFuncState(s *Server) func(method string, params any) {
	if s == nil { return nil }
	state := &s.notifyHookState
	state.notifyHookMu.RLock()
	h := state.notifyHook
	state.notifyHookMu.RUnlock()
	return h
}

func snapshotSSEClientsState(s *Server) []chan []byte {
	if s == nil { return nil }
	return s.sseState.clients.snapshot()
}

func connsSnapshotState(s *Server) map[string]*connEntry {
	if s == nil { return nil }
	return s.connManagerState.connsSnapshot()
}

func removeConnState(s *Server, connID string) (*connEntry, bool) {
	if s == nil { return nil, false }
	return s.connManagerState.removeConn(connID)
}

func allocPendingRequestState(s *Server) (reqID int64, ch <-chan *Response, cleanup func()) {
	if s == nil { return 0, nil, func() {} }
	return s.connManagerState.allocPendingRequest()
}

func getConnState(s *Server, connID string) (*connEntry, bool) {
	if s == nil { return nil, false }
	return s.connManagerState.getConn(connID)
}

func firstConnIDState(s *Server) string {
	if s == nil { return "" }
	return s.connManagerState.firstConnID()
}

func deliverPendingResponseState(s *Server, reqID int64, resp *Response) (bool, bool) {
	if s == nil { return false, false }
	return s.connManagerState.deliverPendingResponse(reqID, resp)
}

func connectionCountState(s *Server) int {
	if s == nil { return 0 }
	return s.connManagerState.connectionCount()
}

func allocConnIDState(s *Server) string {
	if s == nil { return "" }
	return s.connManagerState.allocConnID()
}

func addConnState(s *Server, connID string, entry *connEntry) {
	if s == nil { return }
	s.connManagerState.addConn(connID, entry)
}

func setDiagnosticsCacheState(s *Server, uri string, diagnostics []lsp.Diagnostic) {
	if s == nil { return }
	state := &s.diagnosticsCacheState
	state.diagMu.Lock(); defer state.diagMu.Unlock()
	if state.diagCache == nil { state.diagCache = map[string][]lsp.Diagnostic{} }
	copied := cloneDiagnostics(diagnostics)
	if len(copied) == 0 {
		delete(state.diagCache, uri)
		return
	}
	state.diagCache[uri] = copied
}

func getDiagnosticsCacheState(s *Server, uri string) []lsp.Diagnostic {
	if s == nil { return nil }
	state := &s.diagnosticsCacheState
	state.diagMu.RLock(); diagnostics := cloneDiagnostics(state.diagCache[uri]); state.diagMu.RUnlock()
	return diagnostics
}

func allDiagnosticsCacheState(s *Server) map[string][]lsp.Diagnostic {
	if s == nil {
		return map[string][]lsp.Diagnostic{}
	}
	state := &s.diagnosticsCacheState
	state.diagMu.RLock()
	out := make(map[string][]lsp.Diagnostic, len(state.diagCache))
	for uri, diagnostics := range state.diagCache {
		out[uri] = cloneDiagnostics(diagnostics)
	}
	state.diagMu.RUnlock()
	return out
}

func rememberReportRequesterState(s *Server, workerID, requesterID string, now time.Time) int {
	if s == nil { return 0 }
	return s.turnTrackingState.rememberReportRequester(workerID, requesterID, now)
}

func takeReportRequestersState(s *Server, workerID string, now time.Time) []string {
	if s == nil { return nil }
	return s.turnTrackingState.takeReportRequesters(workerID, now)
}

func setNotifyHookState(s *Server, h func(method string, params any)) {
	if s == nil { return }
	state := &s.notifyHookState
	state.notifyHookMu.Lock(); state.notifyHook = h; state.notifyHookMu.Unlock()
}

func stageUIStateChangedState(s *Server, key string, payload map[string]any, now time.Time, interval time.Duration, onFlush func()) (map[string]any, bool) {
	if s == nil { return nil, false }
	return s.uiThrottleState.stageUIStateChanged(key, payload, now, interval, onFlush)
}

func flushUIStateChangedState(s *Server, key string, now time.Time) (map[string]any, bool) {
	if s == nil { return nil, false }
	return s.uiThrottleState.flushUIStateChanged(key, now)
}

func rememberFileChangesState(s *Server, threadID string, files []string) {
	if s == nil { return }
	s.turnTrackingState.rememberFileChanges(threadID, files)
}

func consumeFileChangesState(s *Server, threadID string) []string {
	if s == nil { return nil }
	return s.turnTrackingState.consumeFileChanges(threadID)
}

func addSSEClientState(s *Server, ch chan []byte) {
	if s == nil || ch == nil { return }
	s.sseState.clients.add(ch)
}

func removeSSEClientState(s *Server, ch chan []byte) {
	if s == nil || ch == nil { return }
	s.sseState.clients.remove(ch)
}

func tryBeginApprovalState(s *Server, key string) bool {
	if s == nil { return false }
	return s.runtimeGuardState.tryBeginApproval(key)
}

func endApprovalState(s *Server, key string) {
	if s == nil { return }
	s.runtimeGuardState.endApproval(key)
}

func hasNotifyHookState(s *Server) bool {
	return notifyHookFuncState(s) != nil
}

func stopAllUIThrottleTimersState(s *Server) {
	if s == nil { return }
	s.uiThrottleState.stopAllTimers()
}

func clearAllToolCallState(s *Server) {
	if s == nil { return }
	state := &s.toolCallState
	state.toolCallMu.Lock(); clear(state.toolCallCount); state.toolCallMu.Unlock()
}

func incrementToolCallState(s *Server, name string) int64 {
	if s == nil { return 0 }
	toolName := strings.TrimSpace(name)
	if toolName == "" { return 0 }
	state := &s.toolCallState
	state.toolCallMu.Lock()
	if state.toolCallCount == nil { state.toolCallCount = make(map[string]int64) }
	state.toolCallCount[toolName]++
	count := state.toolCallCount[toolName]
	state.toolCallMu.Unlock()
	return count
}

func nextThreadSeqState(s *Server) int64 {
	if s == nil { return 0 }
	return s.turnTrackingState.nextThreadSeq()
}

func doRuntimeCleanupState(s *Server, fn func()) {
	if s == nil { if fn != nil { fn() }; return }
	s.runtimeGuardState.doCleanup(fn)
}

func clearAllAgentWorkDirsState(s *Server) {
	if s == nil { return }
	s.codeRunState.clearAllAgentWorkDirs()
}
