package codexadapter

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	commandsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/command"
	commonsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	lifecyclesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/lifecycle"
	promptsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/prompt"
	runtimesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
	trackersvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/tracker"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type uiRuntimeAdapter struct {
	runtime *uistate.RuntimeManager
}

func (a uiRuntimeAdapter) AppendUserMessage(threadID, text string, attachments []agentcore.TimelineAttachment) {
	a.runtime.AppendUserMessage(threadID, text, mapSlice(attachments, func(item agentcore.TimelineAttachment) uistate.TimelineAttachment {
		return uistate.TimelineAttachment{Kind: item.Kind, Name: item.Name, Path: item.Path, PreviewURL: item.PreviewURL}
	}))
}

func (a uiRuntimeAdapter) ThreadTimeline(threadID string) []agentcore.TimelineItem {
	return mapSlice(a.runtime.ThreadTimeline(threadID), func(item uistate.TimelineItem) agentcore.TimelineItem {
		return agentcore.TimelineItem{Kind: item.Kind, Text: item.Text}
	})
}

func coreProcess(proc agentcore.Process) *codexsdk.AgentProcess { typed, _ := proc.(*codexsdk.AgentProcess); return typed }

func (a *Adapter) resolveThreadFromSlashCommand(ctx context.Context, threadID string, requireThreadID bool) (string, error) {
	return commandsvc.ResolveThreadForSlashCommandLogic(ctx, threadID, requireThreadID, a.ThreadList)
}

func (a *Adapter) withProcessMap(methodName string, threadID string, fn func(any) (map[string]any, error)) (map[string]any, error) {
	return withProcess(a, methodName, threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) { return fn(proc) })
}

func (a *Adapter) sendCommandFromAny(proc any, command, args string) error {
	typed, _ := proc.(*codexsdk.AgentProcess)
	return a.SendCommand(typed, command, args)
}

func (a *Adapter) Submit(proc *codexsdk.AgentProcess, prompt string, images, files []string, outputSchema json.RawMessage) error {
	return withClient(proc, func(c codexsdk.Client) error { return c.Submit(prompt, images, files, outputSchema) })
}

func (a *Adapter) SendCommand(proc *codexsdk.AgentProcess, command string, args string) error {
	return withClient(proc, func(c codexsdk.Client) error { return c.SendCommand(command, args) })
}

func (a *Adapter) GetThreadID(proc *codexsdk.AgentProcess) string {
	if proc == nil || proc.Client == nil { return "" }
	return strings.TrimSpace(proc.Client.GetThreadID())
}

func (a *Adapter) ResumeThread(proc *codexsdk.AgentProcess, req codexsdk.ResumeThreadRequest) error {
	return withClient(proc, func(c codexsdk.Client) error { return c.ResumeThread(req) })
}

func (a *Adapter) ListThreads(proc *codexsdk.AgentProcess) ([]codexsdk.ThreadInfo, error) {
	return withClientE(proc, func(c codexsdk.Client) ([]codexsdk.ThreadInfo, error) { return c.ListThreads() })
}

func (a *Adapter) ForkThread(proc *codexsdk.AgentProcess, req codexsdk.ForkThreadRequest) (*codexsdk.ForkThreadResponse, error) {
	return withClientE(proc, func(c codexsdk.Client) (*codexsdk.ForkThreadResponse, error) { return c.ForkThread(req) })
}

func (a *Adapter) RespondError(proc *codexsdk.AgentProcess, id int64, code int, message string) error {
	return withClient(proc, func(c codexsdk.Client) error { return c.RespondError(id, code, message) })
}

func (a *Adapter) SendDynamicToolResult(proc *codexsdk.AgentProcess, callID, output string, requestID *int64) error {
	return withClient(proc, func(c codexsdk.Client) error { return c.SendDynamicToolResult(callID, output, requestID) })
}

