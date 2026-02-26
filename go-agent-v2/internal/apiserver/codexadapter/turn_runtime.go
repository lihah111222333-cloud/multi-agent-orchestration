package codexadapter

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ensureThreadReadyForTurn 负责拉起/恢复线程进程，并处理历史会话丢失降级。
func (a *Adapter) ensureThreadReadyForTurn(ctx context.Context, threadID, cwd string) (*runner.AgentProcess, error) {
	// D11: 总超时 45s，避免 Launch(30s)+Resume(30s) 串行导致前端 turn/start 永不回。
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	manager := a.manager()
	if manager == nil {
		return nil, apperrors.New("Server.ensureThreadReady", "thread manager is not initialized")
	}
	id, err := requireThreadID("Server.ensureThreadReady", threadID)
	if err != nil {
		return nil, err
	}
	launchCwd := strings.TrimSpace(cwd)
	if launchCwd == "" {
		launchCwd = "."
	}

	if proc, ok := a.ensureReadyRunningProcess(ctx, manager, id, launchCwd); ok {
		return proc, nil
	}

	hasHistory := a.ThreadExistsInHistory(ctx, id)
	if !hasHistory {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s not found", id)
	}
	resumeCandidates := a.collectResumeCandidates(ctx, id)

	logger.Info("turn/start: restoring historical thread",
		append(threadLogFields(id),
			"has_history", hasHistory,
			logger.FieldCwd, launchCwd,
			"candidate_count", len(resumeCandidates),
			"candidates", PreviewResumeCandidates(resumeCandidates, 4),
		)...,
	)

	dynamicTools := a.allDynamicToolSchemas()
	startInstructions := a.resolveStartInstructionsForLaunch(ctx, dynamicTools)

	proc, err := a.ensureReadyLaunchProcess(ctx, manager, id, launchCwd, startInstructions, dynamicTools)
	if err != nil {
		return nil, err
	}
	if len(resumeCandidates) == 0 {
		return a.ensureReadyNoResumeCandidates(id, proc), nil
	}

	resumed, lastResumeErr, fatalResumeErr := a.tryResumeHistoricalCandidates(ctx, manager, proc, id, launchCwd, resumeCandidates)
	if fatalResumeErr != nil {
		return nil, fatalResumeErr
	}
	if resumed {
		return proc, nil
	}

	if lastResumeErr != nil {
		return a.ensureReadyResumeFallback(
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

	return a.ensureReadyNoHistoricalRollout(ctx, id, launchCwd, proc, len(resumeCandidates)), nil
}

func (a *Adapter) ensureReadyRunningProcess(
	ctx context.Context,
	manager *runner.AgentManager,
	agentID string,
	launchCwd string,
) (*runner.AgentProcess, bool) {
	if manager == nil {
		return nil, false
	}
	proc := manager.Get(agentID)
	if proc == nil {
		return nil, false
	}
	logger.Info("turn/start: using running process",
		append(threadLogFields(agentID),
			logger.FieldPort, proc.Client.GetPort(),
			"codex_thread_id", a.GetThreadID(proc),
		)...,
	)
	a.setAgentWorkDir(agentID, launchCwd)
	a.registerBinding(ctx, agentID, proc)
	return proc, true
}

func (a *Adapter) ensureReadyLaunchProcess(
	ctx context.Context,
	manager *runner.AgentManager,
	agentID string,
	launchCwd string,
	startInstructions string,
	dynamicTools []agentcore.DynamicTool,
) (*runner.AgentProcess, error) {
	if manager == nil {
		return nil, apperrors.New("Server.ensureThreadReady", "thread manager is not initialized")
	}
	if err := manager.Launch(ctx, agentID, agentID, "", launchCwd, startInstructions, dynamicTools); err != nil {
		// 并发补加载时可能已被其他请求拉起，二次确认后再报错。
		if proc := manager.Get(agentID); proc != nil {
			a.setAgentWorkDir(agentID, launchCwd)
			return proc, nil
		}
		return nil, apperrors.Wrapf(err, "Server.ensureThreadReady", "auto-load thread %s", agentID)
	}

	proc := manager.Get(agentID)
	if proc == nil {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s loaded but not found", agentID)
	}
	a.setAgentWorkDir(agentID, launchCwd)
	logger.Info("turn/start: process launched for restore",
		append(threadLogFields(agentID),
			logger.FieldPort, proc.Client.GetPort(),
			"codex_thread_id_before_resume", a.GetThreadID(proc),
		)...,
	)
	return proc, nil
}

func (a *Adapter) ensureReadyNoResumeCandidates(
	agentID string,
	proc *runner.AgentProcess,
) *runner.AgentProcess {
	logger.Warn("turn/start: no valid historical codex thread id, continue with fresh session",
		threadLogFields(agentID)...,
	)
	if proc != nil {
		proc.MarkSessionLost()
	}
	return proc
}

func (a *Adapter) ensureReadyResumeFallback(
	ctx context.Context,
	manager *runner.AgentManager,
	agentID string,
	launchCwd string,
	proc *runner.AgentProcess,
	lastResumeErr error,
	startInstructions string,
	dynamicTools []agentcore.DynamicTool,
	candidateCount int,
) (*runner.AgentProcess, error) {
	logger.Warn("turn/start: all resume candidates exhausted, fallback to fresh session",
		append(threadLogFields(agentID),
			"candidate_count", candidateCount,
			"last_error", lastResumeErr,
			logger.FieldCwd, launchCwd,
		)...,
	)
	// proc 在 non-crash 路径中仍然存活，但可能被 mgr 移除。
	if manager != nil && manager.Get(agentID) == nil {
		_ = a.cancelCodeRuns(agentID)
		_ = manager.Stop(agentID)
		if launchErr := manager.Launch(ctx, agentID, agentID, "", launchCwd, startInstructions, dynamicTools); launchErr != nil {
			return nil, apperrors.Wrapf(launchErr, "Server.ensureThreadReady", "final re-spawn thread %s", agentID)
		}
		proc = manager.Get(agentID)
		if proc == nil {
			return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s final re-spawn failed", agentID)
		}
	}
	if proc != nil {
		proc.MarkSessionLost()
	}
	a.notifySessionLost(agentID, lastResumeErr)
	a.registerBinding(ctx, agentID, proc)
	return proc, nil
}

func (a *Adapter) ensureReadyNoHistoricalRollout(
	ctx context.Context,
	agentID string,
	launchCwd string,
	proc *runner.AgentProcess,
	candidateCount int,
) *runner.AgentProcess {
	logger.Warn("turn/start: no available historical rollout, continue with fresh session",
		append(threadLogFields(agentID),
			"candidate_count", candidateCount,
			logger.FieldCwd, launchCwd,
		)...,
	)
	if proc != nil {
		proc.MarkSessionLost()
	}
	a.registerBinding(ctx, agentID, proc)
	return proc
}

func (a *Adapter) registerBinding(ctx context.Context, agentID string, proc *runner.AgentProcess) {
	bindingStore := a.bindingStore()
	if bindingStore == nil || proc == nil || proc.Client == nil {
		return
	}
	codexThreadID := a.GetThreadID(proc)
	if codexThreadID == "" {
		return
	}
	if err := bindingStore.Bind(ctx, agentID, codexThreadID, ""); err != nil {
		logger.Warn("turn/start: failed to register binding",
			append(threadLogFields(agentID),
				"codex_thread_id", codexThreadID,
				logger.FieldError, err,
			)...,
		)
	}
}

func (a *Adapter) notifySessionLost(agentID string, lastErr error) {
	method, payload := BuildSessionLostNotification(agentID, lastErr)
	a.notify(method, payload)
}

// BuildSessionLostNotification builds "session lost" fallback notification payload.
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

func (a *Adapter) collectResumeCandidates(ctx context.Context, agentID string) []string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil
	}
	resumeCandidates := make([]string, 0, 4)
	if bindingStore := a.bindingStore(); bindingStore != nil {
		if binding, err := bindingStore.FindByAgentID(ctx, id); err == nil && binding != nil {
			resumeCandidates = append(resumeCandidates, binding.CodexThreadID)
			logger.Info("turn/start: found DB binding",
				append(threadLogFields(id),
					"bound_codex_thread_id", binding.CodexThreadID,
				)...,
			)
		}
	}
	if len(resumeCandidates) == 0 {
		resumeCandidates = append(resumeCandidates, a.ResolveCodexThreadCandidates(ctx, id, appendUniqueThreadIDFallback, PreviewResumeCandidates)...)
	}
	return resumeCandidates
}

