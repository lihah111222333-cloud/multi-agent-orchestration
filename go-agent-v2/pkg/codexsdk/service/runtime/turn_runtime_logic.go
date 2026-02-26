package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func EnsureThreadReadyForTurn(a RuntimeAdapter, ctx context.Context, threadID, cwd string) (Process, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	manager := a.Manager()
	if manager == nil {
		return nil, a.NewError("Server.ensureThreadReady", "thread manager is not initialized")
	}
	id, err := a.RequireThreadID("Server.ensureThreadReady", threadID)
	if err != nil {
		return nil, err
	}
	launchCwd := strings.TrimSpace(cwd)
	if launchCwd == "" {
		launchCwd = "."
	}

	if proc, ok := EnsureReadyRunningProcess(a, ctx, manager, id, launchCwd); ok {
		return proc, nil
	}

	hasHistory := a.ThreadExistsInHistory(ctx, id)
	if !hasHistory {
		return nil, a.NewErrorf("Server.ensureThreadReady", "thread %s not found", id)
	}
	resumeCandidates := CollectResumeCandidates(a, ctx, id)

	logger.Info("turn/start: restoring historical thread",
		append(a.ThreadLogFields(id),
			"has_history", hasHistory,
			logger.FieldCwd, launchCwd,
			"candidate_count", len(resumeCandidates),
			"candidates", a.PreviewResumeCandidates(resumeCandidates, 4),
		)...,
	)

	dynamicTools := a.AllDynamicToolSchemas()
	startInstructions := a.ResolveStartInstructionsForLaunch(ctx, dynamicTools)

	proc, err := EnsureReadyLaunchProcess(a, ctx, manager, id, launchCwd, startInstructions, dynamicTools)
	if err != nil {
		return nil, err
	}
	if len(resumeCandidates) == 0 {
		return EnsureReadyNoResumeCandidates(a, id, proc), nil
	}

	resumed, lastResumeErr, fatalResumeErr := TryResumeHistoricalCandidates(a, ctx, manager, proc, id, launchCwd, resumeCandidates)
	if fatalResumeErr != nil {
		return nil, fatalResumeErr
	}
	if resumed {
		return proc, nil
	}

	if lastResumeErr != nil {
		return EnsureReadyResumeFallback(
			a,
			ctx,
			manager,
			id,
			launchCwd,
			proc,
			lastResumeErr,
			startInstructions,
			dynamicTools,
			len(resumeCandidates),
		)
	}

	return EnsureReadyNoHistoricalRollout(a, ctx, id, launchCwd, proc, len(resumeCandidates)), nil
}

func EnsureReadyRunningProcess(
	a RuntimeAdapter,
	ctx context.Context,
	manager Manager,
	agentID string,
	launchCwd string,
) (Process, bool) {
	if manager == nil {
		return nil, false
	}
	proc := manager.Get(agentID)
	if proc == nil {
		return nil, false
	}
	logger.Info("turn/start: using running process",
		append(a.ThreadLogFields(agentID),
			logger.FieldPort, proc.Port(),
			"codex_thread_id", a.GetThreadID(proc),
		)...,
	)
	a.SetAgentWorkDir(agentID, launchCwd)
	RegisterBinding(a, ctx, agentID, proc)
	return proc, true
}

func EnsureReadyLaunchProcess(
	a RuntimeAdapter,
	ctx context.Context,
	manager Manager,
	agentID string,
	launchCwd string,
	startInstructions string,
	dynamicTools []agentcore.DynamicTool,
) (Process, error) {
	if manager == nil {
		return nil, a.NewError("Server.ensureThreadReady", "thread manager is not initialized")
	}
	if err := manager.Launch(ctx, agentID, agentID, "", launchCwd, startInstructions, dynamicTools); err != nil {
		if proc := manager.Get(agentID); proc != nil {
			a.SetAgentWorkDir(agentID, launchCwd)
			return proc, nil
		}
		return nil, a.WrapErrorf(err, "Server.ensureThreadReady", "auto-load thread %s", agentID)
	}

	proc := manager.Get(agentID)
	if proc == nil {
		return nil, a.NewErrorf("Server.ensureThreadReady", "thread %s loaded but not found", agentID)
	}
	a.SetAgentWorkDir(agentID, launchCwd)
	logger.Info("turn/start: process launched for restore",
		append(a.ThreadLogFields(agentID),
			logger.FieldPort, proc.Port(),
			"codex_thread_id_before_resume", a.GetThreadID(proc),
		)...,
	)
	return proc, nil
}

