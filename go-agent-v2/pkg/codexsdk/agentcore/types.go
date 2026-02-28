package agentcore

import (
	"context"
	"encoding/json"
)

type AutoMatchInput struct {
	Type, Name string
}

type SkillMatchCandidate struct {
	Name         string
	ForceWords   []string
	TriggerWords []string
}

type AutoMatchedSkillMatch struct {
	Name, MatchedBy string
	MatchedTerms    []string
}

type AutoSkillMatchOptions struct {
	IncludeConfiguredExplicit bool
	IncludeConfiguredForce    bool
}

type TurnInput struct {
	Type, Text, URL, Path, Name, Content string
}

type TurnStartRequest struct {
	ThreadID, Cwd        string
	Input                []TurnInput
	SelectedSkills       []string
	ManualSkillSelection bool
	OutputSchema         json.RawMessage
}

type TurnSteerRequest struct {
	ThreadID, ExpectedTurnID string
	Input                    []TurnInput
	SelectedSkills           []string
	ManualSkillSelection     bool
}

type TurnAppendUserTimelineOptions struct {
	ThreadID, Prompt, SubmitPrompt string
	Images, Files                  []string
}

type TurnStartEntryPrepareResult struct {
	Prompt, SubmitPrompt  string
	Images                []string
	Files                 []string
	SelectedSkillCount    int
	AutoMatchedSkillCount int
}

type TurnSteerEntryPrepareResult struct {
	SubmitPrompt string
	Images       []string
	Files        []string
}

type TurnStartEntryResult struct {
	TurnID string
}

type TimelineAttachment struct {
	Kind, Name, Path, PreviewURL string
}

type TimelineItem struct {
	Kind string
	Text string
}

type Binding struct {
	CodexThreadID string
}

type ThreadListItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Archived bool   `json:"archived,omitempty"`
}

type TimelineRuntime interface {
	AppendUserMessage(threadID, text string, attachments []TimelineAttachment)
	ThreadTimeline(threadID string) []TimelineItem
}

type Process interface {
	Port() int
	IsAlive() bool
}

type Manager interface {
	GetProcess(agentID string) Process
	Launch(ctx context.Context, agentID, alias, profile, cwd, startInstructions string, dynamicTools []DynamicTool) error
	Stop(agentID string) error
}

type BindingStore interface {
	Bind(ctx context.Context, agentID, codexThreadID, sessionID string) error
	FindBindingByAgentID(ctx context.Context, agentID string) (*Binding, error)
}

type Event struct {
	Type              string                               `json:"type"`
	Data              json.RawMessage                      `json:"data,omitempty"`
	RequestID         *int64                               `json:"-"`
	RequestIDRaw      json.RawMessage                      `json:"-"`
	RespondFunc       func(code int, message string) error `json:"-"`
	RespondResultFunc func(result any) error               `json:"-"`
	DenyFunc          func() error                         `json:"-"`
}

type TextData struct {
	Delta   string `json:"delta,omitempty"`
	Content string `json:"content,omitempty"`
	Role    string `json:"role,omitempty"`
}

type (
	ErrorData struct {
		Message string `json:"message"`
		Code    string `json:"code,omitempty"`
	}
	WarningData struct {
		Message string `json:"message"`
	}
	TokenCountData struct {
		Input  int `json:"input"`
		Output int `json:"output"`
	}
	SessionConfiguredData struct {
		ThreadID string `json:"thread_id,omitempty"`
	}
	ExecApprovalRequestData struct {
		Command string `json:"command,omitempty"`
		Reason  string `json:"reason,omitempty"`
	}
	ExecCommandBeginData struct {
		Command string `json:"command,omitempty"`
	}
	ExecCommandEndData struct {
		ExitCode int `json:"exit_code"`
	}
	PatchApplyData struct {
		File string `json:"file,omitempty"`
	}
	CollabAgentData struct {
		AgentID string `json:"agent_id,omitempty"`
		Name    string `json:"name,omitempty"`
	}
	ThreadNameUpdatedData struct {
		Name string `json:"name,omitempty"`
	}
	TurnDiffData struct {
		Diff string `json:"diff,omitempty"`
	}
)

type DynamicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type DynamicToolCallData struct {
	ThreadID  string          `json:"threadId"`
	TurnID    string          `json:"turnId"`
	CallID    string          `json:"callId"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

type (
	ThreadInfo struct {
		ThreadID string `json:"thread_id"`
	}
	ResumeThreadRequest struct {
		ThreadID string `json:"thread_id"`
		Path     string `json:"path,omitempty"`
		Cwd      string `json:"cwd,omitempty"`
	}
	ForkThreadRequest struct {
		SourceThreadID string `json:"source_thread_id"`
		Cwd            string `json:"cwd,omitempty"`
	}
	ForkThreadResponse struct {
		ThreadID string `json:"thread_id"`
		Port     int    `json:"port,omitempty"`
	}
)

const (
	EventSessionConfigured = "session_configured"
	EventTurnStarted       = "turn_started"
	EventTurnComplete      = "turn_complete"
	EventTurnAborted       = "turn_aborted"
	EventIdle              = "idle"
	EventError             = "error"
	EventShutdownComplete  = "shutdown_complete"

	EventAgentMessage               = "agent_message"
	EventAgentMessageDelta          = "agent_message_delta"
	EventAgentMessageContentDelta   = "agent_message_content_delta"
	EventAgentReasoning             = "agent_reasoning"
	EventAgentReasoningDelta        = "agent_reasoning_delta"
	EventAgentReasoningRaw          = "agent_reasoning_raw"
	EventAgentReasoningRawDelta     = "agent_reasoning_raw_delta"
	EventAgentReasoningSectionBreak = "agent_reasoning_section_break"
	EventAgentMessageCompleted      = "agent_message_completed"

	EventExecApprovalRequest       = "exec_approval_request"
	EventExecCommandBegin          = "exec_command_begin"
	EventExecCommandOutputDelta    = "exec_command_output_delta"
	EventExecTerminalInteraction   = "exec_terminal_interaction"
	EventExecCommandEnd            = "exec_command_end"
	EventFileChangeApprovalRequest = "file_change_approval_request"

	EventPatchApply      = "patch_apply"
	EventPatchApplyBegin = "patch_apply_begin"
	EventPatchApplyEnd   = "patch_apply_end"
	EventFileRead        = "file_read"
	EventFileUpdated     = "file_updated"
	EventTurnDiff        = "turn_diff"
	EventUndoStarted     = "undo_started"
	EventUndoCompleted   = "undo_completed"

	EventReasoning            = "reasoning"
	EventReasoningDelta       = "reasoning_delta"
	EventReasoningSummary     = "reasoning_summary"
	EventReasoningSummaryPart = "reasoning_summary_part"

	EventMCPToolCallBegin     = "mcp_tool_call_begin"
	EventMCPToolCallEnd       = "mcp_tool_call_end"
	EventMCPToolCall          = "mcp_tool_call"
	EventMCPToolProgress      = "mcp_tool_progress"
	EventMCPListToolsResponse = "mcp_list_tools_response"
	EventListSkillsResponse   = "list_skills_response"
	EventEnteredReviewMode    = "entered_review_mode"
	EventExitedReviewMode     = "exited_review_mode"

	EventCollabAgentSpawnBegin       = "collab_agent_spawn_begin"
	EventCollabAgentSpawnEnd         = "collab_agent_spawn_end"
	EventCollabAgentInteractionBegin = "collab_agent_interaction_begin"
	EventCollabAgentInteractionEnd   = "collab_agent_interaction_end"
	EventCollabWaitingBegin          = "collab_waiting_begin"
	EventCollabWaitingEnd            = "collab_waiting_end"

	EventDynamicToolCall = "dynamic_tool_call"

	EventMCPStartupComplete = "mcp_startup_complete"
	EventMCPStartupUpdate   = "mcp_startup_update"
	EventMCPOAuthCompleted  = "mcp_oauth_completed"

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