func (a *Adapter) tryResumeHistoricalCandidates(
	ctx context.Context,
	manager *runner.AgentManager,
	proc *runner.AgentProcess,
	agentID string,
	launchCwd string,
	resumeCandidates []string,
) (resumed bool, lastResumeErr error, fatalErr error) {
	id := strings.TrimSpace(agentID)
	if id == "" || proc == nil {
		return false, nil, nil
	}
	for _, resumeThreadID := range resumeCandidates {
		err := a.ResumeThread(proc, agentcore.ResumeThreadRequest{
			ThreadID: resumeThreadID,
			Cwd:      launchCwd,
		})
		if err == nil {
			logger.Info("turn/start: historical thread auto-loaded",
				append(threadLogFields(id),
					"resume_thread_id", resumeThreadID,
					"codex_thread_id_after_resume", a.GetThreadID(proc),
					logger.FieldCwd, launchCwd,
				)...,
			)
			a.registerBinding(ctx, id, proc)
			return true, nil, nil
		}

		lastResumeErr = err
		if IsCodexProcessCrashError(err) {
			logger.Error("turn/start: codex crashed during resume, returning error",
				append(threadLogFields(id),
					"resume_thread_id", resumeThreadID,
					logger.FieldError, err,
				)...,
			)
			_ = a.cancelCodeRuns(id)
			if manager != nil {
				_ = manager.Stop(id)
			}
			a.notifySessionLost(id, err)
			return false, lastResumeErr, apperrors.Wrapf(err, "Server.ensureThreadReady",
				"codex crashed while resuming thread %s (rollout=%s)", id, resumeThreadID)
		}

		if IsHistoricalResumeCandidateError(err) {
			logger.Warn("turn/start: resume candidate unavailable, try next",
				append(threadLogFields(id),
					"resume_thread_id", resumeThreadID,
					logger.FieldError, err,
				)...,
			)
			continue
		}

		logger.Error("turn/start: unrecognized resume error",
			append(threadLogFields(id),
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)...,
		)
		return false, lastResumeErr, apperrors.Wrapf(err, "Server.ensureThreadReady",
			"resume failed for thread %s (rollout=%s)", id, resumeThreadID)
	}
	return false, lastResumeErr, nil
}

