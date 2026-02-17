// dashboard_methods.go — Dashboard JSON-RPC 方法 (§ 12)。
//
// 替代 cmd/agent-terminal/dashboard.go 中的 Wails 绑定。
// 前端通过 App.CallAPI('dashboard/xxx', '{"limit":100}') 调用。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"time"
)

// dashLimitParams 通用分页参数。
type dashLimitParams struct {
	Limit int `json:"limit"`
}

func parseDashLimit(params json.RawMessage, defaultLimit int) int {
	var p dashLimitParams
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		return defaultLimit
	}
	return p.Limit
}

func dashCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// isNilStore 安全检测 store 是否为 nil (处理 typed nil 指针)。
//
// Go 中 `(*SomeStore)(nil)` 作为 `any` 参数传入时 `store == nil` 返回 false,
// 但实际解引用会 panic。此函数同时检测 untyped nil 和 typed nil。
func isNilStore(store any) bool {
	if store == nil {
		return true
	}
	v := reflect.ValueOf(store)
	return v.Kind() == reflect.Ptr && v.IsNil()
}

// dashList 通用 Dashboard 列表查询模板。
//
// 封装 dashboard/xxx 方法的共享骨架:
//
//	nil store check → typedHandler unmarshal → dashCtx timeout → query → error→empty fallback → wrap
//
// 用法:
//
//	s.methods["dashboard/dags"] = dashList[dagParams]("dags", s.dagStore,
//	    func(ctx context.Context, p dagParams) (any, error) { return s.dagStore.ListDAGs(ctx, ...) })
func dashList[P any](key string, store any, query func(ctx context.Context, p P) (any, error)) Handler {
	return typedHandler(func(_ context.Context, p P) (any, error) {
		if isNilStore(store) {
			return map[string]any{key: []any{}}, nil
		}
		ctx, cancel := dashCtx()
		defer cancel()
		list, err := query(ctx, p)
		if err != nil {
			slog.Warn("dashboard/"+key+" failed", "error", err)
			return map[string]any{key: []any{}}, nil
		}
		return map[string]any{key: list}, nil
	})
}

// ========================================
// § 12. Dashboard 数据查询
// ========================================

