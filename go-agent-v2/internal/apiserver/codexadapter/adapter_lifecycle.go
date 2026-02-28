package codexadapter

import (
	"context"
	"strconv"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	historysvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/history"
	lifecyclesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/lifecycle"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (a *Adapter) managerProcess(threadID string) any {
	if manager := a.manager(); manager != nil {
		return manager.Get(threadID)
	}
	return nil
}

func (a *Adapter) ThreadStart(ctx context.Context, threadID, cwd, model, modelProvider, approvalPolicy string) (lifecyclesvc.ThreadStartResult, error) {
	return lifecyclesvc.RunThreadStart(ctx, threadID, cwd, model, modelProvider, approvalPolicy, a.allDynamicToolSchemas(), func(ctx context.Context, agentID, name, path, cwd, startInstructions string, dynamicTools []codexsdk.DynamicTool) error {
		if manager := a.manager(); manager != nil {
			return manager.Launch(ctx, agentID, name, path, cwd, startInstructions, dynamicTools)
		}
		return appErrors.New("Server.threadStart", "thread manager is not initialized")
	}, a.managerProcess, func() []lifecyclesvc.AgentInfo { return toLifecycleAgentInfos(a.runningAgents()) }, a.resolveStartInstructionsForLaunch, func(ctx context.Context, threadID string, proc any) {
		if typed, ok := proc.(*codexsdk.AgentProcess); ok {
			a.registerBinding(ctx, threadID, typed)
		}
	}, func(items []lifecyclesvc.AgentInfo) {
		if runtime := a.uiRuntime(); runtime != nil {
			runtime.ReplaceThreads(toRuntimeThreadSnapshots(items))
		}
	})
}

func (a *Adapter) ThreadResume(ctx context.Context, threadID, path, cwd, model string) (lifecyclesvc.ThreadResumeResult, error) {
	id, err := requireThreadID("Server.threadResume", threadID)
	if err != nil {
		return lifecyclesvc.ThreadResumeResult{}, err
	}
	return withProcess(a, "Server.threadResume", id, func(proc *codexsdk.AgentProcess) (lifecyclesvc.ThreadResumeResult, error) {
		return lifecyclesvc.RunThreadResume(ctx, id, path, cwd, model, proc, a.ResolveCodexThreadCandidates, lifecyclesvc.NormalizeCodexThreadID, a.ResumeThread)
	})
}

func (a *Adapter) ThreadFork(threadID string) (lifecyclesvc.ThreadForkResult, error) {
	threadID = strings.TrimSpace(threadID)
	return withProcess(a, "Server.threadFork", threadID, func(proc *codexsdk.AgentProcess) (lifecyclesvc.ThreadForkResult, error) {
		return lifecyclesvc.RunThreadFork(threadID, proc, a.ForkThread, a.nowUnixMilli)
	})
}

func (a *Adapter) ThreadRollback(threadID string, numTurns int) (map[string]any, error) {
	return a.sendThreadCommand("Server.threadRollback", threadID, "/undo", strconv.Itoa(numTurns), "send undo command")
}

func (a *Adapter) ReviewStart(threadID, reviewArgs string) (map[string]any, error) {
	return a.sendThreadCommand("Server.reviewStart", threadID, "/review", reviewArgs, "send review command")
}

func (a *Adapter) sendThreadCommand(methodName, threadID, command, args, wrapMsg string) (map[string]any, error) {
	return withProcess(a, methodName, threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) {
		return lifecyclesvc.RunThreadCommand(proc, methodName, command, args, wrapMsg, a.SendCommand)
	})
}

func (a *Adapter) ThreadRealtimeStart(threadID, prompt string, _ *string) (map[string]any, error) {
	return lifecyclesvc.RunThreadRealtimeStart(threadID, prompt)
}

func (a *Adapter) ThreadRealtimeAppendAudio(threadID string, audio any) (map[string]any, error) {
	return lifecyclesvc.RunThreadRealtimeAppendAudio(threadID, audio)
}

