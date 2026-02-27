package runtime

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type AutoMatchInput = agentcore.AutoMatchInput
type SkillMatchCandidate = agentcore.SkillMatchCandidate
type AutoMatchedSkillMatch = agentcore.AutoMatchedSkillMatch
type AutoSkillMatchOptions = agentcore.AutoSkillMatchOptions
type TurnInput = agentcore.TurnInput
type TurnStartRequest = agentcore.TurnStartRequest
type TurnSteerRequest = agentcore.TurnSteerRequest
type TurnStartEntryResult = agentcore.TurnStartEntryResult
type TurnAppendUserTimelineOptions = agentcore.TurnAppendUserTimelineOptions
type TurnSteerEntryPrepareResult = agentcore.TurnSteerEntryPrepareResult
type TimelineAttachment = agentcore.TimelineAttachment
type TimelineItem = agentcore.TimelineItem
type TimelineRuntime = agentcore.TimelineRuntime
type Binding = agentcore.Binding
type BindingStore = agentcore.BindingStore
type Process = agentcore.Process
type Manager = agentcore.Manager

type (
	ResumeThreadRequest struct {
		ThreadID string
		Cwd      string
	}
	TurnStartPreparedSubmission struct {
		Prompt                string
		SubmitPrompt          string
		Images                []string
		Files                 []string
		TimelineAttachments   []TimelineAttachment
		SelectedSkillCount    int
		AutoMatchedSkillCount int
	}
	ParsedTurnInputs struct {
		Prompt              string
		Images              []string
		Files               []string
		TimelineAttachments []TimelineAttachment
	}
	PreparedSubmissionCommon struct {
		Parsed                ParsedTurnInputs
		SubmitPrompt          string
		SelectedSkillCount    int
		AutoMatchedSkillCount int
	}
)

const maxLSPUsagePromptHintLen = 16000

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
	fillNilFuncs(&a, defaultPrepareAdapter)
	return a
}
func normalizeRuntimeAdapter(a RuntimeAdapter) RuntimeAdapter {
	a.PrepareAdapter = normalizePrepareAdapter(a.PrepareAdapter)
	fillNilFuncs(&a, defaultRuntimeAdapter)
	return a
}

func fillNilFuncs(dst any, defaults any) {
	dstValue := reflect.ValueOf(dst)
	if dstValue.Kind() != reflect.Ptr || dstValue.Elem().Kind() != reflect.Struct {
		return
	}
	target := dstValue.Elem()
	def := reflect.ValueOf(defaults)
	if def.Kind() == reflect.Ptr {
		def = def.Elem()
	}
	if def.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < target.NumField() && i < def.NumField(); i++ {
		field := target.Field(i)
		if field.Kind() != reflect.Func || !field.IsNil() {
			continue
		}
		defField := def.Field(i)
		if defField.Kind() == reflect.Func && !defField.IsNil() {
			field.Set(defField)
		}
	}
}

func withThreadLogFields(a RuntimeAdapter, threadID string, kv ...any) []any {
	return append(a.ThreadLogFields(threadID), kv...)
}

