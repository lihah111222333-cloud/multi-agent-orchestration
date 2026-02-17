// Package service 提供 UI 和 Store 之间的桥接层 (进程内零序列化)。
package service

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

// Service 聚合所有 service 实例。
type Service struct {
	Status   *StatusService
	Logs     *LogService
	DAG      *DAGService
	Tasks    *TaskService
	Commands *CommandService
	Memory   *MemoryService
	Skills   *SkillService
}

// Stores 桌面应用需要的所有 store (复用 dashboard.Stores 结构)。
type Stores struct {
	AgentStatus    *store.AgentStatusStore
	AuditLog       *store.AuditLogStore
	SystemLog      *store.SystemLogStore
	AILog          *store.AILogStore
	BusLog         *store.BusLogStore
	TaskDAG        *store.TaskDAGStore
	TaskAck        *store.TaskAckStore
	TaskTrace      *store.TaskTraceStore
	CommandCard    *store.CommandCardStore
	PromptTemplate *store.PromptTemplateStore
	SharedFile     *store.SharedFileStore
}

// New 创建 Service 层。
func New(s *Stores, skillsDir string) *Service {
	return &Service{
		Status:   &StatusService{store: s.AgentStatus},
		Logs:     &LogService{audit: s.AuditLog, system: s.SystemLog, ai: s.AILog, bus: s.BusLog},
		DAG:      &DAGService{store: s.TaskDAG},
		Tasks:    &TaskService{ack: s.TaskAck, trace: s.TaskTrace},
		Commands: &CommandService{card: s.CommandCard, prompt: s.PromptTemplate},
		Memory:   &MemoryService{store: s.SharedFile},
		Skills:   &SkillService{dir: skillsDir},
	}
}

// --- StatusService ---

// StatusService 封装 AgentStatus 查询。
type StatusService struct {
	store *store.AgentStatusStore
}

// List 返回所有 Agent 状态 (空 status = 不过滤)。
func (s *StatusService) List(ctx context.Context) ([]store.AgentStatus, error) {
	return s.store.List(ctx, "")
}

// --- LogService ---

// LogService 聚合 4 种日志 store。
type LogService struct {
	audit  *store.AuditLogStore
	system *store.SystemLogStore
	ai     *store.AILogStore
	bus    *store.BusLogStore
}

// QueryAudit 查询审计日志。
func (s *LogService) QueryAudit(ctx context.Context, limit int) ([]store.AuditEvent, error) {
	return s.audit.List(ctx, "", "", "", "", limit)
}

// QuerySystem 查询系统日志。
func (s *LogService) QuerySystem(ctx context.Context, limit int) ([]store.SystemLog, error) {
	return s.system.List(ctx, "", "", "", limit)
}

// QueryAI 查询 AI 日志。
func (s *LogService) QueryAI(ctx context.Context, limit int) ([]store.AILogRow, error) {
	return s.ai.Query(ctx, "", "", limit)
}

// QueryBus 查询总线日志。
func (s *LogService) QueryBus(ctx context.Context, limit int) ([]store.BusException, error) {
	return s.bus.List(ctx, "", "", "", limit)
}

// --- DAGService ---

// DAGService 封装 TaskDAG 查询。
type DAGService struct {
	store *store.TaskDAGStore
}

// ListDAGs 查询 DAG 列表。
func (s *DAGService) ListDAGs(ctx context.Context, keyword, status string, limit int) ([]store.TaskDAG, error) {
	return s.store.ListDAGs(ctx, keyword, status, limit)
}

// GetDAGDetail 查询 DAG 详情 (含节点)。
func (s *DAGService) GetDAGDetail(ctx context.Context, dagKey string) (*store.TaskDAG, []store.TaskDAGNode, error) {
	return s.store.GetDAGDetail(ctx, dagKey)
}

// --- TaskService ---

// TaskService 封装 TaskAck + TaskTrace 查询。
type TaskService struct {
	ack   *store.TaskAckStore
	trace *store.TaskTraceStore
}

// ListAcks 查询任务确认列表。
func (s *TaskService) ListAcks(ctx context.Context, limit int) ([]store.TaskAck, error) {
	return s.ack.List(ctx, "", "", "", "", limit)
}

// ListTraces 查询任务追踪列表。
func (s *TaskService) ListTraces(ctx context.Context, limit int) ([]store.TaskTrace, error) {
	return s.trace.List(ctx, "", "", nil, limit)
}

// --- CommandService ---

// CommandService 封装命令卡 + 提示词模板。
type CommandService struct {
	card   *store.CommandCardStore
	prompt *store.PromptTemplateStore
}

// ListCards 查询命令卡列表。
func (s *CommandService) ListCards(ctx context.Context, limit int) ([]store.CommandCard, error) {
	return s.card.List(ctx, "", limit)
}

// ListPrompts 查询提示词模板列表。
func (s *CommandService) ListPrompts(ctx context.Context, limit int) ([]store.PromptTemplate, error) {
	return s.prompt.List(ctx, "", "", limit)
}

// --- MemoryService ---

// MemoryService 封装 SharedFile 操作。
type MemoryService struct {
	store *store.SharedFileStore
}

// ListFiles 查询共享文件列表。
func (s *MemoryService) ListFiles(ctx context.Context) ([]store.SharedFile, error) {
	return s.store.List(ctx, "", 500)
}

// GetFile 读取文件内容。
func (s *MemoryService) GetFile(ctx context.Context, path string) (*store.SharedFile, error) {
	return s.store.Read(ctx, path)
}

// WriteFile 写入文件内容。
func (s *MemoryService) WriteFile(ctx context.Context, path, content, updatedBy string) error {
	_, err := s.store.Write(ctx, path, content, updatedBy)
	return err
}
