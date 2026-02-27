package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// AutoMatchInput carries user input metadata used for skill auto-match.
type AutoMatchInput struct {
	Type string
	Name string
}

// SkillMatchCandidate describes one skill candidate for auto-match classification.
type SkillMatchCandidate struct {
	Name         string
	ForceWords   []string
	TriggerWords []string
}

// AutoMatchedSkillMatch stores one matched skill classification result.
type AutoMatchedSkillMatch struct {
	Name         string
	MatchedBy    string
	MatchedTerms []string
}

// AutoSkillMatchOptions controls how configured skills participate in auto-match.
type AutoSkillMatchOptions struct {
	IncludeConfiguredExplicit bool
	IncludeConfiguredForce    bool
}

// TurnInput is a protocol-level user input item for turn/start and turn/steer.
type TurnInput struct {
	Type    string
	Text    string
	URL     string
	Path    string
	Name    string
	Content string
}

// TurnStartRequest carries protocol params for turn/start.
type TurnStartRequest struct {
	ThreadID             string
	Cwd                  string
	Input                []TurnInput
	SelectedSkills       []string
	ManualSkillSelection bool
	OutputSchema         json.RawMessage
}

// TurnSteerRequest carries protocol params for turn/steer.
type TurnSteerRequest struct {
	ThreadID             string
	ExpectedTurnID       string
	Input                []TurnInput
	SelectedSkills       []string
	ManualSkillSelection bool
}

// TurnStartEntryResult carries response payload for turn/start.
type TurnStartEntryResult struct {
	TurnID string
}

// TurnAppendUserTimelineOptions configures turn/start user timeline rendering.
type TurnAppendUserTimelineOptions struct {
	ThreadID     string
	Prompt       string
	SubmitPrompt string
	Images       []string
	Files        []string
}

// TurnSteerEntryPrepareResult contains prepared submit payload for turn/steer.
type TurnSteerEntryPrepareResult struct {
	SubmitPrompt string
	Images       []string
	Files        []string
}

// TurnStartPreparedSubmission contains prepared submit payload for turn/start.
type TurnStartPreparedSubmission struct {
	Prompt                string
	SubmitPrompt          string
	Images                []string
	Files                 []string
	TimelineAttachments   []TimelineAttachment
	SelectedSkillCount    int
	AutoMatchedSkillCount int
}

// ParsedTurnInputs is normalized turn input breakdown.
type ParsedTurnInputs struct {
	Prompt              string
	Images              []string
	Files               []string
	TimelineAttachments []TimelineAttachment
}

// PreparedSubmissionCommon holds shared prepared fields for start/steer.
type PreparedSubmissionCommon struct {
	Parsed                ParsedTurnInputs
	SubmitPrompt          string
	SelectedSkillCount    int
	AutoMatchedSkillCount int
}

// TimelineAttachment is a lightweight timeline attachment reference.
type TimelineAttachment struct {
	Kind       string
	Name       string
	Path       string
	PreviewURL string
}

// TimelineItem is the minimal thread timeline item view needed by runtime logic.
type TimelineItem struct {
	Kind string
	Text string
}

// TimelineRuntime abstracts UI runtime timeline operations.
type TimelineRuntime interface {
	AppendUserMessage(threadID, text string, attachments []TimelineAttachment)
	ThreadTimeline(threadID string) []TimelineItem
}

// Binding is a lightweight agent/thread binding payload.
type Binding struct {
	CodexThreadID string
}

// BindingStore abstracts binding persistence operations.
type BindingStore interface {
	Bind(ctx context.Context, agentID, codexThreadID, sessionID string) error
	FindByAgentID(ctx context.Context, agentID string) (*Binding, error)
}

// Process is a runtime process abstraction used by service logic.
type Process interface {
	Port() int
	MarkSessionLost()
}

// Manager is the process manager abstraction used by service logic.
type Manager interface {
	Get(agentID string) Process
	Launch(ctx context.Context, agentID, alias, profile, cwd, startInstructions string, dynamicTools []agentcore.DynamicTool) error
	Stop(agentID string) error
}