// turnStartRequest carries protocol params for turn/start.
type turnStartRequest = contracts.TurnStartRequest

// turnSteerRequest carries protocol params for turn/steer.
type turnSteerRequest = contracts.TurnSteerRequest

type turnStartEntryResult struct {
	TurnID string
}

// TurnStart handles turn/start with constructor-time dependencies.
func (a *Adapter) TurnStart(ctx context.Context, req turnStartRequest) (turnStartEntryResult, error) {
	threadID, err := requireThreadID("Server.turnStart", req.ThreadID)
	if err != nil {
		return turnStartEntryResult{}, err
	}
	logger.Info("turn/start: request received",
		append(threadLogFields(threadID),
			logger.FieldCwd, strings.TrimSpace(req.Cwd),
			"input_count", len(req.Input),
			"selected_skills_count", len(req.SelectedSkills),
		)...,
	)
	selectedSkills, err := commonadapter.NormalizeSkillNames(req.SelectedSkills)
	if err != nil {
		return turnStartEntryResult{}, apperrors.Wrap(err, "Server.turnStart", "normalize selected skills")
	}
	prepared, err := a.prepareTurnStartSubmission(threadID, req.Input, selectedSkills, req.ManualSkillSelection)
	if err != nil {
		return turnStartEntryResult{}, err
	}
	logger.Info("turn/start: input prepared",
		append(threadLogFields(threadID),
			"text_len", len(prepared.Prompt),
			"images", len(prepared.Images),
			"files", len(prepared.Files),
			"selected_skills_requested", len(selectedSkills),
			"selected_skills_injected", prepared.SelectedSkillCount,
			"manual_skill_selection", req.ManualSkillSelection,
			"auto_matched_skills", prepared.AutoMatchedSkillCount,
		)...,
	)

	turnID, err := a.startTurnSubmissionAndTrack(
		ctx,
		threadID,
		req.Cwd,
		prepared.SubmitPrompt,
		prepared.Images,
		prepared.Files,
		req.OutputSchema,
	)
	if err != nil {
		return turnStartEntryResult{}, err
	}
	a.appendTurnStartUserTimeline(ctx, prepared.TimelineAttachments, contracts.TurnAppendUserTimelineOptions{
		ThreadID:     threadID,
		Prompt:       prepared.Prompt,
		SubmitPrompt: prepared.SubmitPrompt,
		Images:       prepared.Images,
		Files:        prepared.Files,
	})
	return turnStartEntryResult{TurnID: turnID}, nil
}