func (a *Adapter) resolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []codexsdk.DynamicTool) string {
	hint := promptsvc.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen, a.storeGetter())
	startInstructions, warnings := promptsvc.PrependLSPAvailabilityWarning(
		hint,
		promptsvc.CollectDynamicToolNames(dynamicTools),
		promptsvc.CollectReferencedLSPToolNames,
		commonadapter.MergePromptText,
	)
	if len(warnings) > 0 {
		logger.Warn("codexadapter: start instructions warnings: " + strings.Join(warnings, "; "))
	}
	return startInstructions
}

func (a *Adapter) ResolveLSPUsagePromptHint(ctx context.Context, defaultHint string, maxHintLen int) string {
	return promptsvc.ResolveLSPUsagePromptHint(ctx, defaultHint, maxHintLen, a.storeGetter())
}

func (a *Adapter) runtimeServiceAdapter() runtimesvc.RuntimeAdapter {
	if a == nil {
		return runtimesvc.RuntimeAdapter{}
	}
	ctxDeps := a.depsOrDefault()
	prepare := runtimesvc.PrepareAdapter{
		BuildAttachmentName:            util.BuildAttachmentName,
		BuildAttachmentPreviewURL:      util.BuildAttachmentPreviewURL,
		BuildSelectedSkillPrompt:       a.buildSelectedSkillPrompt,
		ListSkillMatchCandidates:       ctxDeps.ListSkillMatchCandidates,
		ListAgentSkills:                ctxDeps.GetAgentSkills,
		CollectAutoMatchedSkillMatches: promptsvc.CollectAutoMatchedSkillMatches,
		RenderAutoMatchedSkillPrompt:   a.renderAutoMatchedSkillPrompt,
		ActiveTrackedTurnID:            a.activeTrackedTurnID,
		ShowInjectedPromptInChat:       a.showInjectedPromptInChat,
		ResolveLSPUsagePromptHint:      a.ResolveLSPUsagePromptHint,
	}
	if runtime := a.uiRuntime(); runtime != nil {
		prepare.UIRuntime = func() agentcore.TimelineRuntime { return uiRuntimeAdapter{runtime: runtime} }
	}
	adapter := runtimesvc.RuntimeAdapter{
		PrepareAdapter:                    prepare,
		ThreadExistsInHistory:             a.ThreadExistsInHistory,
		AllDynamicToolSchemas:             a.allDynamicToolSchemas,
		ResolveStartInstructionsForLaunch: a.resolveStartInstructionsForLaunch,
		SetAgentWorkDir:                   a.setAgentWorkDir,
		CancelCodeRuns:                    a.cancelCodeRuns,
		ResolveCodexThreadCandidates: func(ctx context.Context, agentID string) []string {
			return a.ResolveCodexThreadCandidates(ctx, agentID, commonsvc.AppendUniqueThreadIDFallback, lifecyclesvc.PreviewResumeCandidates)
		},
		IsCodexProcessCrashError:         lifecyclesvc.IsCodexProcessCrashError,
		IsHistoricalResumeCandidateError: lifecyclesvc.IsHistoricalResumeCandidateError,
		PreviewResumeCandidates:          lifecyclesvc.PreviewResumeCandidates,
		Notify:                           a.notify,
		NormalizeSkillNames:              commonadapter.NormalizeSkillNames,
		BeginTrackedTurn:                 a.beginTrackedTurn,
		TurnSteer:                        a.TurnSteer,
		GetThreadID: func(proc agentcore.Process) string {
			return a.GetThreadID(coreProcess(proc))
		},
		BindingStore: func() agentcore.BindingStore { return a.bindingStore() },
		ResumeThread: func(proc agentcore.Process, req runtimesvc.ResumeThreadRequest) error {
			return a.ResumeThread(coreProcess(proc), codexsdk.ResumeThreadRequest{ThreadID: req.ThreadID, Cwd: req.Cwd})
		},
		Submit: func(proc agentcore.Process, prompt string, images, files []string, outputSchema json.RawMessage) error {
			return a.Submit(coreProcess(proc), prompt, images, files, outputSchema)
		},
		ResolveClientActiveTurnID: func(proc agentcore.Process) string {
			if typed := coreProcess(proc); typed != nil && typed.Client != nil {
				if reader, ok := typed.Client.(interface{ GetActiveTurnID() string }); ok {
					return strings.TrimSpace(reader.GetActiveTurnID())
				}
			}
			return ""
		},
	}
	if manager := a.manager(); manager != nil { adapter.Manager = func() agentcore.Manager { return manager } }
	return adapter
}

