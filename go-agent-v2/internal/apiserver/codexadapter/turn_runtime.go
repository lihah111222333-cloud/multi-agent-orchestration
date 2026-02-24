package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type EnsureThreadReadyOptions struct {
	ThreadID string
	Cwd      string

	Manager      *runner.AgentManager
	BindingStore *store.AgentCodexBindingStore

	ThreadExistsInHistory             func(context.Context, string) bool
	ResolveCodexThreadCandidates      func(context.Context, string) []string
	BuildAllDynamicTools              func() []agentcore.DynamicTool
	ResolveStartInstructionsForLaunch func(context.Context, []agentcore.DynamicTool) string
	SetAgentWorkDir                   func(string, string)
	RegisterBinding                   func(context.Context, string, *runner.AgentProcess)
	CancelCodeRuns                    func(string) int
	BroadcastNotification             func(string, map[string]any)
	BuildSessionLostNotification      func(string, error) (string, map[string]any)
}

// EnsureThreadReadyForTurn 负责拉起/恢复线程进程，并处理历史会话丢失降级。
func (a *Adapter) EnsureThreadReadyForTurn(ctx context.Context, opt EnsureThreadReadyOptions) (*runner.AgentProcess, error) {
	// D11: 总超时 45s，避免 Launch(30s)+Resume(30s) 串行导致前端 turn/start 永不回。
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	id := strings.TrimSpace(opt.ThreadID)
	if id == "" {
		return nil, apperrors.New("Server.ensureThreadReady", "threadId is required")
	}
	if opt.Manager == nil {
		return nil, apperrors.New("Server.ensureThreadReady", "thread manager is not initialized")
	}
	launchCwd := strings.TrimSpace(opt.Cwd)
	if launchCwd == "" {
		launchCwd = "."
	}

	if proc := opt.Manager.Get(id); proc != nil {
		logger.Info("turn/start: using running process",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			logger.FieldPort, proc.Client.GetPort(),
			"codex_thread_id", a.GetThreadID(proc),
		)
		if opt.SetAgentWorkDir != nil {
			opt.SetAgentWorkDir(id, launchCwd)
		}
		if opt.RegisterBinding != nil {
			opt.RegisterBinding(ctx, id, proc)
		}
		return proc, nil
	}

	hasHistory := false
	if opt.ThreadExistsInHistory != nil {
		hasHistory = opt.ThreadExistsInHistory(ctx, id)
	}
	if !hasHistory {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s not found", id)
	}
	resumeCandidates := make([]string, 0, 4)

	// 优先从 agent_codex_binding 表获取绑定的 codexThreadId。
	if opt.BindingStore != nil {
		if binding, err := opt.BindingStore.FindByAgentID(ctx, id); err == nil && binding != nil {
			resumeCandidates = append(resumeCandidates, binding.CodexThreadID)
			logger.Info("turn/start: found DB binding",
				logger.FieldAgentID, id,
				"bound_codex_thread_id", binding.CodexThreadID,
			)
		}
	}

	if len(resumeCandidates) == 0 && opt.ResolveCodexThreadCandidates != nil {
		resumeCandidates = append(resumeCandidates, opt.ResolveCodexThreadCandidates(ctx, id)...)
	}

	logger.Info("turn/start: restoring historical thread",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"has_history", hasHistory,
		logger.FieldCwd, launchCwd,
		"candidate_count", len(resumeCandidates),
		"candidates", PreviewResumeCandidates(resumeCandidates, 4),
	)

	dynamicTools := []agentcore.DynamicTool{}
	if opt.BuildAllDynamicTools != nil {
		dynamicTools = opt.BuildAllDynamicTools()
	}
	startInstructions := ""
	if opt.ResolveStartInstructionsForLaunch != nil {
		startInstructions = opt.ResolveStartInstructionsForLaunch(ctx, dynamicTools)
	}

	if err := opt.Manager.Launch(ctx, id, id, "", launchCwd, startInstructions, dynamicTools); err != nil {
		// 并发补加载时可能已被其他请求拉起，二次确认后再报错。
		if proc := opt.Manager.Get(id); proc != nil {
			if opt.SetAgentWorkDir != nil {
				opt.SetAgentWorkDir(id, launchCwd)
			}
			return proc, nil
		}
		return nil, apperrors.Wrapf(err, "Server.ensureThreadReady", "auto-load thread %s", id)
	}

	proc := opt.Manager.Get(id)
	if proc == nil {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s loaded but not found", id)
	}
	if opt.SetAgentWorkDir != nil {
		opt.SetAgentWorkDir(id, launchCwd)
	}
	logger.Info("turn/start: process launched for restore",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		logger.FieldPort, proc.Client.GetPort(),
		"codex_thread_id_before_resume", a.GetThreadID(proc),
	)
	if len(resumeCandidates) == 0 {
		logger.Warn("turn/start: no valid historical codex thread id, continue with fresh session",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
		)
		proc.MarkSessionLost()
		return proc, nil
	}

	var lastResumeErr error
	for _, resumeThreadID := range resumeCandidates {
		err := a.ResumeThread(proc, agentcore.ResumeThreadRequest{
			ThreadID: resumeThreadID,
			Cwd:      launchCwd,
		})
		if err == nil {
			logger.Info("turn/start: historical thread auto-loaded",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				"codex_thread_id_after_resume", a.GetThreadID(proc),
				logger.FieldCwd, launchCwd,
			)
			if opt.RegisterBinding != nil {
				opt.RegisterBinding(ctx, id, proc)
			}
			return proc, nil
		}

		lastResumeErr = err
		if IsCodexProcessCrashError(err) {
			logger.Error("turn/start: codex crashed during resume, returning error",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)
			if opt.CancelCodeRuns != nil {
				_ = opt.CancelCodeRuns(id)
			}
			_ = opt.Manager.Stop(id)
			if opt.BuildSessionLostNotification != nil && opt.BroadcastNotification != nil {
				method, payload := opt.BuildSessionLostNotification(id, err)
				opt.BroadcastNotification(method, payload)
			}
			return nil, apperrors.Wrapf(err, "Server.ensureThreadReady",
				"codex crashed while resuming thread %s (rollout=%s)", id, resumeThreadID)
		}

		if IsHistoricalResumeCandidateError(err) {
			logger.Warn("turn/start: resume candidate unavailable, try next",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)
			continue
		}

		logger.Error("turn/start: unrecognized resume error",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			"resume_thread_id", resumeThreadID,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrapf(err, "Server.ensureThreadReady",
			"resume failed for thread %s (rollout=%s)", id, resumeThreadID)
	}

	// 所有候选的 rollout 都不可用 (非 crash) → fallback 到 fresh session + 通知前端。
	if lastResumeErr != nil {
		logger.Warn("turn/start: all resume candidates exhausted, fallback to fresh session",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			"candidate_count", len(resumeCandidates),
			"last_error", lastResumeErr,
			logger.FieldCwd, launchCwd,
		)
		// proc 在 non-crash 路径中仍然存活，但可能被 mgr 移除。
		if opt.Manager.Get(id) == nil {
			if opt.CancelCodeRuns != nil {
				_ = opt.CancelCodeRuns(id)
			}
			_ = opt.Manager.Stop(id)
			if launchErr := opt.Manager.Launch(ctx, id, id, "", launchCwd, startInstructions, dynamicTools); launchErr != nil {
				return nil, apperrors.Wrapf(launchErr, "Server.ensureThreadReady", "final re-spawn thread %s", id)
			}
			proc = opt.Manager.Get(id)
			if proc == nil {
				return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s final re-spawn failed", id)
			}
		}
		proc.MarkSessionLost()
		if opt.BuildSessionLostNotification != nil && opt.BroadcastNotification != nil {
			method, payload := opt.BuildSessionLostNotification(id, lastResumeErr)
			opt.BroadcastNotification(method, payload)
		}
		if opt.RegisterBinding != nil {
			opt.RegisterBinding(ctx, id, proc)
		}
		return proc, nil
	}

	logger.Warn("turn/start: no available historical rollout, continue with fresh session",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"candidate_count", len(resumeCandidates),
		logger.FieldCwd, launchCwd,
	)
	proc.MarkSessionLost()
	if opt.RegisterBinding != nil {
		opt.RegisterBinding(ctx, id, proc)
	}
	return proc, nil
}

type StartTurnSubmissionOptions struct {
	ThreadID     string
	Cwd          string
	SubmitPrompt string
	Images       []string
	Files        []string
	OutputSchema json.RawMessage

	EnsureThreadReady    func(context.Context, string, string) (*runner.AgentProcess, error)
	HasActiveTrackedTurn func(string) bool
	ResolveActiveTurnID  func(agentcore.Client) string
	BeginTrackedTurn     func(string, string) string
}

type StartTurnSubmissionResult struct {
	Process *runner.AgentProcess
	TurnID  string
}

// StartTurnSubmissionAndTrack 负责 submit 与 turn tracking 主流程。
func (a *Adapter) StartTurnSubmissionAndTrack(ctx context.Context, opt StartTurnSubmissionOptions) (StartTurnSubmissionResult, error) {
	proc, err := opt.EnsureThreadReady(ctx, opt.ThreadID, opt.Cwd)
	if err != nil {
		return StartTurnSubmissionResult{}, err
	}
	logger.Info("turn/start: thread dispatch resolved",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		logger.FieldPort, proc.Client.GetPort(),
		"codex_thread_id", a.GetThreadID(proc),
	)
	submitStart := time.Now()
	logger.Warn("DIAG: turn/start: about to Submit (events may arrive before tracker setup)",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		logger.FieldPort, proc.Client.GetPort(),
		"has_active_tracked_turn", opt.HasActiveTrackedTurn(opt.ThreadID),
	)
	if err := a.Submit(proc, opt.SubmitPrompt, opt.Images, opt.Files, opt.OutputSchema); err != nil {
		return StartTurnSubmissionResult{}, apperrors.Wrap(err, "Server.turnStart", "submit prompt")
	}
	submitElapsed := time.Since(submitStart)
	logger.Warn("DIAG: turn/start: Submit returned",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		"submit_ms", submitElapsed.Milliseconds(),
		"has_active_tracked_turn", opt.HasActiveTrackedTurn(opt.ThreadID),
	)

	resolvedTurnID := opt.ResolveActiveTurnID(proc.Client)
	if resolvedTurnID == "" {
		logger.Warn("turn/start: active turn id unavailable after submit; tracker will use synthetic id",
			logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		)
	}
	logger.Warn("DIAG: turn/start: about to beginTrackedTurn",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		"resolved_turn_id", resolvedTurnID,
		"gap_since_submit_ms", time.Since(submitStart).Milliseconds(),
		"has_active_tracked_turn", opt.HasActiveTrackedTurn(opt.ThreadID),
	)
	turnID := opt.BeginTrackedTurn(opt.ThreadID, resolvedTurnID)
	logger.Warn("DIAG: turn/start: beginTrackedTurn completed",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		"turn_id", turnID,
		"total_gap_ms", time.Since(submitStart).Milliseconds(),
	)
	return StartTurnSubmissionResult{Process: proc, TurnID: turnID}, nil
}

// ResolveClientActiveTurnID extracts active turn ID if client supports it.
func ResolveClientActiveTurnID(client agentcore.Client) string {
	if client == nil {
		return ""
	}
	reader, ok := client.(interface{ GetActiveTurnID() string })
	if !ok {
		return ""
	}
	return strings.TrimSpace(reader.GetActiveTurnID())
}

// NormalizeInterruptState normalizes runtime status names used by interrupt flow.
func NormalizeInterruptState(raw string) string {
	state := strings.ToLower(strings.TrimSpace(raw))
	if state == "" {
		return "idle"
	}
	switch state {
	case "completed", "complete", "done", "success", "succeeded", "ready", "stopped", "ended", "closed":
		return "idle"
	case "failed", "fail":
		return "error"
	default:
		return state
	}
}

// IsInterruptNoActiveTurnError reports whether interrupt failure means no active turn.
func IsInterruptNoActiveTurnError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no active turn") ||
		strings.Contains(message, "nothing to interrupt") ||
		strings.Contains(message, "not interruptible")
}