func (a *Adapter) ThreadRealtimeAppendText(threadID, text string) (map[string]any, error) {
	return lifecyclesvc.RunThreadRealtimeAppendText(threadID, text)
}

func (a *Adapter) ThreadRealtimeStop(threadID string) (map[string]any, error) {
	return lifecyclesvc.RunThreadRealtimeStop(threadID)
}

func (a *Adapter) ThreadNameSet(ctx context.Context, threadID, name string) (map[string]any, error) {
	return lifecyclesvc.RunThreadNameSet(ctx, threadID, name, a.managerProcess, func(threadID string) bool {
		return threadExistsInRuntime(threadID, a.uiRuntime())
	}, a.ThreadExistsInHistory, a.sendCommandFromAny, func(threadID, alias string) {
		if runtime := a.uiRuntime(); runtime != nil {
			runtime.SetThreadName(threadID, alias)
		}
	}, a.persistThreadAlias)
}

func (a *Adapter) ThreadRead(_ context.Context, threadID string) (map[string]any, error) {
	return withProcess(a, "Server.threadRead", threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) {
		return lifecyclesvc.RunThreadRead(proc, a.ListThreads)
	})
}

func (a *Adapter) ThreadResolve(ctx context.Context, threadID string) (map[string]any, error) {
	return lifecyclesvc.RunThreadResolve(
		ctx,
		threadID,
		func(id string) (state string, port int, codexThreadID string, found bool) {
			return lifecyclesvc.ResolveRunningThreadIdentityFromAgents(id, toLifecycleAgentInfos(a.runningAgents()))
		},
		func(ctx context.Context, id string) string {
			return lifecyclesvc.FirstResolvedCodexThreadIDFromCandidates(ctx, id, a.ResolveCodexThreadCandidates)
		},
		a.ThreadExistsInHistory,
	)
}

func (a *Adapter) ResolveCodexThreadCandidates(ctx context.Context, agentID string, appendUniqueThreadID func(dst []string, seen map[string]struct{}, candidate string) []string, previewCandidates func([]string, int) []string) []string {
	if previewCandidates == nil {
		previewCandidates = lifecyclesvc.PreviewResumeCandidates
	}
	return historysvc.ResolveCodexThreadCandidates(
		ctx,
		agentID,
		0,
		appendUniqueThreadID,
		func(ctx context.Context, agentID string) (string, error) {
			binding, err := a.findBindingByAgentID(ctx, agentID)
			if err != nil || binding == nil {
				return "", err
			}
			return binding.CodexThreadID, nil
		},
		func(ctx context.Context, agentID string) (string, error) {
			status, err := a.findStatusByAgentID(ctx, agentID)
			if err != nil || status == nil {
				return "", err
			}
			return status.SessionID, nil
		},
		previewCandidates,
	)
}

func (a *Adapter) ThreadExistsInHistory(ctx context.Context, threadID string) bool {
	return historysvc.ThreadExistsInHistory(
		ctx,
		threadID,
		0,
		lifecyclesvc.IsLikelyCodexThreadID,
		func(ctx context.Context, agentID string) (bool, error) {
			binding, err := a.findBindingByAgentID(ctx, agentID)
			return binding != nil, err
		},
		func(ctx context.Context, agentID string) (bool, error) {
			status, err := a.findStatusByAgentID(ctx, agentID)
			return status != nil, err
		},
		a.loadThreadArchiveMap,
	)
}

func (a *Adapter) recoverProcess(threadID, reason string) {
	if manager := a.manager(); manager != nil {
		if err := manager.RecoverAgent(threadID, reason); err != nil {
			logger.Warn("codexadapter: process recovery failed",
				logger.FieldAgentID, threadID,
				"reason", reason,
				logger.FieldError, err,
			)
		}
	}
}