var defaultPrepareAdapter = PrepareAdapter{
	MergePromptText: func(prompt, extra string) string {
		trimmedExtra := strings.TrimSpace(extra)
		if trimmedExtra == "" {
			return prompt
		}
		trimmedPrompt := strings.TrimSpace(prompt)
		if trimmedPrompt == "" {
			return extra
		}
		return prompt + "\n" + extra
	},
	FileContentInputText: func(name, content string) string {
		trimmedContent := strings.TrimSpace(content)
		if trimmedContent == "" {
			return ""
		}
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			return trimmedContent
		}
		return "[file:" + trimmedName + "]\n" + trimmedContent
	},
	BuildAttachmentName:          func(path string) string { return strings.TrimSpace(path) },
	BuildAttachmentPreviewURL:    func(path string) string { return strings.TrimSpace(path) },
	BuildSelectedSkillPrompt:     func([]string) (string, int) { return "", 0 },
	ListSkillMatchCandidates:     func() ([]SkillMatchCandidate, error) { return nil, nil },
	ListAgentSkills:              func(string) []string { return nil },
	RenderAutoMatchedSkillPrompt: func(string, []AutoMatchedSkillMatch) (string, int) { return "", 0 },
	CollectAutoMatchedSkillMatches: func(string, []AutoMatchInput, []string, []SkillMatchCandidate, AutoSkillMatchOptions) []AutoMatchedSkillMatch {
		return nil
	},
	ActiveTrackedTurnID: func(string) (string, bool) { return "", false },
	RequireThreadID: func(caller, threadID string) (string, error) {
		id := strings.TrimSpace(threadID)
		if id == "" {
			return "", appErrors.New(caller, "threadId is required")
		}
		return id, nil
	},
	NewError:                  appErrors.New,
	NewErrorf:                 appErrors.Newf,
	ShowInjectedPromptInChat:  func(context.Context) bool { return false },
	ResolveLSPUsagePromptHint: func(_ context.Context, defaultHint string, _ int) string { return defaultHint },
	DefaultLSPUsagePromptHint: func() string { return "" },
	MaxLSPUsagePromptHintLen:  func() int { return maxLSPUsagePromptHintLen },
	UIRuntime:                 func() TimelineRuntime { return nil },
}

var defaultRuntimeAdapter = RuntimeAdapter{
	Manager:                           func() Manager { return nil },
	ThreadExistsInHistory:             func(context.Context, string) bool { return false },
	AllDynamicToolSchemas:             func() []agentcore.DynamicTool { return nil },
	ResolveStartInstructionsForLaunch: func(context.Context, []agentcore.DynamicTool) string { return "" },
	SetAgentWorkDir:                   func(string, string) {},
	ThreadLogFields: func(threadID string) []any {
		id := strings.TrimSpace(threadID)
		return []any{logger.FieldAgentID, id, logger.FieldThreadID, id}
	},
	GetThreadID:                  func(Process) string { return "" },
	CancelCodeRuns:               func(string) int { return 0 },
	BindingStore:                 func() BindingStore { return nil },
	ResolveCodexThreadCandidates: func(context.Context, string) []string { return nil },
	ResumeThread: func(Process, ResumeThreadRequest) error {
		return appErrors.New("Server.ensureThreadReady", "thread process is not available")
	},
	IsCodexProcessCrashError:         func(error) bool { return false },
	IsHistoricalResumeCandidateError: func(error) bool { return false },
	PreviewResumeCandidates:          func(candidates []string, _ int) []string { return append([]string(nil), candidates...) },
	Notify:                           func(string, any) {},
	NormalizeSkillNames:              func(input []string) ([]string, error) { return append([]string(nil), input...), nil },
	WrapError:                        appErrors.Wrap,
	WrapErrorf:                       appErrors.Wrapf,
	ResolveClientActiveTurnID:        func(Process) string { return "" },
	BeginTrackedTurn:                 func(_ string, resolvedTurnID string) string { return strings.TrimSpace(resolvedTurnID) },
	TurnSteer: func(string, string, []string, []string) (map[string]any, error) {
		return nil, appErrors.New("Server.turnSteer", "turn steer is not configured")
	},
	Submit: func(Process, string, []string, []string, json.RawMessage) error {
		return appErrors.New("Server.turnStart", "thread process is not available")
	},
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
		withThreadLogFields(a, id,
			"has_history", hasHistory, logger.FieldCwd, launchCwd,
			"candidate_count", len(resumeCandidates), "candidates", a.PreviewResumeCandidates(resumeCandidates, 4),
		)...,
	)

	dynamicTools := a.AllDynamicToolSchemas()
	startInstructions := a.ResolveStartInstructionsForLaunch(ctx, dynamicTools)

	proc, err := EnsureReadyLaunchProcess(a, ctx, manager, id, launchCwd, startInstructions, dynamicTools)
	if err != nil {
		return nil, err
	}
	if len(resumeCandidates) == 0 {
		logger.Warn("turn/start: no valid historical codex thread id, continue with fresh session", a.ThreadLogFields(id)...)
		return proc, nil
	}

	resumed, lastResumeErr, fatalResumeErr := TryResumeHistoricalCandidates(a, ctx, manager, proc, id, launchCwd, resumeCandidates)
	if fatalResumeErr != nil {
		return nil, fatalResumeErr
	}
	if resumed {
		return proc, nil
	}

	if lastResumeErr != nil {
		return EnsureReadyResumeFallback(a, ctx, manager, id, launchCwd, proc, lastResumeErr, startInstructions, dynamicTools, len(resumeCandidates))
	}

	logger.Warn("turn/start: no available historical rollout, continue with fresh session",
		withThreadLogFields(a, id, "candidate_count", len(resumeCandidates), logger.FieldCwd, launchCwd)...,
	)
	RegisterBinding(a, ctx, id, proc)
	return proc, nil
}

