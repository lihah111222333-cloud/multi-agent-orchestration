package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

type DynamicTool = agentcore.DynamicTool

type Tool struct {
	Schema  DynamicTool
	Handler func(ctx ToolCallContext, args json.RawMessage) string
}

type ToolCallContext struct {
	AgentID   string
	CallID    string
	RequestID *int64
	Ctx       context.Context
}

func Schemas(list []Tool) []DynamicTool {
	out := make([]DynamicTool, 0, len(list))
	for i := range list {
		out = append(out, list[i].Schema)
	}
	return out
}

func FindTool(list []Tool, name string) (Tool, bool) {
	for i := range list {
		if tool := list[i]; tool.Schema.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

type (
	CodeRunProvider interface {
		CodeRunner() CodeExecRunner
		AuditLogger() AuditLogger
	}
	ApprovalProvider interface {
		AwaitApproval(agentID, callID, mode, command string, isDangerous bool) bool
	}
	ResourceProvider interface {
		DAGManager() DAGManager
		CommandCardStore() CardStore
		PromptTemplateStore() TemplateStore
		SharedFileStore() FileStore
		WorkspaceOps() WorkspaceOps
		NotifyEvent(method string, params any)
	}
	OrchestrationProvider interface {
		AgentLauncher() AgentLauncher
		WorkspaceOps() WorkspaceOps
		SubmitPrompt(agentID, prompt string, images, files []string) error
		RememberReportRequest(senderID, workerID string)
		NextThreadSeq() int64
		SaveSubAgent(id, name, cwd string)
		DeleteSubAgent(id string)
	}
	AgentRuntimeProvider interface {
		CancelCodeRuns(agentID string) int
		SetAgentWorkDir(agentID, cwd string)
		ClearAgentWorkDir(agentID string)
		GetAgentWorkDir(agentID string) string
	}
	SchemaProvider interface {
		AllSchemas() []DynamicTool
	}
	CodeExecRunner interface {
		Run(ctx context.Context, req CodeRunRequest) (*CodeRunResult, error)
	}
	AuditLogger interface {
		Append(ctx context.Context, e *AuditEvent) error
	}
)

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

type DAGManager interface {
	SaveDAG(ctx context.Context, d *TaskDAG) (*TaskDAG, error)
	ListDAGs(ctx context.Context, keyword, status string, limit int) ([]TaskDAG, error)
	GetDAGDetail(ctx context.Context, dagKey string) (*TaskDAG, []TaskDAGNode, error)
	SaveNode(ctx context.Context, n *TaskDAGNode) (*TaskDAGNode, error)
	UpdateNodeStatus(ctx context.Context, dagKey, nodeKey, status string, result any) (*TaskDAGNode, error)
	ListNodes(ctx context.Context, dagKey string) ([]TaskDAGNode, error)
}

type CardStore interface {
	Save(ctx context.Context, c any) (any, error)
	Get(ctx context.Context, cardKey string) (any, error)
	List(ctx context.Context, keyword string, limit int) (any, error)
	SetEnabled(ctx context.Context, cardKey string, enabled bool, updatedBy string) error
	Delete(ctx context.Context, cardKey string) error
}

type TemplateStore interface {
	Save(ctx context.Context, t any) (any, error)
	Get(ctx context.Context, promptKey string) (any, error)
	List(ctx context.Context, agentKey, keyword string, limit int) (any, error)
	SetEnabled(ctx context.Context, promptKey string, enabled bool, updatedBy string) error
	Delete(ctx context.Context, promptKey string) error
}

type FileStore interface {
	Write(ctx context.Context, path, content, actor string) (any, error)
	Read(ctx context.Context, path string) (any, error)
	List(ctx context.Context, prefix string, limit int) (any, error)
	Delete(ctx context.Context, path, actor string) (bool, error)
}

type WorkspaceCreateRunRequest struct {
	RunKey     string   `json:"run_key"`
	DagKey     string   `json:"dag_key"`
	SourceRoot string   `json:"source_root"`
	CreatedBy  string   `json:"created_by"`
	Files      []string `json:"files"`
	Metadata   any      `json:"metadata"`
}

type WorkspaceMergeRunRequest struct {
	RunKey        string `json:"run_key"`
	UpdatedBy     string `json:"updated_by"`
	DryRun        bool   `json:"dry_run"`
	DeleteRemoved bool   `json:"delete_removed"`
}

type WorkspaceOps interface {
	CreateRun(ctx context.Context, req WorkspaceCreateRunRequest) (any, error)
	GetRun(ctx context.Context, runKey string) (any, error)
	ListRuns(ctx context.Context, status, dagKey string, limit int) (any, error)
	ResolveRunWorkspace(ctx context.Context, runKey string) (string, error)
	AbortRun(ctx context.Context, runKey, updatedBy, reason string) (any, error)
	MergeRun(ctx context.Context, req WorkspaceMergeRunRequest) (any, error)
}

type AgentLauncher interface {
	Launch(ctx context.Context, id, name, prompt, cwd, instructions string, dynamicTools []DynamicTool) error
	Submit(id, prompt string, images, files []string) error
	Stop(id string) error
	List() any
	GetReport(id string) string
	GetState(id string) string
}