func withProcess[T any](a *Adapter, caller string, threadID string, fn func(*codexsdk.AgentProcess) (T, error)) (T, error) {
	return runtimesvc.WithProcess(a.runtimeServiceAdapter(), caller, threadID, func(proc agentcore.Process) (T, error) {
		return fn(coreProcess(proc))
	})
}

func (a *Adapter) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	return withProcess(a, "Server.turnSteer", threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) {
		return lifecyclesvc.RunTurnSteer(proc, a.Submit, submitPrompt, images, files)
	})
}

func (a *Adapter) CollectAutoMatchedSkillMatchesForThread(threadID string, prompt string, input []contracts.TurnInput, options contracts.AutoSkillMatchOptions) []contracts.AutoMatchedSkillMatch {
	return runtimesvc.CollectAutoMatchedSkillMatchesForThread(a.runtimeServiceAdapter().PrepareAdapter, threadID, prompt, input, options)
}

func (a *Adapter) TurnStart(ctx context.Context, req contracts.TurnStartRequest) (agentcore.TurnStartEntryResult, error) {
	return runtimesvc.TurnStart(a.runtimeServiceAdapter(), ctx, req)
}

func (a *Adapter) TurnSteerFromInput(req contracts.TurnSteerRequest) (map[string]any, error) {
	return runtimesvc.TurnSteerFromInput(a.runtimeServiceAdapter(), req)
}

func (a *Adapter) TurnSteerFromInputAligned(req contracts.TurnSteerRequest) (map[string]any, error) {
	adapter := a.runtimeServiceAdapter()
	return runtimesvc.TurnSteerFromInputAlignedByAdapter(adapter.PrepareAdapter, req, func(runtimeReq agentcore.TurnSteerRequest) (map[string]any, error) {
		return runtimesvc.TurnSteerFromInput(adapter, runtimeReq)
	})
}

func (a *Adapter) sendSlashCommandWithParams(ctx context.Context, params json.RawMessage, command, argKey string, requireThreadID bool) (any, error) {
	parsed, err := commandsvc.ParseSlashCommandArgParams(params, argKey, trackersvc.ExtractTrackedString)
	if err != nil { return nil, err }
	return a.sendSlashCommand(ctx, "Server.sendSlashCommand", parsed.ThreadID, command, parsed.Args, requireThreadID)
}

func (a *Adapter) SendSlashCommandFromRawParams(ctx context.Context, params json.RawMessage, command string) (any, error) {
	return a.sendSlashCommandWithParams(ctx, params, command, "args", false)
}

func (a *Adapter) SendSlashCommandFromRawParamsRequireThreadID(ctx context.Context, params json.RawMessage, command string) (any, error) {
	return a.sendSlashCommandWithParams(ctx, params, command, "args", true)
}

func (a *Adapter) SendSlashCommandWithArgs(params json.RawMessage, command, argKey string) (any, error) {
	return a.sendSlashCommandWithParams(context.Background(), params, command, argKey, false)
}

func (a *Adapter) ThreadSkillsList() (any, error) {
	return commandsvc.ThreadSkillsListResult(a.sendSlashCommand(context.Background(), "Server.threadSkillsList", "", "/skills", "", false))
}

func (a *Adapter) sendSlashCommand(ctx context.Context, methodName, threadID, command, args string, requireThreadID bool) (map[string]any, error) {
	return commandsvc.RunSendSlashCommand(ctx, methodName, threadID, command, args, requireThreadID, a.resolveThreadFromSlashCommand, a.withProcessMap, a.sendCommandFromAny)
}