// IsInterruptActiveState reports whether current state is still active.
func IsInterruptActiveState(state string) bool {
	return isInterruptActiveState(state)
}

// InterruptSettleMode classifies interrupt settle outcome.
func InterruptSettleMode(confirmed bool, afterState string) string {
	if confirmed {
		return "interrupt_confirmed"
	}
	switch NormalizeInterruptState(afterState) {
	case "error":
		return "interrupt_terminal_failed"
	case "idle":
		return "interrupt_terminal_completed"
	default:
		return "interrupt_timeout"
	}
}

// ReadThreadRuntimeState returns normalized thread state via injected runtime/status hooks.
func ReadThreadRuntimeState(threadID string, readRuntimeStatus func(string) string, hasActiveTrackedTurn func(string) bool) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	if readRuntimeStatus == nil {
		if hasActiveTrackedTurn != nil && hasActiveTrackedTurn(id) {
			return "running"
		}
		return ""
	}
	state := NormalizeInterruptState(readRuntimeStatus(id))
	if state == "idle" && hasActiveTrackedTurn != nil && hasActiveTrackedTurn(id) {
		return "running"
	}
	return state
}

// WaitInterruptOutcome waits until interrupt settles based on tracker/runtime state.
func WaitInterruptOutcome(
	threadID string,
	timeout time.Duration,
	activeHint bool,
	waitTrackedTurnTerminal func(string, time.Duration) (string, bool),
	readThreadRuntimeState func(string) string,
) (bool, string, int64, bool) {
	start := time.Now()
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false, "idle", 0, false
	}
	observedActive := activeHint
	if waitTrackedTurnTerminal != nil {
		if terminalStatus, ok := waitTrackedTurnTerminal(id, timeout); ok {
			afterState := NormalizeInterruptState(terminalStatus)
			confirmed := strings.EqualFold(terminalStatus, "interrupted")
			return confirmed, afterState, time.Since(start).Milliseconds(), true
		}
	}
	if readThreadRuntimeState == nil {
		return false, "", time.Since(start).Milliseconds(), observedActive
	}
	deadline := start.Add(timeout)
	lastState := readThreadRuntimeState(id)
	if isInterruptActiveState(lastState) {
		observedActive = true
	}
	for {
		if !isInterruptActiveState(lastState) {
			if !observedActive {
				return false, lastState, time.Since(start).Milliseconds(), false
			}
			return true, lastState, time.Since(start).Milliseconds(), true
		}
		observedActive = true
		if time.Now().After(deadline) {
			return false, lastState, time.Since(start).Milliseconds(), true
		}
		time.Sleep(120 * time.Millisecond)
		lastState = readThreadRuntimeState(id)
	}
}

