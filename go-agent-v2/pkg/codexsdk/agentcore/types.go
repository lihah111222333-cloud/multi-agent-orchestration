package agentcore

import "encoding/json"

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