// ResumeThreadRequest carries resume params for process-level resume.
type ResumeThreadRequest struct {
	ThreadID string
	Cwd      string
}

const (
	defaultLSPUsagePromptHint = ""
	maxLSPUsagePromptHintLen  = 16000
)

// PrepareAdapter provides dependencies for prepare-core logic.
type PrepareAdapter struct {
	MergePromptText           func(left, right string) string
	FileContentInputText      func(name, content string) string
	BuildAttachmentName       func(path string) string
	BuildAttachmentPreviewURL func(path string) string

	BuildSelectedSkillPrompt       func(selectedSkills []string) (string, int)
	ListSkillMatchCandidates       func() ([]SkillMatchCandidate, error)
	ListAgentSkills                func(agentID string) []string
	CollectAutoMatchedSkillMatches func(
		prompt string,
		inputs []AutoMatchInput,
		configuredSkillNames []string,
		candidates []SkillMatchCandidate,
		options AutoSkillMatchOptions,
	) []AutoMatchedSkillMatch
	RenderAutoMatchedSkillPrompt func(agentID string, matches []AutoMatchedSkillMatch) (string, int)

	ActiveTrackedTurnID func(threadID string) (string, bool)
	RequireThreadID     func(caller, threadID string) (string, error)
	NewError            func(caller, message string) error
	NewErrorf           func(caller, format string, args ...any) error

	ShowInjectedPromptInChat  func(ctx context.Context) bool
	ResolveLSPUsagePromptHint func(ctx context.Context, defaultHint string, maxHintLen int) string
	DefaultLSPUsagePromptHint func() string
	MaxLSPUsagePromptHintLen  func() int
	UIRuntime                 func() TimelineRuntime
}

// RuntimeAdapter provides dependencies for turn-runtime logic.
type RuntimeAdapter struct {
	PrepareAdapter

	Manager                           func() Manager
	ThreadExistsInHistory             func(ctx context.Context, threadID string) bool
	AllDynamicToolSchemas             func() []agentcore.DynamicTool
	ResolveStartInstructionsForLaunch func(ctx context.Context, dynamicTools []agentcore.DynamicTool) string
	SetAgentWorkDir                   func(agentID, cwd string)
	ThreadLogFields                   func(threadID string) []any
	GetThreadID                       func(proc Process) string
	CancelCodeRuns                    func(agentID string) int
	BindingStore                      func() BindingStore
	ResolveCodexThreadCandidates      func(ctx context.Context, agentID string) []string
	ResumeThread                      func(proc Process, req ResumeThreadRequest) error
	IsCodexProcessCrashError          func(err error) bool
	IsHistoricalResumeCandidateError  func(err error) bool
	PreviewResumeCandidates           func(candidates []string, limit int) []string
	Notify                            func(method string, payload any)
	NormalizeSkillNames               func(input []string) ([]string, error)
	WrapError                         func(err error, caller, message string) error
	WrapErrorf                        func(err error, caller, format string, args ...any) error

	Submit                    func(proc Process, prompt string, images, files []string, outputSchema json.RawMessage) error
	ResolveClientActiveTurnID func(proc Process) string
	BeginTrackedTurn          func(threadID, resolvedTurnID string) string
	TurnSteer                 func(threadID, submitPrompt string, images, files []string) (map[string]any, error)
}