type TurnInterruptOptions struct {
	ThreadID  string
	ParamsLen int

	ReadThreadRuntimeState            func(string) string
	HasActiveTrackedTurn              func(string) bool
	CancelCodeRuns                    func(string) int
	WithThread                        func(string, func(*runner.AgentProcess) (any, error)) (any, error)
	CompleteTrackedTurn               func(string, string, string) (map[string]any, bool)
	Notify                            func(string, any)
	MarkTrackedTurnInterruptRequested func(string) bool
	WaitInterruptOutcome              func(string, time.Duration, bool) (bool, string, int64, bool)
	IsInterruptNoActiveTurnError      func(error) bool
	InterruptSettleMode               func(bool, string) string
}

// TurnInterrupt 执行 /interrupt 并等待状态收敛。
func (a *Adapter) TurnInterrupt(opt TurnInterruptOptions) (any, error) {
	start := time.Now()
	beforeState := opt.ReadThreadRuntimeState(opt.ThreadID)
	activeTrackedBefore := opt.HasActiveTrackedTurn(opt.ThreadID)
	activeBefore := isInterruptActiveState(beforeState)
	logger.Info("turn/interrupt: request",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		logger.FieldParamsLen, opt.ParamsLen,
		"state_before", beforeState,
		"active_before", activeBefore,
		"active_tracked_before", activeTrackedBefore,
	)
	if cancelled := opt.CancelCodeRuns(opt.ThreadID); cancelled > 0 {
		logger.Info("turn/interrupt: cancelled running code_run executions",
			logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
			"cancelled_runs", cancelled,
		)
	}
	return opt.WithThread(opt.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := a.SendCommand(proc, "/interrupt", ""); err != nil {
			if opt.IsInterruptNoActiveTurnError(err) {
				if activeBefore || activeTrackedBefore {
					if completion, ok := opt.CompleteTrackedTurn(opt.ThreadID, "completed", "interrupt_no_active_turn"); ok {
						opt.Notify("turn/completed", completion)
					} else {
						opt.Notify("turn/completed", map[string]any{
							"threadId": opt.ThreadID,
							"status":   "completed",
							"reason":   "interrupt_no_active_turn",
						})
					}
				}
				logger.Info("turn/interrupt: no active turn",
					logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
					"state_before", beforeState,
					logger.FieldDurationMS, time.Since(start).Milliseconds(),
				)
				return map[string]any{
					"confirmed":     false,
					"mode":          "no_active_turn",
					"interruptSent": false,
					"stateBefore":   beforeState,
					"stateAfter":    beforeState,
				}, nil
			}
			logger.Warn("turn/interrupt: send command failed",
				logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
				logger.FieldError, err,
				logger.FieldDurationMS, time.Since(start).Milliseconds(),
			)
			return nil, err
		}
		logger.Info("turn/interrupt: command sent",
			logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		opt.MarkTrackedTurnInterruptRequested(opt.ThreadID)
		confirmed, afterState, waitedMS, observedActive := opt.WaitInterruptOutcome(
			opt.ThreadID,
			6*time.Second,
			activeBefore || activeTrackedBefore,
		)
		mode := opt.InterruptSettleMode(confirmed, afterState)
		if !observedActive {
			confirmed = false
			mode = "no_active_turn"
		}
		logger.Info("turn/interrupt: settle",
			logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
			"confirmed", confirmed,
			"mode", mode,
			"active_observed", observedActive,
			"state_before", beforeState,
			"state_after", afterState,
			"waited_ms", waitedMS,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return map[string]any{
			"confirmed":      confirmed,
			"mode":           mode,
			"interruptSent":  true,
			"stateBefore":    beforeState,
			"stateAfter":     afterState,
			"waitedMs":       waitedMS,
			"activeObserved": observedActive,
		}, nil
	})
}

// TurnForceCompleteOptions carries forced completion dependencies.
type TurnForceCompleteOptions struct {
	ThreadID string

	CancelCodeRuns               func(string) int
	WithThread                   func(string, func(*runner.AgentProcess) (any, error)) (any, error)
	IsInterruptNoActiveTurnError func(error) bool
	CompleteTrackedTurn          func(string, string, string) (map[string]any, bool)
	Notify                       func(string, any)
}

// TurnForceComplete forcibly finalizes current turn state (best effort interrupt + tracker cleanup).
func (a *Adapter) TurnForceComplete(opt TurnForceCompleteOptions) (any, error) {
	logger.Info("turn/forceComplete: request",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
	)
	if opt.CancelCodeRuns != nil {
		if cancelled := opt.CancelCodeRuns(opt.ThreadID); cancelled > 0 {
			logger.Info("turn/forceComplete: cancelled running code_run executions",
				logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
				"cancelled_runs", cancelled,
			)
		}
	}
	if opt.WithThread == nil {
		return nil, apperrors.New("Server.turnForceComplete", "thread resolver is not configured")
	}
	return opt.WithThread(opt.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := a.SendCommand(proc, "/interrupt", ""); err != nil {
			noActiveTurn := IsInterruptNoActiveTurnError(err)
			if opt.IsInterruptNoActiveTurnError != nil {
				noActiveTurn = opt.IsInterruptNoActiveTurnError(err)
			}
			if noActiveTurn {
				logger.Info("turn/forceComplete: no active turn (best-effort)",
					logger.FieldAgentID, opt.ThreadID)
			} else {
				logger.Warn("turn/forceComplete: interrupt failed (best-effort)",
					logger.FieldAgentID, opt.ThreadID, logger.FieldError, err)
			}
		}

		if opt.CompleteTrackedTurn != nil && opt.Notify != nil {
			if completion, ok := opt.CompleteTrackedTurn(opt.ThreadID, "completed", "force_complete"); ok {
				opt.Notify("turn/completed", completion)
			} else {
				opt.Notify("turn/completed", map[string]any{
					"threadId": opt.ThreadID,
					"status":   "completed",
					"reason":   "force_complete",
				})
			}
		}

		return map[string]any{
			"confirmed":      true,
			"forceCompleted": true,
		}, nil
	})
}