func EnsureReadyNoResumeCandidates(a RuntimeAdapter, agentID string, proc Process) Process {
	logger.Warn("turn/start: no valid historical codex thread id, continue with fresh session",
		a.ThreadLogFields(agentID)...,
	)
	if proc != nil {
		proc.MarkSessionLost()
	}
	return proc
}

func EnsureReadyResumeFallback(
	a RuntimeAdapter,
	ctx context.Context,
	manager Manager,
	agentID string,
	launchCwd string,
	proc Process,
	lastResumeErr error,
	startInstructions string,
	dynamicTools []agentcore.DynamicTool,
	candidateCount int,
) (Process, error) {
	logger.Warn("turn/start: all resume candidates exhausted, fallback to fresh session",
		append(a.ThreadLogFields(agentID),
			"candidate_count", candidateCount,
			"last_error", lastResumeErr,
			logger.FieldCwd, launchCwd,
		)...,
	)
	if manager != nil && manager.Get(agentID) == nil {
		a.CancelCodeRuns(agentID)
		_ = manager.Stop(agentID)
		if launchErr := manager.Launch(ctx, agentID, agentID, "", launchCwd, startInstructions, dynamicTools); launchErr != nil {
			return nil, a.WrapErrorf(launchErr, "Server.ensureThreadReady", "final re-spawn thread %s", agentID)
		}
		proc = manager.Get(agentID)
		if proc == nil {
			return nil, a.NewErrorf("Server.ensureThreadReady", "thread %s final re-spawn failed", agentID)
		}
	}
	if proc != nil {
		proc.MarkSessionLost()
	}
	NotifySessionLost(a, agentID, lastResumeErr)
	RegisterBinding(a, ctx, agentID, proc)
	return proc, nil
}

func EnsureReadyNoHistoricalRollout(
	a RuntimeAdapter,
	ctx context.Context,
	agentID string,
	launchCwd string,
	proc Process,
	candidateCount int,
) Process {
	logger.Warn("turn/start: no available historical rollout, continue with fresh session",
		append(a.ThreadLogFields(agentID),
			"candidate_count", candidateCount,
			logger.FieldCwd, launchCwd,
		)...,
	)
	if proc != nil {
		proc.MarkSessionLost()
	}
	RegisterBinding(a, ctx, agentID, proc)
	return proc
}

func RegisterBinding(a RuntimeAdapter, ctx context.Context, agentID string, proc Process) {
	bindingStore := a.BindingStore()
	if bindingStore == nil || proc == nil {
		return
	}
	codexThreadID := a.GetThreadID(proc)
	if codexThreadID == "" {
		return
	}
	if err := bindingStore.Bind(ctx, agentID, codexThreadID, ""); err != nil {
		logger.Warn("turn/start: failed to register binding",
			append(a.ThreadLogFields(agentID),
				"codex_thread_id", codexThreadID,
				logger.FieldError, err,
			)...,
		)
	}
}

func BuildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
	detail := ""
	if lastErr != nil {
		detail = lastErr.Error()
	}
	return "ui/state/changed", map[string]any{
		"source":   "session_lost_warning",
		"agent_id": agentID,
		"warning":  "会话历史已丢失 (codex session 文件不存在)，已自动回退到全新会话",
		"detail":   detail,
	}
}

func NotifySessionLost(a RuntimeAdapter, agentID string, lastErr error) {
	method, payload := BuildSessionLostNotification(agentID, lastErr)
	a.Notify(method, payload)
}

