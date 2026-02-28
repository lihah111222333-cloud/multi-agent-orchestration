package codex

import (
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

type (
	Event                   = agentcore.Event
	TextData                = agentcore.TextData
	ErrorData               = agentcore.ErrorData
	WarningData             = agentcore.WarningData
	TokenCountData          = agentcore.TokenCountData
	SessionConfiguredData   = agentcore.SessionConfiguredData
	ExecApprovalRequestData = agentcore.ExecApprovalRequestData
	ExecCommandBeginData    = agentcore.ExecCommandBeginData
	ExecCommandEndData      = agentcore.ExecCommandEndData
	PatchApplyData          = agentcore.PatchApplyData
	CollabAgentData         = agentcore.CollabAgentData
	ThreadNameUpdatedData   = agentcore.ThreadNameUpdatedData
	TurnDiffData            = agentcore.TurnDiffData
	DynamicTool             = agentcore.DynamicTool
	DynamicToolCallData     = agentcore.DynamicToolCallData
	ThreadInfo              = agentcore.ThreadInfo
	ResumeThreadRequest     = agentcore.ResumeThreadRequest
	ForkThreadRequest       = agentcore.ForkThreadRequest
	ForkThreadResponse      = agentcore.ForkThreadResponse
)

type DynamicToolCallResponse struct {
	ContentItems []DynamicToolContentItem `json:"contentItems"`
	Success      bool                     `json:"success"`
}

type DynamicToolContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type MCPToolCallData struct {
	ToolName string          `json:"tool_name,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
}

type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type MCPListToolsResponseData struct {
	Tools []MCPTool `json:"tools,omitempty"`
}

type ListSkillsResponseData struct {
	Skills []string `json:"skills,omitempty"`
}

const (
	EventSessionConfigured           = agentcore.EventSessionConfigured
	EventTurnStarted                 = agentcore.EventTurnStarted
	EventTurnComplete                = agentcore.EventTurnComplete
	EventIdle                        = agentcore.EventIdle
	EventError                       = agentcore.EventError
	EventShutdownComplete            = agentcore.EventShutdownComplete
	EventAgentMessage                = agentcore.EventAgentMessage
	EventAgentMessageDelta           = agentcore.EventAgentMessageDelta
	EventAgentMessageContentDelta    = agentcore.EventAgentMessageContentDelta
	EventAgentReasoning              = agentcore.EventAgentReasoning
	EventAgentReasoningDelta         = agentcore.EventAgentReasoningDelta
	EventAgentReasoningRaw           = agentcore.EventAgentReasoningRaw
	EventAgentReasoningRawDelta      = agentcore.EventAgentReasoningRawDelta
	EventAgentReasoningSectionBreak  = agentcore.EventAgentReasoningSectionBreak
	EventExecApprovalRequest         = agentcore.EventExecApprovalRequest
	EventExecCommandBegin            = agentcore.EventExecCommandBegin
	EventExecCommandOutputDelta      = agentcore.EventExecCommandOutputDelta
	EventExecCommandEnd              = agentcore.EventExecCommandEnd
	EventPatchApplyBegin             = agentcore.EventPatchApplyBegin
	EventPatchApplyEnd               = agentcore.EventPatchApplyEnd
	EventTurnDiff                    = agentcore.EventTurnDiff
	EventUndoStarted                 = agentcore.EventUndoStarted
	EventUndoCompleted               = agentcore.EventUndoCompleted
	EventMCPToolCallBegin            = agentcore.EventMCPToolCallBegin
	EventMCPToolCallEnd              = agentcore.EventMCPToolCallEnd
	EventMCPListToolsResponse        = agentcore.EventMCPListToolsResponse
	EventListSkillsResponse          = agentcore.EventListSkillsResponse
	EventEnteredReviewMode           = agentcore.EventEnteredReviewMode
	EventExitedReviewMode            = agentcore.EventExitedReviewMode
	EventCollabAgentSpawnBegin       = agentcore.EventCollabAgentSpawnBegin
	EventCollabAgentSpawnEnd         = agentcore.EventCollabAgentSpawnEnd
	EventCollabAgentInteractionBegin = agentcore.EventCollabAgentInteractionBegin
	EventCollabAgentInteractionEnd   = agentcore.EventCollabAgentInteractionEnd
	EventCollabWaitingBegin          = agentcore.EventCollabWaitingBegin
	EventCollabWaitingEnd            = agentcore.EventCollabWaitingEnd
	EventDynamicToolCall             = agentcore.EventDynamicToolCall
	EventMCPStartupComplete          = agentcore.EventMCPStartupComplete
	EventAgentMessageCompleted       = agentcore.EventAgentMessageCompleted
	EventTokenCount                  = agentcore.EventTokenCount
	EventContextCompacted            = agentcore.EventContextCompacted
	EventThreadNameUpdated           = agentcore.EventThreadNameUpdated
	EventThreadRolledBack            = agentcore.EventThreadRolledBack
	EventWarning                     = agentcore.EventWarning
	EventStreamError                 = agentcore.EventStreamError
	EventBackgroundEvent             = agentcore.EventBackgroundEvent
	EventPlanDelta                   = agentcore.EventPlanDelta
	EventPlanUpdate                  = agentcore.EventPlanUpdate
	EventConnectionDead              = agentcore.EventConnectionDead
)

type SubmitMessage struct {
	Type     string   `json:"type"`
	Prompt   string   `json:"prompt"`
	Images   []string `json:"images,omitempty"`
	Files    []string `json:"files,omitempty"`
	Skills   []Skill  `json:"skills,omitempty"`
	Mentions []any    `json:"mentions,omitempty"`
}

type Skill struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type CommandMessage struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Args    string `json:"args"`
}

type DynamicToolResultMessage struct {
	Type       string `json:"type"`
	ToolCallID string `json:"tool_call_id"`
	Output     string `json:"output"`
}

type CreateThreadRequest struct {
	Prompt         string        `json:"prompt"`
	Model          string        `json:"model,omitempty"`
	Profile        string        `json:"profile,omitempty"`
	Cwd            string        `json:"cwd,omitempty"`
	ApprovalPolicy string        `json:"approval_policy,omitempty"`
	Sandbox        string        `json:"sandbox,omitempty"`
	Images         []string      `json:"images,omitempty"`
	Files          []string      `json:"files,omitempty"`
	Skills         []Skill       `json:"skills,omitempty"`
	DynamicTools   []DynamicTool `json:"dynamic_tools,omitempty"`
}

type CreateThreadResponse struct {
	ThreadID string `json:"thread_id"`
	Port     int    `json:"port,omitempty"`
}

type HealthResponse struct {
	Status string `json:"status"`
	Port   int    `json:"port"`
	PID    int    `json:"pid"`
}

const (
	CmdCompact, CmdInterrupt = "/compact", "/interrupt"
	CmdClean, CmdShutdown = "/clean", "/shutdown"
	CmdUndo, CmdModel = "/undo", "/model"
	CmdRename, CmdReview = "/rename", "/review"
	CmdMCP, CmdSkills = "/mcp", "/skills"
	CmdApprovals, CmdPermissions = "/approvals", "/permissions"
	CmdPersonality = "/personality"
	CmdDebugMDrop, CmdDebugMUpdate = "/debug-m-drop", "/debug-m-update"
)

type CommandDef struct {
	Cmd       string
	Label     string
	HasArgs   bool
	ArgsHint  string
	Dangerous bool
}

var AllCommands = []CommandDef{
	{CmdCompact, "压缩上下文", false, "", false},
	{CmdInterrupt, "中断生成", false, "", false},
	{CmdClean, "清理终端", false, "", false},
	{CmdShutdown, "关闭 Agent", false, "", true},
	{CmdUndo, "撤销上一步", false, "", false},
	{CmdModel, "切换模型", true, "模型名称 (空=列出)", false},
	{CmdRename, "重命名线程", true, "新名称", false},
	{CmdReview, "代码审查", true, "自定义指令 (可选)", false},
	{CmdMCP, "列出 MCP 工具", false, "", false},
	{CmdSkills, "列出 Skills", false, "", false},
	{CmdApprovals, "审批策略", true, "never|on-failure|on-request|untrusted", false},
	{CmdPermissions, "审批策略 (别名)", true, "never|on-failure|on-request|untrusted", false},
	{CmdPersonality, "设置人格", true, "none|friendly|pragmatic", false},
	{CmdDebugMDrop, "清除记忆 (调试)", false, "", true},
	{CmdDebugMUpdate, "更新记忆 (调试)", false, "", false},
}
