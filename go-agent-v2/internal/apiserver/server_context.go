package apiserver

import (
	"context"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
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

func (h codexAdapterHooks) listSkillNames() ([]string, error) {
	if h.server == nil || h.server.skillSvc == nil {
		return []string{}, nil
	}
	list, err := h.server.skillSvc.ListSkills()
	if err != nil {
		return nil, err
	}
	skillNames := make([]string, 0, len(list))
	for _, item := range list {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		skillNames = append(skillNames, name)
	}
	return skillNames, nil
}

func (h codexAdapterHooks) listSkillMatchCandidates() ([]contracts.SkillMatchCandidate, error) {
	return listSkillMatchCandidates(h.server)
}

func (h codexAdapterHooks) getAgentSkills(agentID string) []string {
	return getAgentSkills(h.server, agentID)
}

func (h codexAdapterHooks) allSchemas() []agentcore.DynamicTool {
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
		ListSkillNames:           hooks.listSkillNames,
		ListSkillMatchCandidates: hooks.listSkillMatchCandidates,
		GetAgentSkills:           hooks.getAgentSkills,
		Notify:                   hooks.notify,
	})
}

func registerCodeRunCancelState(s *Server, agentID, callID string, cancel context.CancelFunc) string {
	if s == nil {
		return ""
	}
	return s.codeRunState.registerCodeRunCancel(agentID, callID, cancel)
}

func unregisterCodeRunCancelState(s *Server, agentID, runKey string) {
	if s == nil {
		return
	}
	s.codeRunState.unregisterCodeRunCancel(agentID, runKey)
}

func cancelCodeRunsState(s *Server, agentID string) int {
	if s == nil {
		return 0
	}
	return s.codeRunState.cancelCodeRuns(agentID)
}

func setAgentWorkDirState(s *Server, agentID, cwd string) {
	if s == nil {
		return
	}
	s.codeRunState.setAgentWorkDir(agentID, cwd)
}

func clearAgentWorkDirState(s *Server, agentID string) {
	if s == nil {
		return
	}
	s.codeRunState.clearAgentWorkDir(agentID)
}

func getAgentWorkDirState(s *Server, agentID string) string {
	if s == nil {
		return ""
	}
	return s.codeRunState.getAgentWorkDir(agentID)
}

func cancelAllCodeRuns(s *Server) int {
	if s == nil {
		return 0
	}
	return s.codeRunState.cancelAllCodeRuns()
}
func notifyHookFuncState(s *Server) func(method string, params any) {
	if s == nil {
		return nil
	}
	return s.notifyHookState.hook()
}

func snapshotSSEClientsState(s *Server) []chan []byte {
	if s == nil {
		return nil
	}
	return s.sseState.snapshotClients()
}

func connsSnapshotState(s *Server) map[string]*connEntry {
	if s == nil {
		return nil
	}
	return s.connManagerState.connsSnapshot()
}

func removeConnState(s *Server, connID string) (*connEntry, bool) {
	if s == nil {
		return nil, false
	}
	return s.connManagerState.removeConn(connID)
}

func allocPendingRequestState(s *Server) (reqID int64, ch <-chan *Response, cleanup func()) {
	if s == nil {
		return 0, nil, func() {}
	}
	return s.connManagerState.allocPendingRequest()
}

func getConnState(s *Server, connID string) (*connEntry, bool) {
	if s == nil {
		return nil, false
	}
	return s.connManagerState.getConn(connID)
}

func firstConnIDState(s *Server) string {
	if s == nil {
		return ""
	}
	return s.connManagerState.firstConnID()
}

func deliverPendingResponseState(s *Server, reqID int64, resp *Response) (bool, bool) {
	if s == nil {
		return false, false
	}
	return s.connManagerState.deliverPendingResponse(reqID, resp)
}

func connectionCountState(s *Server) int {
	if s == nil {
		return 0
	}
	return s.connManagerState.connectionCount()
}

func allocConnIDState(s *Server) string {
	if s == nil {
		return ""
	}
	return s.connManagerState.allocConnID()
}

func addConnState(s *Server, connID string, entry *connEntry) {
	if s == nil {
		return
	}
	s.connManagerState.addConn(connID, entry)
}
func setDiagnosticsCacheState(s *Server, uri string, diagnostics []lsp.Diagnostic) {
	if s == nil {
		return
	}
	s.diagnosticsCacheState.setDiagnostics(uri, diagnostics)
}

func getDiagnosticsCacheState(s *Server, uri string) []lsp.Diagnostic {
	if s == nil {
		return nil
	}
	return s.diagnosticsCacheState.getDiagnostics(uri)
}

func allDiagnosticsCacheState(s *Server) map[string][]lsp.Diagnostic {
	if s == nil {
		return map[string][]lsp.Diagnostic{}
	}
	return s.diagnosticsCacheState.allDiagnostics()
}
func rememberReportRequesterState(s *Server, workerID, requesterID string, now time.Time) int {
	if s == nil {
		return 0
	}
	return s.turnTrackingState.rememberReportRequester(workerID, requesterID, now)
}

func takeReportRequestersState(s *Server, workerID string, now time.Time) []string {
	if s == nil {
		return nil
	}
	return s.turnTrackingState.takeReportRequesters(workerID, now)
}

func setNotifyHookState(s *Server, h func(method string, params any)) {
	if s == nil {
		return
	}
	s.notifyHookState.setHook(h)
}

func stageUIStateChangedState(s *Server, key string, payload map[string]any, now time.Time, interval time.Duration, onFlush func()) (map[string]any, bool) {
	if s == nil {
		return nil, false
	}
	return s.uiThrottleState.stageUIStateChanged(key, payload, now, interval, onFlush)
}

func flushUIStateChangedState(s *Server, key string, now time.Time) (map[string]any, bool) {
	if s == nil {
		return nil, false
	}
	return s.uiThrottleState.flushUIStateChanged(key, now)
}

func rememberFileChangesState(s *Server, threadID string, files []string) {
	if s == nil {
		return
	}
	s.turnTrackingState.rememberFileChanges(threadID, files)
}

func consumeFileChangesState(s *Server, threadID string) []string {
	if s == nil {
		return nil
	}
	return s.turnTrackingState.consumeFileChanges(threadID)
}

func addSSEClientState(s *Server, ch chan []byte) {
	if s == nil {
		return
	}
	s.sseState.addClient(ch)
}

func removeSSEClientState(s *Server, ch chan []byte) {
	if s == nil {
		return
	}
	s.sseState.removeClient(ch)
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
	if s == nil {
		return false
	}
	return s.runtimeGuardState.tryBeginApproval(key)
}

func endApprovalState(s *Server, key string) {
	if s == nil {
		return
	}
	s.runtimeGuardState.endApproval(key)
}

func hasNotifyHookState(s *Server) bool {
	if s == nil {
		return false
	}
	return s.notifyHookState.hasHook()
}

func incrementToolCallState(s *Server, name string) int64 {
	if s == nil {
		return 0
	}
	return s.toolCallState.increment(name)
}

func nextThreadSeqState(s *Server) int64 {
	if s == nil {
		return 0
	}
	return s.turnTrackingState.nextThreadSeq()
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
	if s == nil {
		return
	}
	s.codeRunState.clearAllAgentWorkDirs()
}
