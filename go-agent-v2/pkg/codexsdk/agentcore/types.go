package agentcore

import (
	"context"
	"encoding/json"
)

// ────────────────────────────────────────────────────
// Shared DTO types (canonical definitions)
// Used by contracts, service/*, and consumer/*
// ────────────────────────────────────────────────────

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

// TurnAppendUserTimelineOptions configures turn/start user timeline rendering.
type TurnAppendUserTimelineOptions struct {
	ThreadID     string
	Prompt       string
	SubmitPrompt string
	Images       []string
	Files        []string
}

// TurnStartEntryPrepareResult contains prepared submit payload for turn/start.
type TurnStartEntryPrepareResult struct {
	Prompt                string
	SubmitPrompt          string
	Images                []string
	Files                 []string
	SelectedSkillCount    int
	AutoMatchedSkillCount int
}

// TurnSteerEntryPrepareResult contains prepared submit payload for turn/steer.
type TurnSteerEntryPrepareResult struct {
	SubmitPrompt string
	Images       []string
	Files        []string
}

// TurnStartEntryResult carries response payload for turn/start.
type TurnStartEntryResult struct {
	TurnID string
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

// Binding is a lightweight agent/thread binding payload.
type Binding struct {
	CodexThreadID string
}

// ThreadListItem models one thread list payload entry.
type ThreadListItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// ────────────────────────────────────────────────────
// Shared interfaces
// ────────────────────────────────────────────────────

// TimelineRuntime abstracts UI runtime timeline operations.
type TimelineRuntime interface {
	AppendUserMessage(threadID, text string, attachments []TimelineAttachment)
	ThreadTimeline(threadID string) []TimelineItem
}

// Process is a runtime process abstraction used by service logic.
type Process interface {
	Port() int
	IsAlive() bool
}

// Manager is the process manager abstraction used by service logic.
type Manager interface {
	Get(agentID string) Process
	Launch(ctx context.Context, agentID, alias, profile, cwd, startInstructions string, dynamicTools []DynamicTool) error
	Stop(agentID string) error
}

// BindingStore abstracts binding persistence operations.
type BindingStore interface {
	Bind(ctx context.Context, agentID, codexThreadID, sessionID string) error
	FindByAgentID(ctx context.Context, agentID string) (*Binding, error)
}

// Event is the CLI-agnostic event envelope.
type Event struct {
	Type         string          `json:"type"`
	Data         json.RawMessage `json:"data,omitempty"`
	RequestID    *int64          `json:"-"`
	RequestIDRaw json.RawMessage `json:"-"`

	// Keep callback signatures unchanged in Phase 1.
	RespondFunc       func(code int, message string) error `json:"-"`
	RespondResultFunc func(result any) error               `json:"-"`
	DenyFunc          func() error                         `json:"-"`
}

// TextData is shared by message/reasoning/exec deltas.
type TextData struct {
	Delta   string `json:"delta,omitempty"`
	Content string `json:"content,omitempty"`
	Role    string `json:"role,omitempty"`
}

// ErrorData is a fatal error payload.
type ErrorData struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// WarningData is a non-fatal warning payload.
type WarningData struct {
	Message string `json:"message"`
}

// TokenCountData carries token usage stats.
type TokenCountData struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// SessionConfiguredData is emitted after session setup.
type SessionConfiguredData struct {
	ThreadID string `json:"thread_id,omitempty"`
}

// ExecApprovalRequestData requests command execution approval.
type ExecApprovalRequestData struct {
	Command string `json:"command,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// ExecCommandBeginData is emitted when command execution starts.
type ExecCommandBeginData struct {
	Command string `json:"command,omitempty"`
}

// ExecCommandEndData is emitted when command execution ends.
type ExecCommandEndData struct {
	ExitCode int `json:"exit_code"`
}

// PatchApplyData is emitted for patch apply events.
type PatchApplyData struct {
	File string `json:"file,omitempty"`
}

// CollabAgentData carries collaboration-agent event data.
type CollabAgentData struct {
	AgentID string `json:"agent_id,omitempty"`
	Name    string `json:"name,omitempty"`
}

// ThreadNameUpdatedData carries updated thread name.
type ThreadNameUpdatedData struct {
	Name string `json:"name,omitempty"`
}

// TurnDiffData carries turn diff text.
type TurnDiffData struct {
	Diff string `json:"diff,omitempty"`
}

// DynamicTool is injected into thread/start dynamic tools.
type DynamicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// DynamicToolCallData is the dynamic tool call server request payload.
// Keep camelCase tags unchanged for wire compatibility in Phase 1.
type DynamicToolCallData struct {
	ThreadID  string          `json:"threadId"`
	TurnID    string          `json:"turnId"`
	CallID    string          `json:"callId"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

// Session management DTOs.
type ThreadInfo struct {
	ThreadID string `json:"thread_id"`
}

type ResumeThreadRequest struct {
	ThreadID string `json:"thread_id"`
	Path     string `json:"path,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
}

type ForkThreadRequest struct {
	SourceThreadID string `json:"source_thread_id"`
	Cwd            string `json:"cwd,omitempty"`
}

type ForkThreadResponse struct {
	ThreadID string `json:"thread_id"`
	Port     int    `json:"port,omitempty"`
}

// Generic event constants.
const (
	// Core lifecycle.
	EventSessionConfigured = "session_configured"
	EventTurnStarted       = "turn_started"
	EventTurnComplete      = "turn_complete"
	EventTurnAborted       = "turn_aborted"
	EventIdle              = "idle"
	EventError             = "error"
	EventShutdownComplete  = "shutdown_complete"

	// Agent output.
	EventAgentMessage               = "agent_message"
	EventAgentMessageDelta          = "agent_message_delta"
	EventAgentMessageContentDelta   = "agent_message_content_delta"
	EventAgentReasoning             = "agent_reasoning"
	EventAgentReasoningDelta        = "agent_reasoning_delta"
	EventAgentReasoningRaw          = "agent_reasoning_raw"
	EventAgentReasoningRawDelta     = "agent_reasoning_raw_delta"
	EventAgentReasoningSectionBreak = "agent_reasoning_section_break"
	EventAgentMessageCompleted      = "agent_message_completed"

	// Command execution.
	EventExecApprovalRequest       = "exec_approval_request"
	EventExecCommandBegin          = "exec_command_begin"
	EventExecCommandOutputDelta    = "exec_command_output_delta"
	EventExecTerminalInteraction   = "exec_terminal_interaction"
	EventExecCommandEnd            = "exec_command_end"
	EventFileChangeApprovalRequest = "file_change_approval_request"

	// Code changes.
	EventPatchApply      = "patch_apply"
	EventPatchApplyBegin = "patch_apply_begin"
	EventPatchApplyEnd   = "patch_apply_end"
	EventFileRead        = "file_read"
	EventFileUpdated     = "file_updated"
	EventTurnDiff        = "turn_diff"
	EventUndoStarted     = "undo_started"
	EventUndoCompleted   = "undo_completed"

	// Reasoning compatibility events.
	EventReasoning            = "reasoning"
	EventReasoningDelta       = "reasoning_delta"
	EventReasoningSummary     = "reasoning_summary"
	EventReasoningSummaryPart = "reasoning_summary_part"

	// MCP / Skills / Review.
	EventMCPToolCallBegin     = "mcp_tool_call_begin"
	EventMCPToolCallEnd       = "mcp_tool_call_end"
	EventMCPToolCall          = "mcp_tool_call"
	EventMCPToolProgress      = "mcp_tool_progress"
	EventMCPListToolsResponse = "mcp_list_tools_response"
	EventListSkillsResponse   = "list_skills_response"
	EventEnteredReviewMode    = "entered_review_mode"
	EventExitedReviewMode     = "exited_review_mode"

	// Collaboration.
	EventCollabAgentSpawnBegin       = "collab_agent_spawn_begin"
	EventCollabAgentSpawnEnd         = "collab_agent_spawn_end"
	EventCollabAgentInteractionBegin = "collab_agent_interaction_begin"
	EventCollabAgentInteractionEnd   = "collab_agent_interaction_end"
	EventCollabWaitingBegin          = "collab_waiting_begin"
	EventCollabWaitingEnd            = "collab_waiting_end"

	// Dynamic tools.
	EventDynamicToolCall = "dynamic_tool_call"

	// MCP startup.
	EventMCPStartupComplete = "mcp_startup_complete"
	EventMCPStartupUpdate   = "mcp_startup_update"
	EventMCPOAuthCompleted  = "mcp_oauth_completed"

	// Others.
	EventTokenCount        = "token_count"
	EventContextCompacted  = "context_compacted"
	EventThreadNameUpdated = "thread_name_updated"
	EventThreadRolledBack  = "thread_rolled_back"
	EventWarning           = "warning"
	EventStreamError       = "stream_error"
	EventBackgroundEvent   = "background_event"
	EventTurnPlan          = "turn_plan"
	EventPlanDelta         = "plan_delta"
	EventPlanUpdate        = "plan_update"
	EventConnectionDead    = "connection_dead"
)