func EnsureReadyRunningProcess(a RuntimeAdapter, ctx context.Context, manager Manager, agentID string, launchCwd string) (Process, bool) {
	a = normalizeRuntimeAdapter(a)
	if manager == nil {
		return nil, false
	}
	proc := manager.Get(agentID)
	if proc == nil {
		return nil, false
	}
	// Lazy Recovery: 检测到死进程 (连接断开 / StateError 等)，
	// 清理后返回 false，让调用者走 launch + resume 恢复路径。
	if !proc.IsAlive() {
		logger.Warn("turn/start: dead process detected, stopping for auto-recovery",
			withThreadLogFields(a, agentID, logger.FieldPort, proc.Port())...,
		)
		a.CancelCodeRuns(agentID)
		_ = manager.Stop(agentID)
		return nil, false
	}
	logger.Info("turn/start: using running process",
		withThreadLogFields(a, agentID, logger.FieldPort, proc.Port(), "codex_thread_id", a.GetThreadID(proc))...,
	)
	a.SetAgentWorkDir(agentID, launchCwd)
	RegisterBinding(a, ctx, agentID, proc)
	return proc, true
}

func EnsureReadyLaunchProcess(a RuntimeAdapter, ctx context.Context, manager Manager, agentID string, launchCwd string, startInstructions string, dynamicTools []agentcore.DynamicTool) (Process, error) {
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
		withThreadLogFields(a, agentID, logger.FieldPort, proc.Port(), "codex_thread_id_before_resume", a.GetThreadID(proc))...,
	)
	return proc, nil
}

func EnsureReadyResumeFallback(a RuntimeAdapter, ctx context.Context, manager Manager, agentID string, launchCwd string, proc Process, lastResumeErr error, startInstructions string, dynamicTools []agentcore.DynamicTool, candidateCount int) (Process, error) {
	a = normalizeRuntimeAdapter(a)
	logger.Warn("turn/start: all resume candidates exhausted, fallback to fresh session",
		withThreadLogFields(a, agentID, "candidate_count", candidateCount, "last_error", lastResumeErr, logger.FieldCwd, launchCwd)...,
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
	RegisterBinding(a, ctx, agentID, proc)
	return proc, nil
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
			withThreadLogFields(a, agentID, "codex_thread_id", codexThreadID, logger.FieldError, err)...,
		)
	}
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
				withThreadLogFields(a, id, "bound_codex_thread_id", binding.CodexThreadID)...,
			)
		}
	}
	if len(resumeCandidates) == 0 {
		resumeCandidates = append(resumeCandidates, a.ResolveCodexThreadCandidates(ctx, id)...)
	}
	return resumeCandidates
}