func normalizePrepareAdapter(a PrepareAdapter) PrepareAdapter {
	if a.MergePromptText == nil {
		a.MergePromptText = func(prompt, extra string) string {
			trimmedExtra := strings.TrimSpace(extra)
			if trimmedExtra == "" {
				return prompt
			}
			trimmedPrompt := strings.TrimSpace(prompt)
			if trimmedPrompt == "" {
				return extra
			}
			return prompt + "\n" + extra
		}
	}
	if a.FileContentInputText == nil {
		a.FileContentInputText = func(name, content string) string {
			trimmedContent := strings.TrimSpace(content)
			if trimmedContent == "" {
				return ""
			}
			trimmedName := strings.TrimSpace(name)
			if trimmedName == "" {
				return trimmedContent
			}
			return "[file:" + trimmedName + "]\n" + trimmedContent
		}
	}
	if a.BuildAttachmentName == nil {
		a.BuildAttachmentName = func(path string) string { return strings.TrimSpace(path) }
	}
	if a.BuildAttachmentPreviewURL == nil {
		a.BuildAttachmentPreviewURL = func(path string) string { return strings.TrimSpace(path) }
	}
	if a.BuildSelectedSkillPrompt == nil {
		a.BuildSelectedSkillPrompt = func([]string) (string, int) { return "", 0 }
	}
	if a.ListSkillMatchCandidates == nil {
		a.ListSkillMatchCandidates = func() ([]SkillMatchCandidate, error) { return nil, nil }
	}
	if a.ListAgentSkills == nil {
		a.ListAgentSkills = func(string) []string { return nil }
	}
	if a.CollectAutoMatchedSkillMatches == nil {
		a.CollectAutoMatchedSkillMatches = func(string, []AutoMatchInput, []string, []SkillMatchCandidate, AutoSkillMatchOptions) []AutoMatchedSkillMatch {
			return nil
		}
	}
	if a.RenderAutoMatchedSkillPrompt == nil {
		a.RenderAutoMatchedSkillPrompt = func(string, []AutoMatchedSkillMatch) (string, int) { return "", 0 }
	}
	if a.ActiveTrackedTurnID == nil {
		a.ActiveTrackedTurnID = func(string) (string, bool) { return "", false }
	}
	if a.RequireThreadID == nil {
		a.RequireThreadID = func(caller, threadID string) (string, error) {
			id := strings.TrimSpace(threadID)
			if id == "" {
				return "", appErrors.New(caller, "threadId is required")
			}
			return id, nil
		}
	}
	if a.NewError == nil {
		a.NewError = appErrors.New
	}
	if a.NewErrorf == nil {
		a.NewErrorf = appErrors.Newf
	}
	if a.ShowInjectedPromptInChat == nil {
		a.ShowInjectedPromptInChat = func(context.Context) bool { return false }
	}
	if a.ResolveLSPUsagePromptHint == nil {
		a.ResolveLSPUsagePromptHint = func(_ context.Context, defaultHint string, _ int) string { return defaultHint }
	}
	if a.DefaultLSPUsagePromptHint == nil {
		a.DefaultLSPUsagePromptHint = func() string { return defaultLSPUsagePromptHint }
	}
	if a.MaxLSPUsagePromptHintLen == nil {
		a.MaxLSPUsagePromptHintLen = func() int { return maxLSPUsagePromptHintLen }
	}
	if a.UIRuntime == nil {
		a.UIRuntime = func() TimelineRuntime { return nil }
	}
	return a
}

