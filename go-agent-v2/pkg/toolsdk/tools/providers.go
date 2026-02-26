package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

// DynamicTool is the tools-layer alias for dynamic tool schema.
type DynamicTool = agentcore.DynamicTool

// Tool is a dynamic tool schema paired with its runtime handler.
type Tool struct {
	Schema  DynamicTool
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
func Schemas(list []Tool) []DynamicTool {
	if len(list) == 0 {
		return nil
	}
	out := make([]DynamicTool, 0, len(list))
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
	CodeRunner() CodeExecRunner
	AuditLogger() AuditLogger
}

// ApprovalProvider exposes approval flow without transport coupling.
type ApprovalProvider interface {
	AwaitApproval(agentID, callID, mode, command string, isDangerous bool) bool
}

// ResourceProvider exposes resource stores and event notification hooks.
type ResourceProvider interface {
	DAGManager() DAGManager
	CommandCardStore() CardStore
	PromptTemplateStore() TemplateStore
	SharedFileStore() FileStore
	WorkspaceOps() WorkspaceOps
	NotifyEvent(method string, params any)
}

// OrchestrationProvider exposes orchestration runtime dependencies.
type OrchestrationProvider interface {
	AgentLauncher() AgentLauncher
	WorkspaceOps() WorkspaceOps
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
	AllSchemas() []DynamicTool
}

// CodeExecRunner abstracts code execution behavior.
type CodeExecRunner interface {
	Run(ctx context.Context, req CodeRunRequest) (*CodeRunResult, error)
}

// AuditLogger abstracts audit event persistence behavior.
type AuditLogger interface {
	Append(ctx context.Context, e *AuditEvent) error
}

// TaskDAG is tools-layer DAG DTO.
type TaskDAG struct {
	ID          int        `json:"id"`
	DagKey      string     `json:"dag_key"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	CreatedBy   string     `json:"created_by"`
	Metadata    any        `json:"metadata"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TaskDAGNode is tools-layer DAG node DTO.
type TaskDAGNode struct {
	ID         int        `json:"id"`
	DagKey     string     `json:"dag_key"`
	NodeKey    string     `json:"node_key"`
	Title      string     `json:"title"`
	NodeType   string     `json:"node_type"`
	AssignedTo string     `json:"assigned_to"`
	DependsOn  any        `json:"depends_on"`
	Status     string     `json:"status"`
	CommandRef string     `json:"command_ref"`
	Config     any        `json:"config"`
	Result     any        `json:"result"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// DAGManager abstracts DAG persistence behavior.
type DAGManager interface {
	SaveDAG(ctx context.Context, d *TaskDAG) (*TaskDAG, error)
	ListDAGs(ctx context.Context, keyword, status string, limit int) ([]TaskDAG, error)
	GetDAGDetail(ctx context.Context, dagKey string) (*TaskDAG, []TaskDAGNode, error)
	SaveNode(ctx context.Context, n *TaskDAGNode) (*TaskDAGNode, error)
	UpdateNodeStatus(ctx context.Context, dagKey, nodeKey, status string, result any) (*TaskDAGNode, error)
	ListNodes(ctx context.Context, dagKey string) ([]TaskDAGNode, error)
}

// CardStore abstracts command card persistence behavior.
type CardStore interface {
	Save(ctx context.Context, c any) (any, error)
	Get(ctx context.Context, cardKey string) (any, error)
	List(ctx context.Context, keyword string, limit int) (any, error)
	SetEnabled(ctx context.Context, cardKey string, enabled bool, updatedBy string) error
	Delete(ctx context.Context, cardKey string) error
}

// TemplateStore abstracts prompt template persistence behavior.
type TemplateStore interface {
	Save(ctx context.Context, t any) (any, error)
	Get(ctx context.Context, promptKey string) (any, error)
	List(ctx context.Context, agentKey, keyword string, limit int) (any, error)
	SetEnabled(ctx context.Context, promptKey string, enabled bool, updatedBy string) error
	Delete(ctx context.Context, promptKey string) error
}

// FileStore abstracts shared file persistence behavior.
type FileStore interface {
	Write(ctx context.Context, path, content, actor string) (any, error)
	Read(ctx context.Context, path string) (any, error)
	List(ctx context.Context, prefix string, limit int) (any, error)
	Delete(ctx context.Context, path, actor string) (bool, error)
}

// WorkspaceCreateRunRequest is tools-layer workspace create request.
type WorkspaceCreateRunRequest struct {
	RunKey     string   `json:"run_key"`
	DagKey     string   `json:"dag_key"`
	SourceRoot string   `json:"source_root"`
	CreatedBy  string   `json:"created_by"`
	Files      []string `json:"files"`
	Metadata   any      `json:"metadata"`
}

// WorkspaceMergeRunRequest is tools-layer workspace merge request.
type WorkspaceMergeRunRequest struct {
	RunKey        string `json:"run_key"`
	UpdatedBy     string `json:"updated_by"`
	DryRun        bool   `json:"dry_run"`
	DeleteRemoved bool   `json:"delete_removed"`
}

// WorkspaceOps abstracts workspace run lifecycle behavior.
type WorkspaceOps interface {
	CreateRun(ctx context.Context, req WorkspaceCreateRunRequest) (any, error)
	GetRun(ctx context.Context, runKey string) (any, error)
	ListRuns(ctx context.Context, status, dagKey string, limit int) (any, error)
	ResolveRunWorkspace(ctx context.Context, runKey string) (string, error)
	AbortRun(ctx context.Context, runKey, updatedBy, reason string) (any, error)
	MergeRun(ctx context.Context, req WorkspaceMergeRunRequest) (any, error)
}

// AgentLauncher abstracts agent process lifecycle behavior.
type AgentLauncher interface {
	Launch(ctx context.Context, id, name, prompt, cwd, instructions string, dynamicTools []DynamicTool) error
	Submit(id, prompt string, images, files []string) error
	Stop(id string) error
	List() any
}