// dashAgentStatus 查询所有 Agent 状态。
func (s *Server) dashAgentStatus(_ context.Context, params json.RawMessage) (any, error) {
	if s.agentStatusStore == nil {
		return map[string]any{"agents": []any{}}, nil
	}
	var p struct {
		Status string `json:"status"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.agentStatusStore.List(ctx, p.Status)
	if err != nil {
		slog.Warn("dashboard/agentStatus failed", "error", err)
		return map[string]any{"agents": []any{}}, nil
	}
	return map[string]any{"agents": list}, nil
}

// dashDAGs 查询 DAG 列表。
func (s *Server) dashDAGs(_ context.Context, params json.RawMessage) (any, error) {
	if s.dagStore == nil {
		return map[string]any{"dags": []any{}}, nil
	}
	var p struct {
		Keyword string `json:"keyword"`
		Status  string `json:"status"`
		Limit   int    `json:"limit"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 100
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.dagStore.ListDAGs(ctx, p.Keyword, p.Status, p.Limit)
	if err != nil {
		slog.Warn("dashboard/dags failed", "error", err)
		return map[string]any{"dags": []any{}}, nil
	}
	return map[string]any{"dags": list}, nil
}

// dashTaskAcks 查询任务工单列表。
func (s *Server) dashTaskAcks(_ context.Context, params json.RawMessage) (any, error) {
	if s.taskAckStore == nil {
		return map[string]any{"acks": []any{}}, nil
	}
	var p struct {
		Keyword    string `json:"keyword"`
		Status     string `json:"status"`
		Priority   string `json:"priority"`
		AssignedTo string `json:"assignedTo"`
		Limit      int    `json:"limit"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 100
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.taskAckStore.List(ctx, p.Keyword, p.Status, p.Priority, p.AssignedTo, p.Limit)
	if err != nil {
		slog.Warn("dashboard/taskAcks failed", "error", err)
		return map[string]any{"acks": []any{}}, nil
	}
	return map[string]any{"acks": list}, nil
}

// dashTaskTraces 查询任务追踪列表。
func (s *Server) dashTaskTraces(_ context.Context, params json.RawMessage) (any, error) {
	if s.taskTraceStore == nil {
		return map[string]any{"traces": []any{}}, nil
	}
	var p struct {
		AgentID string `json:"agentId"`
		Keyword string `json:"keyword"`
		Limit   int    `json:"limit"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 100
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.taskTraceStore.List(ctx, p.AgentID, p.Keyword, nil, p.Limit)
	if err != nil {
		slog.Warn("dashboard/taskTraces failed", "error", err)
		return map[string]any{"traces": []any{}}, nil
	}
	return map[string]any{"traces": list}, nil
}

// dashCommandCards 查询命令卡列表。
func (s *Server) dashCommandCards(_ context.Context, params json.RawMessage) (any, error) {
	if s.cmdStore == nil {
		return map[string]any{"cards": []any{}}, nil
	}
	var p struct {
		Keyword string `json:"keyword"`
		Limit   int    `json:"limit"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 100
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.cmdStore.List(ctx, p.Keyword, p.Limit)
	if err != nil {
		slog.Warn("dashboard/commandCards failed", "error", err)
		return map[string]any{"cards": []any{}}, nil
	}
	return map[string]any{"cards": list}, nil
}

// dashPrompts 查询提示词模板列表。
func (s *Server) dashPrompts(_ context.Context, params json.RawMessage) (any, error) {
	if s.promptStore == nil {
		return map[string]any{"prompts": []any{}}, nil
	}
	var p struct {
		AgentKey string `json:"agentKey"`
		Keyword  string `json:"keyword"`
		Limit    int    `json:"limit"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 100
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.promptStore.List(ctx, p.AgentKey, p.Keyword, p.Limit)
	if err != nil {
		slog.Warn("dashboard/prompts failed", "error", err)
		return map[string]any{"prompts": []any{}}, nil
	}
	return map[string]any{"prompts": list}, nil
}

// dashSharedFiles 查询共享文件列表。
func (s *Server) dashSharedFiles(_ context.Context, params json.RawMessage) (any, error) {
	if s.fileStore == nil {
		return map[string]any{"files": []any{}}, nil
	}
	var p struct {
		Prefix string `json:"prefix"`
		Limit  int    `json:"limit"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 500
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.fileStore.List(ctx, p.Prefix, p.Limit)
	if err != nil {
		slog.Warn("dashboard/sharedFiles failed", "error", err)
		return map[string]any{"files": []any{}}, nil
	}
	return map[string]any{"files": list}, nil
}

// dashSkills 扫描 .agent/skills/ 目录。
func (s *Server) dashSkills(_ context.Context, _ json.RawMessage) (any, error) {
	if s.skillSvc == nil {
		return map[string]any{"skills": []any{}}, nil
	}
	list, err := s.skillSvc.ListSkills()
	if err != nil {
		slog.Warn("dashboard/skills failed", "error", err)
		return map[string]any{"skills": []any{}}, nil
	}
	return map[string]any{"skills": list}, nil
}

// dashAuditLogs 查询审计日志。
func (s *Server) dashAuditLogs(_ context.Context, params json.RawMessage) (any, error) {
	if s.auditLogStore == nil {
		return map[string]any{"logs": []any{}}, nil
	}
	var p struct {
		EventType string `json:"eventType"`
		Action    string `json:"action"`
		Actor     string `json:"actor"`
		Keyword   string `json:"keyword"`
		Limit     int    `json:"limit"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 100
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.auditLogStore.List(ctx, p.EventType, p.Action, p.Actor, p.Keyword, p.Limit)
	if err != nil {
		slog.Warn("dashboard/auditLogs failed", "error", err)
		return map[string]any{"logs": []any{}}, nil
	}
	return map[string]any{"logs": list}, nil
}

// dashAILogs 查询 AI 调用日志。
func (s *Server) dashAILogs(_ context.Context, params json.RawMessage) (any, error) {
	if s.aiLogStore == nil {
		return map[string]any{"logs": []any{}}, nil
	}
	var p struct {
		Category string `json:"category"`
		Keyword  string `json:"keyword"`
		Limit    int    `json:"limit"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 100
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.aiLogStore.Query(ctx, p.Category, p.Keyword, p.Limit)
	if err != nil {
		slog.Warn("dashboard/aiLogs failed", "error", err)
		return map[string]any{"logs": []any{}}, nil
	}
	return map[string]any{"logs": list}, nil
}

// dashBusLogs 查询总线异常日志。
func (s *Server) dashBusLogs(_ context.Context, params json.RawMessage) (any, error) {
	if s.busLogStore == nil {
		return map[string]any{"logs": []any{}}, nil
	}
	var p struct {
		Category string `json:"category"`
		Severity string `json:"severity"`
		Keyword  string `json:"keyword"`
		Limit    int    `json:"limit"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 100
	}
	ctx, cancel := dashCtx()
	defer cancel()
	list, err := s.busLogStore.List(ctx, p.Category, p.Severity, p.Keyword, p.Limit)
	if err != nil {
		slog.Warn("dashboard/busLogs failed", "error", err)
		return map[string]any{"logs": []any{}}, nil
	}
	return map[string]any{"logs": list}, nil
}

// ========================================
// Dashboard 详情方法
// ========================================

// dashDAGDetail 查询 DAG 详情 (含节点)。
func (s *Server) dashDAGDetail(_ context.Context, params json.RawMessage) (any, error) {
	if s.dagStore == nil {
		return nil, fmt.Errorf("dag store not initialized")
	}
	var p struct {
		DAGKey string `json:"dagKey"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.DAGKey == "" {
		return nil, fmt.Errorf("dagKey is required")
	}
	ctx, cancel := dashCtx()
	defer cancel()
	dag, nodes, err := s.dagStore.GetDAGDetail(ctx, p.DAGKey)
	if err != nil {
		return nil, fmt.Errorf("dashboard/dagDetail: %w", err)
	}
	return map[string]any{"dag": dag, "nodes": nodes}, nil
}