// BuildResumeCandidates builds ordered resume candidates from thread id and resolved ids.
func BuildResumeCandidates(threadID string, resolved []string, normalize func(string) string) []string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	if normalize != nil {
		if normalized := normalize(id); normalized != "" {
			return []string{normalized}
		}
	}
	candidates := make([]string, 0, len(resolved))
	seen := map[string]struct{}{}
	for _, candidate := range resolved {
		value := strings.TrimSpace(candidate)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}
	if len(candidates) > 0 {
		return candidates
	}
	return []string{id}
}

// TryResumeCandidates attempts each candidate in order and returns first success.
func TryResumeCandidates(
	candidates []string,
	fallbackID string,
	resumeFn func(string) error,
	isCandidateError func(error) bool,
) (string, error) {
	if len(candidates) == 0 {
		logger.Warn("thread/resume: no resume candidates available",
			logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
			"reason", "no codex thread ID resolved from history",
		)
		return "", apperrors.Newf("tryResumeCandidates", "no resume candidates available for thread %s", fallbackID)
	}
	if isCandidateError == nil {
		isCandidateError = IsHistoricalResumeCandidateError
	}

	var lastErr error
	for _, id := range candidates {
		err := resumeFn(id)
		if err == nil {
			return id, nil
		}
		lastErr = err
		if isCandidateError(err) {
			logger.Warn("thread/resume: candidate unavailable, trying next",
				logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
				"resume_thread_id", id,
				logger.FieldError, err,
			)
			continue
		}
		return "", err
	}

	logger.Warn("thread/resume: all resume candidates exhausted",
		logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
		"candidate_count", len(candidates),
		"last_error", lastErr,
		"reason", "all historical rollouts unavailable",
	)
	if lastErr != nil {
		return "", apperrors.Wrapf(lastErr, "tryResumeCandidates", "all resume candidates unavailable for thread %s after %d attempts", fallbackID, len(candidates))
	}
	return "", apperrors.Newf("tryResumeCandidates", "all resume candidates unavailable for thread %s after %d attempts", fallbackID, len(candidates))
}