func CollectResumeCandidates(a RuntimeAdapter, ctx context.Context, agentID string) []string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil
	}
	resumeCandidates := make([]string, 0, 4)
	if bindingStore := a.BindingStore(); bindingStore != nil {
		if binding, err := bindingStore.FindByAgentID(ctx, id); err == nil && binding != nil {
			resumeCandidates = append(resumeCandidates, binding.CodexThreadID)
			logger.Info("turn/start: found DB binding",
				append(a.ThreadLogFields(id),
					"bound_codex_thread_id", binding.CodexThreadID,
				)...,
			)
		}
	}
	if len(resumeCandidates) == 0 {
		resumeCandidates = append(resumeCandidates, a.ResolveCodexThreadCandidates(ctx, id)...)
	}
	return resumeCandidates
}

func TryResumeHistoricalCandidates(
	a RuntimeAdapter,
	ctx context.Context,
	manager Manager,
	proc Process,
	agentID string,
	launchCwd string,
	resumeCandidates []string,
) (resumed bool, lastResumeErr error, fatalErr error) {
	id := strings.TrimSpace(agentID)
	if id == "" || proc == nil {
		return false, nil, nil
	}
	for _, resumeThreadID := range resumeCandidates {
		err := a.ResumeThread(proc, ResumeThreadRequest{ThreadID: resumeThreadID, Cwd: launchCwd})
		if err == nil {
			logger.Info("turn/start: historical thread auto-loaded",
				append(a.ThreadLogFields(id),
					"resume_thread_id", resumeThreadID,
					"codex_thread_id_after_resume", a.GetThreadID(proc),
					logger.FieldCwd, launchCwd,
				)...,
			)
			RegisterBinding(a, ctx, id, proc)
			return true, nil, nil
		}

		lastResumeErr = err
		if a.IsCodexProcessCrashError(err) {
			logger.Error("turn/start: codex crashed during resume, returning error",
				append(a.ThreadLogFields(id),
					"resume_thread_id", resumeThreadID,
					logger.FieldError, err,
				)...,
			)
			a.CancelCodeRuns(id)
			if manager != nil {
				_ = manager.Stop(id)
			}
			NotifySessionLost(a, id, err)
			return false, lastResumeErr, a.WrapErrorf(err, "Server.ensureThreadReady",
				"codex crashed while resuming thread %s (rollout=%s)", id, resumeThreadID)
		}

		if a.IsHistoricalResumeCandidateError(err) {
			logger.Warn("turn/start: resume candidate unavailable, try next",
				append(a.ThreadLogFields(id),
					"resume_thread_id", resumeThreadID,
					logger.FieldError, err,
				)...,
			)
			continue
		}

		logger.Error("turn/start: unrecognized resume error",
			append(a.ThreadLogFields(id),
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)...,
		)
		return false, lastResumeErr, a.WrapErrorf(err, "Server.ensureThreadReady",
			"resume failed for thread %s (rollout=%s)", id, resumeThreadID)
	}
	return false, lastResumeErr, nil
}

func TurnStart(a RuntimeAdapter, ctx context.Context, req TurnStartRequest) (TurnStartEntryResult, error) {
	threadID, err := a.RequireThreadID("Server.turnStart", req.ThreadID)
	if err != nil {
		return TurnStartEntryResult{}, err
	}
	logger.Info("turn/start: request received",
		append(a.ThreadLogFields(threadID),
			logger.FieldCwd, strings.TrimSpace(req.Cwd),
			"input_count", len(req.Input),
			"selected_skills_count", len(req.SelectedSkills),
		)...,
	)
	selectedSkills, err := a.NormalizeSkillNames(req.SelectedSkills)
	if err != nil {
		return TurnStartEntryResult{}, a.WrapError(err, "Server.turnStart", "normalize selected skills")
	}
	prepared, err := PrepareTurnStartSubmission(a, threadID, req.Input, selectedSkills, req.ManualSkillSelection)
	if err != nil {
		return TurnStartEntryResult{}, err
	}
	logger.Info("turn/start: input prepared",
		append(a.ThreadLogFields(threadID),
			"text_len", len(prepared.Prompt),
			"images", len(prepared.Images),
			"files", len(prepared.Files),
			"selected_skills_requested", len(selectedSkills),
			"selected_skills_injected", prepared.SelectedSkillCount,
			"manual_skill_selection", req.ManualSkillSelection,
			"auto_matched_skills", prepared.AutoMatchedSkillCount,
		)...,
	)

	turnID, err := StartTurnSubmissionAndTrack(
		a,
		ctx,
		threadID,
		req.Cwd,
		prepared.SubmitPrompt,
		prepared.Images,
		prepared.Files,
		req.OutputSchema,
	)
	if err != nil {
		return TurnStartEntryResult{}, err
	}
	AppendTurnStartUserTimeline(a, ctx, prepared.TimelineAttachments, TurnAppendUserTimelineOptions{
		ThreadID:     threadID,
		Prompt:       prepared.Prompt,
		SubmitPrompt: prepared.SubmitPrompt,
		Images:       prepared.Images,
		Files:        prepared.Files,
	})
	return TurnStartEntryResult{TurnID: turnID}, nil
}

