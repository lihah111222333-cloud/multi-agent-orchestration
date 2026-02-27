package lifecycle

import (
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	lifecyclesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/lifecycle"
)

type AgentInfo = lifecyclesvc.AgentInfo
type ThreadSnapshot = lifecyclesvc.ThreadSnapshot
type ThreadStartResult = lifecyclesvc.ThreadStartResult
type ThreadResumeResult = lifecyclesvc.ThreadResumeResult
type ThreadForkResult = lifecyclesvc.ThreadForkResult

func mapSlice[S any, D any](in []S, mapFn func(S) D) []D {
	if len(in) == 0 {
		return nil
	}
	out := make([]D, len(in))
	for i, item := range in {
		out[i] = mapFn(item)
	}
	return out
}

func ToAgentInfos(items []codexsdk.AgentInfo) []AgentInfo {
	return mapSlice(items, func(item codexsdk.AgentInfo) AgentInfo {
		return AgentInfo{ID: item.ID, Name: item.Name, State: string(item.State), Port: item.Port, ThreadID: item.ThreadID}
	})
}

func ToRuntimeThreadSnapshots(items []AgentInfo) []uistate.ThreadSnapshot {
	return mapSlice(items, func(item AgentInfo) uistate.ThreadSnapshot {
		return uistate.ThreadSnapshot{ID: item.ID, Name: item.Name, State: item.State}
	})
}

func ThreadExistsInRuntime(threadID string, runtime *uistate.RuntimeManager) bool {
	if runtime == nil {
		return false
	}
	snapshots := runtime.SnapshotLight().Threads
	return ThreadExistsInRuntimeSnapshots(threadID, mapSlice(snapshots, func(item uistate.ThreadSnapshot) ThreadSnapshot {
		return ThreadSnapshot{ID: item.ID}
	}))
}

var (
	AppendUniqueThreadIDFallback            = common.AppendUniqueThreadIDFallback
	RunThreadStart                          = lifecyclesvc.RunThreadStart
	RunThreadResume                         = lifecyclesvc.RunThreadResume
	RunThreadFork                           = lifecyclesvc.RunThreadFork
	RunThreadRealtimeStart                  = lifecyclesvc.RunThreadRealtimeStart
	RunThreadRealtimeAppendAudio            = lifecyclesvc.RunThreadRealtimeAppendAudio
	RunThreadRealtimeAppendText             = lifecyclesvc.RunThreadRealtimeAppendText
	RunThreadRealtimeStop                   = lifecyclesvc.RunThreadRealtimeStop
	RunTurnSteer                            = lifecyclesvc.RunTurnSteer
	RunThreadCommand                        = lifecyclesvc.RunThreadCommand
	RunThreadNameSet                        = lifecyclesvc.RunThreadNameSet
	RunThreadRead                           = lifecyclesvc.RunThreadRead
	RunThreadResolve                        = lifecyclesvc.RunThreadResolve
	FirstResolvedCodexThreadIDFromCandidates = lifecyclesvc.FirstResolvedCodexThreadIDFromCandidates
	ResolveRunningThreadIdentityFromAgents  = lifecyclesvc.ResolveRunningThreadIdentityFromAgents
	ThreadExistsInRuntimeSnapshots          = lifecyclesvc.ThreadExistsInRuntimeSnapshots
	NormalizeCodexThreadID                  = lifecyclesvc.NormalizeCodexThreadID
	IsLikelyCodexThreadID                   = lifecyclesvc.IsLikelyCodexThreadID
	BuildResumeCandidates                   = lifecyclesvc.BuildResumeCandidates
	TryResumeCandidates                     = lifecyclesvc.TryResumeCandidates
	IsHistoricalResumeCandidateError        = lifecyclesvc.IsHistoricalResumeCandidateError
	IsCodexProcessCrashError                = lifecyclesvc.IsCodexProcessCrashError
	PreviewResumeCandidates                 = lifecyclesvc.PreviewResumeCandidates
)