// IsHistoricalResumeCandidateError determines whether error means a candidate can be skipped.
func IsHistoricalResumeCandidateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no rollout found for thread id"):
		return true
	case strings.Contains(msg, "failed to load rollout"):
		return true
	case strings.Contains(msg, "thread/resume returned empty thread id"):
		return true
	case strings.Contains(msg, "thread/resume returned empty response without fallback thread id"):
		return true
	case strings.Contains(msg, "websocket: close 1006"):
		return true
	case strings.Contains(msg, "abnormal closure"):
		return true
	case strings.Contains(msg, "history not found"):
		return true
	case strings.Contains(msg, "already at oldest turn"):
		return true
	case strings.Contains(msg, "rollout file missing"):
		return true
	case strings.Contains(msg, "session file not found"):
		return true
	case strings.Contains(msg, "invalid thread id"):
		return true
	default:
		return false
	}
}

// IsCodexProcessCrashError determines whether error indicates codex process crash.
func IsCodexProcessCrashError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "websocket: close 1006") ||
		strings.Contains(msg, "abnormal closure")
}

// PreviewResumeCandidates returns a shortened candidate preview for logs.
func PreviewResumeCandidates(candidates []string, limit int) []string {
	if len(candidates) == 0 {
		return nil
	}
	if limit <= 0 || len(candidates) <= limit {
		return append([]string(nil), candidates...)
	}
	out := append([]string(nil), candidates[:limit]...)
	out = append(out, fmt.Sprintf("...+%d more", len(candidates)-limit))
	return out
}

func isInterruptActiveState(state string) bool {
	s := NormalizeInterruptState(state)
	switch s {
	case "inprogress", "in_progress", "running", "streaming", "thinking", "starting", "responding", "editing", "waiting", "syncing":
		return true
	default:
		return false
	}
}
