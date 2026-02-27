package codexadapter

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/store"
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

type managerAdapter struct {
	manager *codexsdk.AgentManager
}

func (a managerAdapter) Get(agentID string) agentcore.Process {
	return wrapProcess(a.manager.Get(agentID))
}

func (a managerAdapter) Launch(ctx context.Context, agentID, alias, profile, cwd, startInstructions string, dynamicTools []agentcore.DynamicTool) error {
	return a.manager.Launch(ctx, agentID, alias, profile, cwd, startInstructions, dynamicTools)
}

func (a managerAdapter) Stop(agentID string) error {
	return a.manager.Stop(agentID)
}

type bindingStoreAdapter struct {
	store *store.AgentCodexBindingStore
}

func (a bindingStoreAdapter) Bind(ctx context.Context, agentID, codexThreadID, sessionID string) error {
	return a.store.Bind(ctx, agentID, codexThreadID, sessionID)
}

func (a bindingStoreAdapter) FindByAgentID(ctx context.Context, agentID string) (*agentcore.Binding, error) {
	binding, err := a.store.FindByAgentID(ctx, agentID)
	if err != nil || binding == nil {
		return nil, err
	}
	return &agentcore.Binding{CodexThreadID: binding.CodexThreadID}, nil
}

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

type processAdapter struct {
	proc *codexsdk.AgentProcess
}

func (p processAdapter) Port() int {
	if p.proc != nil && p.proc.Client != nil {
		return p.proc.Client.GetPort()
	}
	return 0
}

func (p processAdapter) IsAlive() bool {
	return p.proc != nil && p.proc.IsAlive()
}

func wrapProcess(proc *codexsdk.AgentProcess) agentcore.Process {
	if proc == nil {
		return nil
	}
	return processAdapter{proc: proc}
}

func unwrapProcess(proc agentcore.Process) *codexsdk.AgentProcess {
	switch p := proc.(type) {
	case processAdapter:
		return p.proc
	case *processAdapter:
		if p == nil {
			return nil
		}
		return p.proc
	default:
		return nil
	}
}

func (a *Adapter) resolveThreadFromSlashCommand(
	ctx context.Context,
	threadID string,
	requireThreadID bool,
) (string, error) {
	return commandsvc.ResolveThreadForSlashCommandLogic(ctx, threadID, requireThreadID, func(ctx context.Context) ([]commandsvc.ThreadListItem, error) {
		return a.ThreadList(ctx)
	})
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
	if proc == nil || proc.Client == nil {
		return ""
	}
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

func (a *Adapter) resolveCodexThreadCandidatesForRuntime(ctx context.Context, agentID string) []string {
	return a.ResolveCodexThreadCandidates(ctx, agentID, commonsvc.AppendUniqueThreadIDFallback, lifecyclesvc.PreviewResumeCandidates)
}

func (a *Adapter) runtimeServiceAdapter() runtimesvc.RuntimeAdapter {
	if a == nil {
		return runtimesvc.RuntimeAdapter{}
	}
	ctxDeps := a.depsOrDefault()
	prepare := runtimesvc.PrepareAdapter{
		MergePromptText:                commonadapter.MergePromptText,
		FileContentInputText:           commonadapter.FileContentInputText,
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
		DefaultLSPUsagePromptHint:      func() string { return defaultLSPUsagePromptHint },
		MaxLSPUsagePromptHintLen:       func() int { return maxLSPUsagePromptHintLen },
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
		ThreadLogFields:                   threadLogFields,
		CancelCodeRuns:                    a.cancelCodeRuns,
		ResolveCodexThreadCandidates:      a.resolveCodexThreadCandidatesForRuntime,
		IsCodexProcessCrashError:          lifecyclesvc.IsCodexProcessCrashError,
		IsHistoricalResumeCandidateError:  lifecyclesvc.IsHistoricalResumeCandidateError,
		PreviewResumeCandidates:           lifecyclesvc.PreviewResumeCandidates,
		Notify:                            a.notify,
		NormalizeSkillNames:               commonadapter.NormalizeSkillNames,
		BeginTrackedTurn:                  a.beginTrackedTurn,
		TurnSteer:                         a.TurnSteer,
		GetThreadID: func(proc agentcore.Process) string {
			return a.GetThreadID(unwrapProcess(proc))
		},
		BindingStore: func() agentcore.BindingStore {
			bindingStore := a.bindingStore()
			if bindingStore == nil {
				return nil
			}
			return bindingStoreAdapter{store: bindingStore}
		},
		ResumeThread: func(proc agentcore.Process, req runtimesvc.ResumeThreadRequest) error {
			return a.ResumeThread(unwrapProcess(proc), codexsdk.ResumeThreadRequest{ThreadID: req.ThreadID, Cwd: req.Cwd})
		},
		Submit: func(proc agentcore.Process, prompt string, images, files []string, outputSchema json.RawMessage) error {
			return a.Submit(unwrapProcess(proc), prompt, images, files, outputSchema)
		},
		ResolveClientActiveTurnID: func(proc agentcore.Process) string {
			return a.resolveClientActiveTurnIDForRuntime(unwrapProcess(proc))
		},
	}
	if manager := a.manager(); manager != nil {
		adapter.Manager = func() agentcore.Manager { return managerAdapter{manager: manager} }
	}
	return adapter
}

func (a *Adapter) resolveClientActiveTurnIDForRuntime(proc *codexsdk.AgentProcess) string {
	if proc == nil || proc.Client == nil {
		return ""
	}
	reader, ok := proc.Client.(interface{ GetActiveTurnID() string })
	if !ok {
		return ""
	}
	return strings.TrimSpace(reader.GetActiveTurnID())
}

func (a *Adapter) resolveProcess(caller, threadID string) (*codexsdk.AgentProcess, error) {
	return withProcess(a, caller, threadID, func(proc *codexsdk.AgentProcess) (*codexsdk.AgentProcess, error) {
		return proc, nil
	})
}

func withProcess[T any](a *Adapter, caller string, threadID string, fn func(*codexsdk.AgentProcess) (T, error)) (T, error) {
	return runtimesvc.WithProcess(a.runtimeServiceAdapter(), caller, threadID, func(proc agentcore.Process) (T, error) {
		return fn(unwrapProcess(proc))
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
	if err != nil {
		return nil, err
	}
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
	result, err := a.sendSlashCommand(context.Background(), "Server.threadSkillsList", "", "/skills", "", false)
	return commandsvc.ThreadSkillsListResult(result, err)
}

func (a *Adapter) sendSlashCommand(ctx context.Context, methodName, threadID, command, args string, requireThreadID bool) (map[string]any, error) {
	return commandsvc.RunSendSlashCommand(ctx, methodName, threadID, command, args, requireThreadID, a.resolveThreadFromSlashCommand, a.withProcessMap, a.sendCommandFromAny)
}
