// Package codex 封装 Codex HTTP API 客户端。
//
// 支持: 进程管理、线程 CRUD、WebSocket 全双工通信、40+ 事件类型、15 种斜杠命令。
// 参考: http-api-usage.md v8.8.90
package codex

import "encoding/json"

// Event Codex WebSocket 事件信封。
type Event struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	RequestID *int64          `json:"-"` // 非零 = codex 发起的 Server Request, 需要 JSON-RPC response。

	// RespondFunc 允许在不依赖 proc 查找的情况下回复 codex server request。
	// 由 readLoop/handleRPCEvent 在检测到 server request 时注入,
	// 闭包捕获发送该请求的 client, 绕过 mgr.Get(agentID) 查找。
	RespondFunc func(code int, message string) error `json:"-"`

	// DenyFunc 允许在 proc==nil 时自动拒绝审批请求。
	// 闭包捕获发送该事件的 client.Submit("no"), 绕过 mgr.Get(agentID) 查找。
	DenyFunc func() error `json:"-"`
}

// ========================================
// 事件数据类型 (合并同构类型, 消除空结构体)
// ========================================

// TextData 文本增量/完整内容 (统一用于 agent_message_delta, reasoning_delta, exec_output_delta)。
type TextData struct {
	Delta   string `json:"delta,omitempty"`
	Content string `json:"content,omitempty"`
	Role    string `json:"role,omitempty"`
}

// ErrorData 错误。
type ErrorData struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// WarningData 非致命警告。
type WarningData struct {
	Message string `json:"message"`
}

// SessionConfiguredData 会话初始化完成。
type SessionConfiguredData struct {
	ThreadID string `json:"thread_id,omitempty"`
}

// ExecApprovalRequestData 请求执行审批。
type ExecApprovalRequestData struct {
	Command string `json:"command,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// ExecCommandBeginData 命令开始执行。
type ExecCommandBeginData struct {
	Command string `json:"command,omitempty"`
}

// ExecCommandEndData 命令执行结束。
type ExecCommandEndData struct {
	ExitCode int `json:"exit_code"`
}

// PatchApplyData 补丁应用。
type PatchApplyData struct {
	File string `json:"file,omitempty"`
}

// DynamicTool 动态工具定义 (通过 thread/start 注入 agent)。
type DynamicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// DynamicToolCallData codex 发起的动态工具调用 (Server Request)。
//
// codex 使用 camelCase 字段 (确认自 Rust v2.rs:3131-3140):
//
//	{"threadId": "xxx", "turnId": "xxx", "callId": "xxx",
//	 "tool": "lsp_hover", "arguments": {"file_path": "...", "line": 42, "column": 10}}
type DynamicToolCallData struct {
	ThreadID  string          `json:"threadId"`
	TurnID    string          `json:"turnId"`
	CallID    string          `json:"callId"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

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

// CollabAgentData 协作代理事件数据。
type CollabAgentData struct {
	AgentID string `json:"agent_id,omitempty"`
	Name    string `json:"name,omitempty"`
}

// TokenCountData Token 统计。
type TokenCountData struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// ThreadNameUpdatedData 线程名称更新。
type ThreadNameUpdatedData struct {
	Name string `json:"name,omitempty"`
}

// TurnDiffData 本次 turn 的文件差异。
type TurnDiffData struct {
	Diff string `json:"diff,omitempty"`
}

// ========================================
// 事件类型常量
// ========================================

const (
	// 核心生命周期
	EventSessionConfigured = "session_configured"
	EventTurnStarted       = "turn_started"
	EventTurnComplete      = "turn_complete"
	EventIdle              = "idle"
	EventError             = "error"
	EventShutdownComplete  = "shutdown_complete"

	// Agent 输出
	EventAgentMessage               = "agent_message"
	EventAgentMessageDelta          = "agent_message_delta"
	EventAgentMessageContentDelta   = "agent_message_content_delta"
	EventAgentReasoning             = "agent_reasoning"
	EventAgentReasoningDelta        = "agent_reasoning_delta"
	EventAgentReasoningRaw          = "agent_reasoning_raw"
	EventAgentReasoningRawDelta     = "agent_reasoning_raw_delta"
	EventAgentReasoningSectionBreak = "agent_reasoning_section_break"

	// 命令执行
	EventExecApprovalRequest    = "exec_approval_request"
	EventExecCommandBegin       = "exec_command_begin"
	EventExecCommandOutputDelta = "exec_command_output_delta"
	EventExecCommandEnd         = "exec_command_end"

	// 代码修改
	EventPatchApplyBegin = "patch_apply_begin"
	EventPatchApplyEnd   = "patch_apply_end"
	EventTurnDiff        = "turn_diff"
	EventUndoStarted     = "undo_started"
	EventUndoCompleted   = "undo_completed"

	// MCP / Skills / Review
	EventMCPToolCallBegin     = "mcp_tool_call_begin"
	EventMCPToolCallEnd       = "mcp_tool_call_end"
	EventMCPListToolsResponse = "mcp_list_tools_response"
	EventListSkillsResponse   = "list_skills_response"
	EventEnteredReviewMode    = "entered_review_mode"
	EventExitedReviewMode     = "exited_review_mode"

	// 协作代理
	EventCollabAgentSpawnBegin       = "collab_agent_spawn_begin"
	EventCollabAgentSpawnEnd         = "collab_agent_spawn_end"
	EventCollabAgentInteractionBegin = "collab_agent_interaction_begin"
	EventCollabAgentInteractionEnd   = "collab_agent_interaction_end"
	EventCollabWaitingBegin          = "collab_waiting_begin"
	EventCollabWaitingEnd            = "collab_waiting_end"

	// Dynamic Tools (自定义工具回调)
	EventDynamicToolCall = "dynamic_tool_call"

	// MCP 启动
	EventMCPStartupComplete = "mcp_startup_complete"

	// Agent 消息完成
	EventAgentMessageCompleted = "agent_message_completed"

	// 其他
	EventTokenCount        = "token_count"
	EventContextCompacted  = "context_compacted"
	EventThreadNameUpdated = "thread_name_updated"
	EventThreadRolledBack  = "thread_rolled_back"
	EventWarning           = "warning"
	EventStreamError       = "stream_error"
	EventBackgroundEvent   = "background_event"
	EventPlanDelta         = "plan_delta"
	EventPlanUpdate        = "plan_update"
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

// ThreadInfo GET /threads 列表项。
type ThreadInfo struct {
	ThreadID string `json:"thread_id"`
}

// ResumeThreadRequest 恢复已有会话 (对应 CLI: codex resume <id> [path])。
type ResumeThreadRequest struct {
	ThreadID string `json:"thread_id"`
	Path     string `json:"path,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
}

// ForkThreadRequest 分叉会话 (对应 CLI: codex fork <id> [path])。
type ForkThreadRequest struct {
	SourceThreadID string `json:"source_thread_id"`
	Cwd            string `json:"cwd,omitempty"`
}

// ForkThreadResponse POST /threads/:id/fork 响应。
type ForkThreadResponse struct {
	ThreadID string `json:"thread_id"`
	Port     int    `json:"port,omitempty"`
}

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
