// Package mcp 提供 MCP 服务器 (对应 Python agents/all_in_one.py)。
package mcp

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/store"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// Server MCP 服务器。
type Server struct {
	stores *Stores
}

// Stores MCP 工具依赖。
type Stores struct {
	Interaction      *store.InteractionStore
	TaskTrace        *store.TaskTraceStore
	PromptTemplate   *store.PromptTemplateStore
	CommandCard      *store.CommandCardStore
	AuditLog         *store.AuditLogStore
	SharedFile       *store.SharedFileStore
	AgentStatus      *store.AgentStatusStore
	TopologyApproval *store.TopologyApprovalStore
	DBQuery          *store.DBQueryStore
}

// NewServer 创建 MCP 服务器。
func NewServer(stores *Stores) *Server {
	return &Server{stores: stores}
}

// Start 启动 MCP 服务器 (stdio transport)。
// 待集成 mcp-go SDK — 目前使用简易 JSON-RPC over stdin/stdout。
func (s *Server) Start(ctx context.Context) error {
	logger.Info("MCP server starting (stdio)")
	// TODO: 集成 github.com/mark3labs/mcp-go
	// 以下为工具注册占位
	tools := s.toolRegistry()
	logger.Infow("MCP tools registered", logger.FieldCount, len(tools))
	<-ctx.Done()
	return nil
}

// Tool MCP 工具定义。
type Tool struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, args json.RawMessage) (any, error)
}

type toolParams struct {
	Keyword   string `json:"keyword"`
	Limit     int    `json:"limit"`
	AgentID   string `json:"agent_id"`
	EventType string `json:"event_type"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Status    string `json:"status"`
	ThreadID  string `json:"thread_id"`
	Prefix    string `json:"prefix"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	SQL       string `json:"sql"`
}

// toolRegistry 注册 10 个 MCP 工具 (对应 Python @mcp.tool)。
func (s *Server) toolRegistry() []Tool {
	return []Tool{
		{Name: "interaction", Description: "交互记录 CRUD"},
		{Name: "task_trace", Description: "任务追踪查询"},
		{Name: "prompt_template", Description: "提示词模板管理"},
		{Name: "command_card", Description: "命令卡管理"},
		{Name: "shared_file", Description: "共享文件读写"},
		{Name: "audit_log", Description: "审计日志查询"},
		{Name: "agent_status", Description: "Agent 状态查询"},
		{Name: "topology_approval", Description: "拓扑审批管理"},
		{Name: "db_query", Description: "通用数据库查询"},
		{Name: "config_manage", Description: "配置管理"},
	}
}

// HandleTool 处理工具调用 (对应 Python all_in_one.py 10 个 @mcp.tool)。
func (s *Server) HandleTool(ctx context.Context, name string, args json.RawMessage) (any, error) {
	p := parseToolParams(args)
	handlers := map[string]func(context.Context) (any, error){
		"interaction": func(ctx context.Context) (any, error) {
			return s.stores.Interaction.List(ctx, p.ThreadID, p.Keyword, p.Limit)
		},
		"task_trace": func(ctx context.Context) (any, error) {
			return s.stores.TaskTrace.List(ctx, p.AgentID, p.Keyword, nil, p.Limit)
		},
		"prompt_template": func(ctx context.Context) (any, error) {
			return s.stores.PromptTemplate.List(ctx, "", p.Keyword, p.Limit)
		},
		"command_card": func(ctx context.Context) (any, error) {
			return s.stores.CommandCard.List(ctx, p.Keyword, p.Limit)
		},
		"shared_file": func(ctx context.Context) (any, error) {
			if p.Path != "" && p.Content != "" {
				return s.stores.SharedFile.Write(ctx, p.Path, p.Content, p.Actor)
			}
			return s.stores.SharedFile.List(ctx, p.Prefix, p.Limit)
		},
		"audit_log": func(ctx context.Context) (any, error) {
			return s.stores.AuditLog.List(ctx, p.EventType, p.Action, p.Actor, p.Keyword, p.Limit)
		},
		"agent_status": func(ctx context.Context) (any, error) {
			return s.stores.AgentStatus.List(ctx, p.Status)
		},
		"topology_approval": func(ctx context.Context) (any, error) {
			return s.stores.TopologyApproval.GetPending(ctx)
		},
		"db_query": func(ctx context.Context) (any, error) {
			if p.SQL == "" {
				return nil, apperrors.New("MCP.HandleTool", "db_query: sql is required")
			}
			return s.stores.DBQuery.Query(ctx, p.SQL, p.Limit)
		},
	}
	handler, ok := handlers[name]
	if !ok {
		return nil, apperrors.Newf("MCP.HandleTool", "unknown tool: %s", name)
	}
	return handler(ctx)
}

func parseToolParams(args json.RawMessage) toolParams {
	var params toolParams
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			logger.Debug("mcp: unmarshal tool args", logger.FieldError, err)
		}
	}
	params.Limit = normalizeToolLimit(params.Limit)
	return params
}

func normalizeToolLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}