func TurnSteerFromInput(a RuntimeAdapter, req TurnSteerRequest) (map[string]any, error) {
	threadID, err := a.RequireThreadID("Server.turnSteer", req.ThreadID)
	if err != nil {
		return nil, err
	}
	selectedSkills, err := a.NormalizeSkillNames(req.SelectedSkills)
	if err != nil {
		return nil, a.WrapError(err, "Server.turnSteer", "normalize selected skills")
	}
	prepared, err := PrepareTurnSteerSubmission(a, threadID, req.Input, selectedSkills, req.ManualSkillSelection)
	if err != nil {
		return nil, err
	}
	return a.TurnSteer(threadID, prepared.SubmitPrompt, prepared.Images, prepared.Files)
}

func StartTurnSubmissionAndTrack(
	a RuntimeAdapter,
	ctx context.Context,
	threadID string,
	cwd string,
	submitPrompt string,
	images []string,
	files []string,
	outputSchema json.RawMessage,
) (string, error) {
	threadID, err := a.RequireThreadID("Server.turnStart", threadID)
	if err != nil {
		return "", err
	}
	proc, err := EnsureThreadReadyForTurn(a, ctx, threadID, cwd)
	if err != nil {
		return "", err
	}
	logger.Info("turn/start: thread dispatch resolved",
		append(a.ThreadLogFields(threadID),
			logger.FieldPort, proc.Port(),
			"codex_thread_id", a.GetThreadID(proc),
		)...,
	)
	submitStart := time.Now()
	if err := a.Submit(proc, submitPrompt, images, files, outputSchema); err != nil {
		return "", a.WrapError(err, "Server.turnStart", "submit prompt")
	}
	logger.Info("turn/start: submit returned",
		append(a.ThreadLogFields(threadID),
			"submit_ms", time.Since(submitStart).Milliseconds(),
		)...,
	)

	resolvedTurnID := a.ResolveClientActiveTurnID(proc)
	if resolvedTurnID == "" {
		logger.Warn("turn/start: active turn id unavailable after submit; tracker will use synthetic id",
			a.ThreadLogFields(threadID)...,
		)
	}
	turnID := a.BeginTrackedTurn(threadID, resolvedTurnID)
	logger.Info("turn/start: tracker registered",
		append(a.ThreadLogFields(threadID),
			"turn_id", turnID,
			"tracker_setup_ms", time.Since(submitStart).Milliseconds(),
		)...,
	)
	return turnID, nil
}

func ResolveProcess(a RuntimeAdapter, caller, threadID string) (Process, error) {
	id, err := a.RequireThreadID(caller, threadID)
	if err != nil {
		return nil, err
	}
	manager := a.Manager()
	if manager == nil {
		return nil, a.NewError(caller, "thread resolver is not configured")
	}
	proc := manager.Get(id)
	if proc == nil {
		return nil, a.NewErrorf(caller, "thread %s not found", id)
	}
	return proc, nil
}

func WithProcess[T any](
	a RuntimeAdapter,
	caller string,
	threadID string,
	fn func(Process) (T, error),
) (T, error) {
	var zero T
	proc, err := ResolveProcess(a, caller, threadID)
	if err != nil {
		return zero, err
	}
	return fn(proc)
}
