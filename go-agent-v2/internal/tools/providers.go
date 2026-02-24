package tools

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

// Tool is a dynamic tool schema paired with its runtime handler.
type Tool struct {
	Schema  agentcore.DynamicTool
	Handler func(ctx ToolCallContext, args json.RawMessage) string
}

// ToolCallContext carries dynamic tool call runtime metadata.
type ToolCallContext struct {
	AgentID   string
	CallID    string
	RequestID *int64
	Ctx       context.Context
}

// Schemas extracts schemas from tool definitions.
func Schemas(list []Tool) []agentcore.DynamicTool {
	if len(list) == 0 {
		return nil
	}
	out := make([]agentcore.DynamicTool, 0, len(list))
	for _, tool := range list {
		out = append(out, tool.Schema)
	}
	return out
}

// FindTool finds a tool by schema name.
func FindTool(list []Tool, name string) (Tool, bool) {
	for _, tool := range list {
		if tool.Schema.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

// CodeRunProvider exposes runtime dependencies for code_run tools.
type CodeRunProvider interface {
	CodeRunner() *executor.CodeRunner
	AuditLogStore() *store.AuditLogStore
}

// ApprovalProvider exposes approval flow without transport coupling.
type ApprovalProvider interface {
	AwaitApproval(agentID, callID, mode, command string, isDangerous bool) bool
}

// ResourceProvider exposes resource stores and event notification hooks.
type ResourceProvider interface {
	DAGStore() *store.TaskDAGStore
	CommandCardStore() *store.CommandCardStore
	PromptTemplateStore() *store.PromptTemplateStore
	SharedFileStore() *store.SharedFileStore
	WorkspaceManager() *service.WorkspaceManager
	NotifyEvent(method string, params any)
}

// OrchestrationProvider exposes orchestration runtime dependencies.
type OrchestrationProvider interface {
	Manager() *runner.AgentManager
	WorkspaceManager() *service.WorkspaceManager
	SubmitPrompt(agentID, prompt string, images, files []string) error
	RememberReportRequest(senderID, workerID string)
	NextThreadSeq() int64
}

// AgentRuntimeProvider exposes cross-tool agent runtime state.
type AgentRuntimeProvider interface {
	CancelCodeRuns(agentID string) int
	SetAgentWorkDir(agentID, cwd string)
	ClearAgentWorkDir(agentID string)
	GetAgentWorkDir(agentID string) string
}

// SchemaProvider exposes all dynamic tool schemas for recursive orchestration dependencies.
type SchemaProvider interface {
	AllSchemas() []agentcore.DynamicTool
}
