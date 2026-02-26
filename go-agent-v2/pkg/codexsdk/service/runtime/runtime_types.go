package runtime

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
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

// PrepareAdapter provides dependencies for prepare-core logic.
type PrepareAdapter interface {
	MergePromptText(left, right string) string
	FileContentInputText(name, content string) string

	BuildSelectedSkillPrompt(selectedSkills []string) (string, int)
	ListSkillMatchCandidates() ([]SkillMatchCandidate, error)
	ListAgentSkills(agentID string) []string
	CollectAutoMatchedSkillMatches(
		prompt string,
		inputs []AutoMatchInput,
		configuredSkillNames []string,
		candidates []SkillMatchCandidate,
		options AutoSkillMatchOptions,
	) []AutoMatchedSkillMatch
	RenderAutoMatchedSkillPrompt(agentID string, matches []AutoMatchedSkillMatch) (string, int)

	ActiveTrackedTurnID(threadID string) (string, bool)
	RequireThreadID(caller, threadID string) (string, error)
	NewError(caller, message string) error
	NewErrorf(caller, format string, args ...any) error

	ShowInjectedPromptInChat(ctx context.Context) bool
	ResolveLSPUsagePromptHint(ctx context.Context, defaultHint string, maxHintLen int) string
	DefaultLSPUsagePromptHint() string
	MaxLSPUsagePromptHintLen() int
	UIRuntime() TimelineRuntime
}

// RuntimeAdapter provides dependencies for turn-runtime logic.
type RuntimeAdapter interface {
	PrepareAdapter

	Manager() Manager
	ThreadExistsInHistory(ctx context.Context, threadID string) bool
	AllDynamicToolSchemas() []agentcore.DynamicTool
	ResolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []agentcore.DynamicTool) string
	SetAgentWorkDir(agentID, cwd string)
	ThreadLogFields(threadID string) []any
	GetThreadID(proc Process) string
	CancelCodeRuns(agentID string) int
	BindingStore() BindingStore
	ResolveCodexThreadCandidates(ctx context.Context, agentID string) []string
	ResumeThread(proc Process, req ResumeThreadRequest) error
	IsCodexProcessCrashError(err error) bool
	IsHistoricalResumeCandidateError(err error) bool
	PreviewResumeCandidates(candidates []string, limit int) []string
	Notify(method string, payload any)
	NormalizeSkillNames(input []string) ([]string, error)
	WrapError(err error, caller, message string) error
	WrapErrorf(err error, caller, format string, args ...any) error

	Submit(proc Process, prompt string, images, files []string, outputSchema json.RawMessage) error
	ResolveClientActiveTurnID(proc Process) string
	BeginTrackedTurn(threadID, resolvedTurnID string) string
	TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error)
}
