// Package mcp 提供 MCP 服务器 (对应 Python agents/all_in_one.py)。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/multi-agent/go-agent-v2/internal/store"
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
	// 通用参数结构 (keyword + limit 大部分工具共享)
	var p struct {
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
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			logger.Debug("mcp: unmarshal tool args", logger.FieldError, err)
		}
	}
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 100
	}

	switch name {
	case "interaction":
		return s.stores.Interaction.List(ctx, p.ThreadID, p.Keyword, p.Limit)
	case "task_trace":
		return s.stores.TaskTrace.List(ctx, p.AgentID, p.Keyword, nil, p.Limit)
	case "prompt_template":
		return s.stores.PromptTemplate.List(ctx, "", p.Keyword, p.Limit)
	case "command_card":
		return s.stores.CommandCard.List(ctx, p.Keyword, p.Limit)
	case "shared_file":
		if p.Path != "" && p.Content != "" {
			return s.stores.SharedFile.Write(ctx, p.Path, p.Content, p.Actor)
		}
		return s.stores.SharedFile.List(ctx, p.Prefix, p.Limit)
	case "audit_log":
		return s.stores.AuditLog.List(ctx, p.EventType, p.Action, p.Actor, p.Keyword, p.Limit)
	case "agent_status":
		return s.stores.AgentStatus.List(ctx, p.Status)
	case "topology_approval":
		return s.stores.TopologyApproval.GetPending(ctx)
	case "db_query":
		if p.SQL == "" {
			return nil, fmt.Errorf("db_query: sql is required")
		}
		return s.stores.DBQuery.Query(ctx, p.SQL, p.Limit)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}
