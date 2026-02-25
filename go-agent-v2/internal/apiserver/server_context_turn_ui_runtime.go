package apiserver

import "time"

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