func normalizeRuntimeAdapter(a RuntimeAdapter) RuntimeAdapter {
	a.PrepareAdapter = normalizePrepareAdapter(a.PrepareAdapter)

	if a.Manager == nil {
		a.Manager = func() Manager { return nil }
	}
	if a.ThreadExistsInHistory == nil {
		a.ThreadExistsInHistory = func(context.Context, string) bool { return false }
	}
	if a.AllDynamicToolSchemas == nil {
		a.AllDynamicToolSchemas = func() []agentcore.DynamicTool { return nil }
	}
	if a.ResolveStartInstructionsForLaunch == nil {
		a.ResolveStartInstructionsForLaunch = func(context.Context, []agentcore.DynamicTool) string { return "" }
	}
	if a.SetAgentWorkDir == nil {
		a.SetAgentWorkDir = func(string, string) {}
	}
	if a.ThreadLogFields == nil {
		a.ThreadLogFields = func(threadID string) []any {
			id := strings.TrimSpace(threadID)
			return []any{
				logger.FieldAgentID, id,
				logger.FieldThreadID, id,
			}
		}
	}
	if a.GetThreadID == nil {
		a.GetThreadID = func(Process) string { return "" }
	}
	if a.CancelCodeRuns == nil {
		a.CancelCodeRuns = func(string) int { return 0 }
	}
	if a.BindingStore == nil {
		a.BindingStore = func() BindingStore { return nil }
	}
	if a.ResolveCodexThreadCandidates == nil {
		a.ResolveCodexThreadCandidates = func(context.Context, string) []string { return nil }
	}
	if a.ResumeThread == nil {
		a.ResumeThread = func(Process, ResumeThreadRequest) error {
			return appErrors.New("Server.ensureThreadReady", "thread process is not available")
		}
	}
	if a.IsCodexProcessCrashError == nil {
		a.IsCodexProcessCrashError = func(error) bool { return false }
	}
	if a.IsHistoricalResumeCandidateError == nil {
		a.IsHistoricalResumeCandidateError = func(error) bool { return false }
	}
	if a.PreviewResumeCandidates == nil {
		a.PreviewResumeCandidates = func(candidates []string, _ int) []string {
			return append([]string(nil), candidates...)
		}
	}
	if a.Notify == nil {
		a.Notify = func(string, any) {}
	}
	if a.NormalizeSkillNames == nil {
		a.NormalizeSkillNames = func(input []string) ([]string, error) {
			return append([]string(nil), input...), nil
		}
	}
	if a.WrapError == nil {
		a.WrapError = appErrors.Wrap
	}
	if a.WrapErrorf == nil {
		a.WrapErrorf = appErrors.Wrapf
	}
	if a.Submit == nil {
		a.Submit = func(Process, string, []string, []string, json.RawMessage) error {
			return appErrors.New("Server.turnStart", "thread process is not available")
		}
	}
	if a.ResolveClientActiveTurnID == nil {
		a.ResolveClientActiveTurnID = func(Process) string { return "" }
	}
	if a.BeginTrackedTurn == nil {
		a.BeginTrackedTurn = func(_ string, resolvedTurnID string) string { return strings.TrimSpace(resolvedTurnID) }
	}
	if a.TurnSteer == nil {
		a.TurnSteer = func(string, string, []string, []string) (map[string]any, error) {
			return nil, appErrors.New("Server.turnSteer", "turn steer is not configured")
		}
	}
	return a
}

func EnsureThreadReadyForTurn(a RuntimeAdapter, ctx context.Context, threadID, cwd string) (Process, error) {
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
	method, payload := BuildSessionLostNotification(agentID, lastErr)
	a.Notify(method, payload)
}

func CollectResumeCandidates(a RuntimeAdapter, ctx context.Context, agentID string) []string {
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
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
	prepared, err := PrepareTurnStartSubmission(a.PrepareAdapter, threadID, req.Input, selectedSkills, req.ManualSkillSelection)
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
	AppendTurnStartUserTimeline(a.PrepareAdapter, ctx, prepared.TimelineAttachments, TurnAppendUserTimelineOptions{
		ThreadID:     threadID,
		Prompt:       prepared.Prompt,
		SubmitPrompt: prepared.SubmitPrompt,
		Images:       prepared.Images,
		Files:        prepared.Files,
	})
	return TurnStartEntryResult{TurnID: turnID}, nil
}

func TurnSteerFromInput(a RuntimeAdapter, req TurnSteerRequest) (map[string]any, error) {
	a = normalizeRuntimeAdapter(a)
	threadID, err := a.RequireThreadID("Server.turnSteer", req.ThreadID)
	if err != nil {
		return nil, err
	}
	selectedSkills, err := a.NormalizeSkillNames(req.SelectedSkills)
	if err != nil {
		return nil, a.WrapError(err, "Server.turnSteer", "normalize selected skills")
	}
	prepared, err := PrepareTurnSteerSubmission(a.PrepareAdapter, threadID, req.Input, selectedSkills, req.ManualSkillSelection)
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
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
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
	a = normalizeRuntimeAdapter(a)
	var zero T
	proc, err := ResolveProcess(a, caller, threadID)
	if err != nil {
		return zero, err
	}
	return fn(proc)
}
