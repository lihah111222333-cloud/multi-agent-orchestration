// Package codex 封装 Codex HTTP API 客户端。
//
// 支持: 进程管理、线程 CRUD、WebSocket 全双工通信、40+ 事件类型、15 种斜杠命令。
// 参考: http-api-usage.md v8.8.90
package codex

import (
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

// CLI-agnostic types are aliased to agentcore in Phase 1.
type Event = agentcore.Event
type TextData = agentcore.TextData
type ErrorData = agentcore.ErrorData
type WarningData = agentcore.WarningData
type TokenCountData = agentcore.TokenCountData
type SessionConfiguredData = agentcore.SessionConfiguredData
type ExecApprovalRequestData = agentcore.ExecApprovalRequestData
type ExecCommandBeginData = agentcore.ExecCommandBeginData
type ExecCommandEndData = agentcore.ExecCommandEndData
type PatchApplyData = agentcore.PatchApplyData
type CollabAgentData = agentcore.CollabAgentData
type ThreadNameUpdatedData = agentcore.ThreadNameUpdatedData
type TurnDiffData = agentcore.TurnDiffData
type DynamicTool = agentcore.DynamicTool
type DynamicToolCallData = agentcore.DynamicToolCallData

// DynamicToolCallResponse codex 期望的动态工具结果格式。
//
//	{"contentItems": [{"type": "inputText", "text": "..."}], "success": true}
type DynamicToolCallResponse struct {
	ContentItems []DynamicToolContentItem `json:"contentItems"`
	Success      bool                     `json:"success"`
}

// DynamicToolContentItem 结果内容项。
type DynamicToolContentItem struct {
	Type string `json:"type"` // "inputText"
	Text string `json:"text"`
}

// MCPToolCallData MCP 工具调用。
type MCPToolCallData struct {
	ToolName string          `json:"tool_name,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
}

// MCPTool 单个 MCP 工具描述。
type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// MCPListToolsResponseData MCP 工具列表。
type MCPListToolsResponseData struct {
	Tools []MCPTool `json:"tools,omitempty"`
}

// ListSkillsResponseData Skills 列表。
type ListSkillsResponseData struct {
	Skills []string `json:"skills,omitempty"`
}

// ========================================
// 事件类型常量
// ========================================
//
// NOTE: 保留 codex.Event* 导出是本阶段白名单要求。
// internal/bus 仍通过 codex.Event / codex.Event* 消费事件，P5 不迁移该路径。

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

// ========================================
// Client → Server 消息
// ========================================

// SubmitMessage Client→Server 提交对话。
type SubmitMessage struct {
	Type     string   `json:"type"` // "submit"
	Prompt   string   `json:"prompt"`
	Images   []string `json:"images,omitempty"`
	Files    []string `json:"files,omitempty"`
	Skills   []Skill  `json:"skills,omitempty"`
	Mentions []any    `json:"mentions,omitempty"`
}

// Skill 技能描述。
type Skill struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// CommandMessage Client→Server 斜杠命令。
type CommandMessage struct {
	Type    string `json:"type"`    // "command"
	Command string `json:"command"` // "/compact", ...
	Args    string `json:"args"`
}

// DynamicToolResultMessage Client→Server 动态工具结果回传。
type DynamicToolResultMessage struct {
	Type       string `json:"type"` // "dynamic_tool_result"
	ToolCallID string `json:"tool_call_id"`
	Output     string `json:"output"`
}

// ========================================
// HTTP API 请求/响应
// ========================================

// CreateThreadRequest POST /threads 请求。
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

// CreateThreadResponse POST /threads 响应。
type CreateThreadResponse struct {
	ThreadID string `json:"thread_id"`
	Port     int    `json:"port,omitempty"`
}

// HealthResponse GET /health 响应。
type HealthResponse struct {
	Status string `json:"status"`
	Port   int    `json:"port"`
	PID    int    `json:"pid"`
}

type ThreadInfo = agentcore.ThreadInfo
type ResumeThreadRequest = agentcore.ResumeThreadRequest
type ForkThreadRequest = agentcore.ForkThreadRequest
type ForkThreadResponse = agentcore.ForkThreadResponse

// ========================================
// 斜杠命令
// ========================================

const (
	CmdCompact      = "/compact"
	CmdInterrupt    = "/interrupt"
	CmdClean        = "/clean"
	CmdShutdown     = "/shutdown"
	CmdUndo         = "/undo"
	CmdModel        = "/model"
	CmdRename       = "/rename"
	CmdReview       = "/review"
	CmdMCP          = "/mcp"
	CmdSkills       = "/skills"
	CmdApprovals    = "/approvals"
	CmdPermissions  = "/permissions"
	CmdPersonality  = "/personality"
	CmdDebugMDrop   = "/debug-m-drop"
	CmdDebugMUpdate = "/debug-m-update"
)

// CommandDef 斜杠命令定义。
type CommandDef struct {
	Cmd       string
	Label     string
	HasArgs   bool
	ArgsHint  string
	Dangerous bool
}

// AllCommands 所有斜杠命令列表 (用于 UI)。
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
