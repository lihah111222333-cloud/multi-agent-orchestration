// dashboard.go — Wails 绑定: 数据库 Dashboard 查询。
//
// 前端通过 window.go.main.Dashboard.XXX() 调用。
// 所有方法返回 JSON-safe 结构, Wails 自动序列化。
package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

// Dashboard Wails 绑定 — 数据库查询 (前端通过 window.go.main.Dashboard 调用)。
type Dashboard struct {
	svc         *service.Service
	pool        *pgxpool.Pool
	sysLogStore *store.SystemLogStore
	intStore    *store.InteractionStore
}

// NewDashboard 创建 Dashboard 实例 (需要 DB 连接池)。
func NewDashboard(pool *pgxpool.Pool) *Dashboard {
	stores := &service.Stores{
		AgentStatus:    store.NewAgentStatusStore(pool),
		AuditLog:       store.NewAuditLogStore(pool),
		SystemLog:      store.NewSystemLogStore(pool),
		AILog:          store.NewAILogStore(pool),
		BusLog:         store.NewBusLogStore(pool),
		TaskDAG:        store.NewTaskDAGStore(pool),
		TaskAck:        store.NewTaskAckStore(pool),
		TaskTrace:      store.NewTaskTraceStore(pool),
		CommandCard:    store.NewCommandCardStore(pool),
		PromptTemplate: store.NewPromptTemplateStore(pool),
		SharedFile:     store.NewSharedFileStore(pool),
	}
	return &Dashboard{
		svc:         service.New(stores, ".agent/skills"),
		pool:        pool,
		sysLogStore: store.NewSystemLogStore(pool),
		intStore:    store.NewInteractionStore(pool),
	}
}

// ─── Agent 状态 ───

// ListAgentStatus 查询所有 Agent 状态。
func (d *Dashboard) ListAgentStatus() ([]store.AgentStatus, error) {
	list, err := d.svc.Status.List(context.Background())
	if err != nil {
		slog.Warn("dashboard: ListAgentStatus failed", "error", err)
		return []store.AgentStatus{}, nil
	}
	return list, nil
}

// ─── DAG ───

// ListDAGs 查询 DAG 列表。
func (d *Dashboard) ListDAGs() ([]store.TaskDAG, error) {
	list, err := d.svc.DAG.ListDAGs(context.Background(), "", "", 100)
	if err != nil {
		slog.Warn("dashboard: ListDAGs failed", "error", err)
		return []store.TaskDAG{}, nil
	}
	return list, nil
}

// GetDAGDetail 查询 DAG 详情 (含全部节点)。
func (d *Dashboard) GetDAGDetail(dagKey string) (map[string]any, error) {
	dag, nodes, err := d.svc.DAG.GetDAGDetail(context.Background(), dagKey)
	if err != nil {
		slog.Warn("dashboard: GetDAGDetail failed", "error", err)
		return nil, err
	}
	return map[string]any{"dag": dag, "nodes": nodes}, nil
}

// ─── Tasks ───

// ListTaskAcks 查询任务工单列表。
func (d *Dashboard) ListTaskAcks() ([]store.TaskAck, error) {
	list, err := d.svc.Tasks.ListAcks(context.Background(), 100)
	if err != nil {
		slog.Warn("dashboard: ListTaskAcks failed", "error", err)
		return []store.TaskAck{}, nil
	}
	return list, nil
}

// ListTaskTraces 查询任务追踪列表。
func (d *Dashboard) ListTaskTraces() ([]store.TaskTrace, error) {
	list, err := d.svc.Tasks.ListTraces(context.Background(), 100)
	if err != nil {
		slog.Warn("dashboard: ListTaskTraces failed", "error", err)
		return []store.TaskTrace{}, nil
	}
	return list, nil
}

// ─── Commands & Prompts ───

// ListCommandCards 查询命令卡列表。
func (d *Dashboard) ListCommandCards() ([]store.CommandCard, error) {
	list, err := d.svc.Commands.ListCards(context.Background(), 100)
	if err != nil {
		slog.Warn("dashboard: ListCommandCards failed", "error", err)
		return []store.CommandCard{}, nil
	}
	return list, nil
}

