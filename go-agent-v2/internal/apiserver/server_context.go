package apiserver

import (
	"context"
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
	return allSchemas(h.server)
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

func withServer(s *Server, fn func(*Server)) {
	if s == nil || fn == nil {
		return
	}
	fn(s)
}

func serverValue[T any](s *Server, fallback T, fn func(*Server) T) T {
	if s == nil || fn == nil {
		return fallback
	}
	return fn(s)
}

func serverValue2[T1, T2 any](s *Server, fallback1 T1, fallback2 T2, fn func(*Server) (T1, T2)) (T1, T2) {
	if s == nil || fn == nil {
		return fallback1, fallback2
	}
	return fn(s)
}

func registerCodeRunCancelState(s *Server, agentID, callID string, cancel context.CancelFunc) string {
	return serverValue(s, "", func(s *Server) string {
		return s.codeRunState.registerCodeRunCancel(agentID, callID, cancel)
	})
}

func unregisterCodeRunCancelState(s *Server, agentID, runKey string) {
	withServer(s, func(s *Server) {
		s.codeRunState.unregisterCodeRunCancel(agentID, runKey)
	})
}

func cancelCodeRunsState(s *Server, agentID string) int {
	return serverValue(s, 0, func(s *Server) int {
		return s.codeRunState.cancelCodeRuns(agentID)
	})
}

func setAgentWorkDirState(s *Server, agentID, cwd string) {
	withServer(s, func(s *Server) {
		s.codeRunState.setAgentWorkDir(agentID, cwd)
	})
}

func clearAgentWorkDirState(s *Server, agentID string) {
	withServer(s, func(s *Server) {
		s.codeRunState.clearAgentWorkDir(agentID)
	})
}

func getAgentWorkDirState(s *Server, agentID string) string {
	return serverValue(s, "", func(s *Server) string {
		return s.codeRunState.getAgentWorkDir(agentID)
	})
}

func cancelAllCodeRuns(s *Server) int {
	return serverValue(s, 0, func(s *Server) int {
		return s.codeRunState.cancelAllCodeRuns()
	})
}

func notifyHookFuncState(s *Server) func(method string, params any) {
	return serverValue(s, nil, func(s *Server) func(method string, params any) {
		return s.notifyHookState.hook()
	})
}

func snapshotSSEClientsState(s *Server) []chan []byte {
	return serverValue(s, nil, func(s *Server) []chan []byte {
		return s.sseState.clients.snapshot()
	})
}

func connsSnapshotState(s *Server) map[string]*connEntry {
	return serverValue(s, nil, func(s *Server) map[string]*connEntry {
		return s.connManagerState.connsSnapshot()
	})
}

func removeConnState(s *Server, connID string) (*connEntry, bool) {
	return serverValue2(s, (*connEntry)(nil), false, func(s *Server) (*connEntry, bool) {
		return s.connManagerState.removeConn(connID)
	})
}

func allocPendingRequestState(s *Server) (reqID int64, ch <-chan *Response, cleanup func()) {
	if s == nil {
		return 0, nil, func() {}
	}
	return s.connManagerState.allocPendingRequest()
}

func getConnState(s *Server, connID string) (*connEntry, bool) {
	return serverValue2(s, (*connEntry)(nil), false, func(s *Server) (*connEntry, bool) {
		return s.connManagerState.getConn(connID)
	})
}

func firstConnIDState(s *Server) string {
	return serverValue(s, "", func(s *Server) string {
		return s.connManagerState.firstConnID()
	})
}

func deliverPendingResponseState(s *Server, reqID int64, resp *Response) (bool, bool) {
	return serverValue2(s, false, false, func(s *Server) (bool, bool) {
		return s.connManagerState.deliverPendingResponse(reqID, resp)
	})
}

func connectionCountState(s *Server) int {
	return serverValue(s, 0, func(s *Server) int {
		return s.connManagerState.connectionCount()
	})
}

func allocConnIDState(s *Server) string {
	return serverValue(s, "", func(s *Server) string {
		return s.connManagerState.allocConnID()
	})
}

func addConnState(s *Server, connID string, entry *connEntry) {
	withServer(s, func(s *Server) {
		s.connManagerState.addConn(connID, entry)
	})
}

func setDiagnosticsCacheState(s *Server, uri string, diagnostics []lsp.Diagnostic) {
	withServer(s, func(s *Server) {
		s.diagnosticsCacheState.setDiagnostics(uri, diagnostics)
	})
}

func getDiagnosticsCacheState(s *Server, uri string) []lsp.Diagnostic {
	return serverValue(s, nil, func(s *Server) []lsp.Diagnostic {
		return s.diagnosticsCacheState.getDiagnostics(uri)
	})
}

func allDiagnosticsCacheState(s *Server) map[string][]lsp.Diagnostic {
	return serverValue(s, map[string][]lsp.Diagnostic{}, func(s *Server) map[string][]lsp.Diagnostic {
		return s.diagnosticsCacheState.allDiagnostics()
	})
}

func rememberReportRequesterState(s *Server, workerID, requesterID string, now time.Time) int {
	return serverValue(s, 0, func(s *Server) int {
		return s.turnTrackingState.rememberReportRequester(workerID, requesterID, now)
	})
}

func takeReportRequestersState(s *Server, workerID string, now time.Time) []string {
	return serverValue(s, nil, func(s *Server) []string {
		return s.turnTrackingState.takeReportRequesters(workerID, now)
	})
}

func setNotifyHookState(s *Server, h func(method string, params any)) {
	withServer(s, func(s *Server) {
		s.notifyHookState.setHook(h)
	})
}

func stageUIStateChangedState(s *Server, key string, payload map[string]any, now time.Time, interval time.Duration, onFlush func()) (map[string]any, bool) {
	return serverValue2(s, map[string]any(nil), false, func(s *Server) (map[string]any, bool) {
		return s.uiThrottleState.stageUIStateChanged(key, payload, now, interval, onFlush)
	})
}

func flushUIStateChangedState(s *Server, key string, now time.Time) (map[string]any, bool) {
	return serverValue2(s, map[string]any(nil), false, func(s *Server) (map[string]any, bool) {
		return s.uiThrottleState.flushUIStateChanged(key, now)
	})
}

func rememberFileChangesState(s *Server, threadID string, files []string) {
	withServer(s, func(s *Server) {
		s.turnTrackingState.rememberFileChanges(threadID, files)
	})
}

func consumeFileChangesState(s *Server, threadID string) []string {
	return serverValue(s, nil, func(s *Server) []string {
		return s.turnTrackingState.consumeFileChanges(threadID)
	})
}

func addSSEClientState(s *Server, ch chan []byte) {
	if ch == nil {
		return
	}
	withServer(s, func(s *Server) {
		s.sseState.clients.add(ch)
	})
}

func removeSSEClientState(s *Server, ch chan []byte) {
	if ch == nil {
		return
	}
	withServer(s, func(s *Server) {
		s.sseState.clients.remove(ch)
	})
}

func withThreadAliasLock(s *Server, fn func()) {
	if s == nil {
		if fn != nil {
			fn()
		}
		return
	}
	s.threadAliasState.withLock(fn)
}

func tryBeginApprovalState(s *Server, key string) bool {
	return serverValue(s, false, func(s *Server) bool {
		return s.runtimeGuardState.tryBeginApproval(key)
	})
}

func endApprovalState(s *Server, key string) {
	withServer(s, func(s *Server) {
		s.runtimeGuardState.endApproval(key)
	})
}

func hasNotifyHookState(s *Server) bool {
	return serverValue(s, false, func(s *Server) bool {
		return s.notifyHookState.hasHook()
	})
}

func stopAllUIThrottleTimersState(s *Server) {
	withServer(s, func(s *Server) {
		s.uiThrottleState.stopAllTimers()
	})
}

func clearAllToolCallState(s *Server) {
	withServer(s, func(s *Server) {
		s.toolCallState.clearAll()
	})
}

func incrementToolCallState(s *Server, name string) int64 {
	return serverValue(s, int64(0), func(s *Server) int64 {
		return s.toolCallState.increment(name)
	})
}

func nextThreadSeqState(s *Server) int64 {
	return serverValue(s, int64(0), func(s *Server) int64 {
		return s.turnTrackingState.nextThreadSeq()
	})
}

func doRuntimeCleanupState(s *Server, fn func()) {
	if s == nil {
		if fn != nil {
			fn()
		}
		return
	}
	s.runtimeGuardState.doCleanup(fn)
}

func clearAllAgentWorkDirsState(s *Server) {
	withServer(s, func(s *Server) {
		s.codeRunState.clearAllAgentWorkDirs()
	})
}