// TurnSteerFromInput handles turn/steer with constructor-time dependencies.
func (a *Adapter) TurnSteerFromInput(req turnSteerRequest) (map[string]any, error) {
	threadID, err := requireThreadID("Server.turnSteer", req.ThreadID)
	if err != nil {
		return nil, err
	}
	selectedSkills, err := commonadapter.NormalizeSkillNames(req.SelectedSkills)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.turnSteer", "normalize selected skills")
	}
	prepared, err := a.prepareTurnSteerSubmission(threadID, req.Input, selectedSkills, req.ManualSkillSelection)
	if err != nil {
		return nil, err
	}
	return a.TurnSteer(threadID, prepared.SubmitPrompt, prepared.Images, prepared.Files)
}

// StartTurnSubmissionAndTrack handles submit and turn tracker bootstrap.
func (a *Adapter) startTurnSubmissionAndTrack(
	ctx context.Context,
	threadID string,
	cwd string,
	submitPrompt string,
	images []string,
	files []string,
	outputSchema json.RawMessage,
) (string, error) {
	threadID, err := requireThreadID("Server.turnStart", threadID)
	if err != nil {
		return "", err
	}
	proc, err := a.ensureThreadReadyForTurn(ctx, threadID, cwd)
	if err != nil {
		return "", err
	}
	logger.Info("turn/start: thread dispatch resolved",
		append(threadLogFields(threadID),
			logger.FieldPort, proc.Client.GetPort(),
			"codex_thread_id", a.GetThreadID(proc),
		)...,
	)
	submitStart := time.Now()
	if err := a.Submit(proc, submitPrompt, images, files, outputSchema); err != nil {
		return "", apperrors.Wrap(err, "Server.turnStart", "submit prompt")
	}
	logger.Info("turn/start: submit returned",
		append(threadLogFields(threadID),
			"submit_ms", time.Since(submitStart).Milliseconds(),
		)...,
	)

	resolvedTurnID := resolveClientActiveTurnID(proc.Client)
	if resolvedTurnID == "" {
		logger.Warn("turn/start: active turn id unavailable after submit; tracker will use synthetic id",
			threadLogFields(threadID)...,
		)
	}
	turnID := a.beginTrackedTurn(threadID, resolvedTurnID)
	logger.Info("turn/start: tracker registered",
		append(threadLogFields(threadID),
			"turn_id", turnID,
			"tracker_setup_ms", time.Since(submitStart).Milliseconds(),
		)...,
	)
	return turnID, nil
}

func (a *Adapter) resolveProcess(caller, threadID string) (*runner.AgentProcess, error) {
	id, err := requireThreadID(caller, threadID)
	if err != nil {
		return nil, err
	}
	manager := a.manager()
	if manager == nil {
		return nil, apperrors.New(caller, "thread resolver is not configured")
	}
	proc := manager.Get(id)
	if proc == nil {
		return nil, apperrors.Newf(caller, "thread %s not found", id)
	}
	return proc, nil
}

func withProcess[T any](
	a *Adapter,
	caller string,
	threadID string,
	fn func(*runner.AgentProcess) (T, error),
) (T, error) {
	var zero T
	proc, err := a.resolveProcess(caller, threadID)
	if err != nil {
		return zero, err
	}
	return fn(proc)
}