func TryResumeHistoricalCandidates(a RuntimeAdapter, ctx context.Context, manager Manager, proc Process, agentID string, launchCwd string, resumeCandidates []string) (resumed bool, lastResumeErr error, fatalErr error) {
	a = normalizeRuntimeAdapter(a)
	id := strings.TrimSpace(agentID)
	if id == "" || proc == nil {
		return false, nil, nil
	}
	for _, resumeThreadID := range resumeCandidates {
		err := a.ResumeThread(proc, ResumeThreadRequest{ThreadID: resumeThreadID, Cwd: launchCwd})
		if err == nil {
			logger.Info("turn/start: historical thread auto-loaded",
				withThreadLogFields(a, id, "resume_thread_id", resumeThreadID, "codex_thread_id_after_resume", a.GetThreadID(proc), logger.FieldCwd, launchCwd)...,
			)
			RegisterBinding(a, ctx, id, proc)
			return true, nil, nil
		}

		lastResumeErr = err
		if a.IsCodexProcessCrashError(err) {
			logger.Error("turn/start: codex crashed during resume, returning error",
				withThreadLogFields(a, id, "resume_thread_id", resumeThreadID, logger.FieldError, err)...,
			)
			a.CancelCodeRuns(id)
			if manager != nil {
				_ = manager.Stop(id)
			}
			return false, lastResumeErr, a.WrapErrorf(err, "Server.ensureThreadReady", "codex crashed while resuming thread %s (rollout=%s)", id, resumeThreadID)
		}

		if a.IsHistoricalResumeCandidateError(err) {
			logger.Warn("turn/start: resume candidate unavailable, try next",
				withThreadLogFields(a, id, "resume_thread_id", resumeThreadID, logger.FieldError, err)...,
			)
			continue
		}

		logger.Error("turn/start: unrecognized resume error",
			withThreadLogFields(a, id, "resume_thread_id", resumeThreadID, logger.FieldError, err)...,
		)
		return false, lastResumeErr, a.WrapErrorf(err, "Server.ensureThreadReady", "resume failed for thread %s (rollout=%s)", id, resumeThreadID)
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
		withThreadLogFields(a, threadID, logger.FieldCwd, strings.TrimSpace(req.Cwd), "input_count", len(req.Input), "selected_skills_count", len(req.SelectedSkills))...,
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
		withThreadLogFields(a, threadID,
			"text_len", len(prepared.Prompt), "images", len(prepared.Images), "files", len(prepared.Files),
			"selected_skills_requested", len(selectedSkills), "selected_skills_injected", prepared.SelectedSkillCount,
			"manual_skill_selection", req.ManualSkillSelection, "auto_matched_skills", prepared.AutoMatchedSkillCount,
		)...,
	)

	turnID, err := StartTurnSubmissionAndTrack(a, ctx, threadID, req.Cwd, prepared.SubmitPrompt, prepared.Images, prepared.Files, req.OutputSchema)
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

func StartTurnSubmissionAndTrack(a RuntimeAdapter, ctx context.Context, threadID string, cwd string, submitPrompt string, images []string, files []string, outputSchema json.RawMessage) (string, error) {
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
		withThreadLogFields(a, threadID, logger.FieldPort, proc.Port(), "codex_thread_id", a.GetThreadID(proc))...,
	)
	submitStart := time.Now()
	if err := a.Submit(proc, submitPrompt, images, files, outputSchema); err != nil {
		return "", a.WrapError(err, "Server.turnStart", "submit prompt")
	}
	logger.Info("turn/start: submit returned",
		withThreadLogFields(a, threadID, "submit_ms", time.Since(submitStart).Milliseconds())...,
	)

	resolvedTurnID := a.ResolveClientActiveTurnID(proc)
	if resolvedTurnID == "" {
		logger.Warn("turn/start: active turn id unavailable after submit; tracker will use synthetic id",
			a.ThreadLogFields(threadID)...,
		)
	}
	turnID := a.BeginTrackedTurn(threadID, resolvedTurnID)
	logger.Info("turn/start: tracker registered",
		withThreadLogFields(a, threadID, "turn_id", turnID, "tracker_setup_ms", time.Since(submitStart).Milliseconds())...,
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

func WithProcess[T any](a RuntimeAdapter, caller string, threadID string, fn func(Process) (T, error)) (T, error) {
	a = normalizeRuntimeAdapter(a)
	var zero T
	proc, err := ResolveProcess(a, caller, threadID)
	if err != nil {
		return zero, err
	}
	return fn(proc)
}