// ListPromptTemplates 查询提示词模板列表。
func (d *Dashboard) ListPromptTemplates() ([]store.PromptTemplate, error) {
	list, err := d.svc.Commands.ListPrompts(context.Background(), 100)
	if err != nil {
		slog.Warn("dashboard: ListPromptTemplates failed", "error", err)
		return []store.PromptTemplate{}, nil
	}
	return list, nil
}

// ─── Memory / Shared Files ───

// ListSharedFiles 查询共享文件列表。
func (d *Dashboard) ListSharedFiles() ([]store.SharedFile, error) {
	list, err := d.svc.Memory.ListFiles(context.Background())
	if err != nil {
		slog.Warn("dashboard: ListSharedFiles failed", "error", err)
		return []store.SharedFile{}, nil
	}
	return list, nil
}

// ReadSharedFile 读取单个共享文件。
func (d *Dashboard) ReadSharedFile(path string) (*store.SharedFile, error) {
	return d.svc.Memory.GetFile(context.Background(), path)
}

// WriteSharedFile 写入共享文件。
func (d *Dashboard) WriteSharedFile(path, content, actor string) error {
	return d.svc.Memory.WriteFile(context.Background(), path, content, actor)
}

// ─── Logs ───

// ListSystemLogs 查询系统日志 (v2: 全字段过滤)。
func (d *Dashboard) ListSystemLogs(level, source, component, agentID, eventType, toolName, keyword string, limit int) ([]store.SystemLog, error) {
	if limit <= 0 || limit > 2000 {
		limit = 100
	}
	logs, err := d.sysLogStore.ListV2(context.Background(), store.ListParams{
		Level:     level,
		Source:    source,
		Component: component,
		AgentID:   agentID,
		EventType: eventType,
		ToolName:  toolName,
		Keyword:   keyword,
		Limit:     limit,
	})
	if err != nil {
		slog.Warn("dashboard: ListSystemLogs failed", "error", err)
		return []store.SystemLog{}, nil
	}
	return logs, nil
}

// ListLogFilters 返回日志筛选器可选值。
func (d *Dashboard) ListLogFilters() (map[string][]string, error) {
	filters, err := d.sysLogStore.ListFilterValues(context.Background())
	if err != nil {
		slog.Warn("dashboard: ListLogFilters failed", "error", err)
		return map[string][]string{}, nil
	}
	return filters, nil
}

// ListAuditLogs 查询审计日志。
func (d *Dashboard) ListAuditLogs(eventType, action, actor, keyword string, limit int) ([]store.AuditEvent, error) {
	if limit <= 0 || limit > 2000 {
		limit = 100
	}
	logs, err := d.svc.Logs.QueryAudit(context.Background(), limit)
	if err != nil {
		slog.Warn("dashboard: ListAuditLogs failed", "error", err)
		return []store.AuditEvent{}, nil
	}
	return logs, nil
}

// ListAILogs 查询 AI 日志。
func (d *Dashboard) ListAILogs(keyword string, limit int) ([]store.AILogRow, error) {
	if limit <= 0 || limit > 2000 {
		limit = 100
	}
	logs, err := d.svc.Logs.QueryAI(context.Background(), limit)
	if err != nil {
		slog.Warn("dashboard: ListAILogs failed", "error", err)
		return []store.AILogRow{}, nil
	}
	return logs, nil
}

// ListBusLogs 查询总线日志。
func (d *Dashboard) ListBusLogs(keyword string, limit int) ([]store.BusException, error) {
	if limit <= 0 || limit > 2000 {
		limit = 100
	}
	logs, err := d.svc.Logs.QueryBus(context.Background(), limit)
	if err != nil {
		slog.Warn("dashboard: ListBusLogs failed", "error", err)
		return []store.BusException{}, nil
	}
	return logs, nil
}

// ─── Interactions ───

// ListInteractions 查询交互记录。
func (d *Dashboard) ListInteractions(threadID, keyword string, limit int) ([]store.Interaction, error) {
	if limit <= 0 || limit > 2000 {
		limit = 100
	}
	list, err := d.intStore.List(context.Background(), threadID, keyword, limit)
	if err != nil {
		slog.Warn("dashboard: ListInteractions failed", "error", err)
		return []store.Interaction{}, nil
	}
	return list, nil
}

// Close 关闭数据库连接池。
func (d *Dashboard) Close() {
	if d.pool != nil {
		d.pool.Close()
	}
}
