// dashboard_methods.go — Dashboard JSON-RPC 方法 (§ 12)。
//
// 替代 cmd/agent-terminal/dashboard.go 中的 Wails 绑定。
// 前端通过 App.CallAPI('dashboard/xxx', { limit: 100 }) 调用。
package apiserver

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// dashLimitParams 通用分页参数。
type dashLimitParams struct {
	Limit int `json:"limit"`
}

func parseDashLimit(params json.RawMessage, defaultLimit int) int {
	var p dashLimitParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			logger.Warn("dashboard: unmarshal limit params", logger.FieldError, err)
		}
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		return defaultLimit
	}
	return p.Limit
}

func dashCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// toolCtx 资源工具通用 5 秒超时上下文。
func toolCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
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
func dashList[P any](key string, store any, query func(ctx context.Context, p P) (any, error)) Handler {
	return typedHandler(func(_ context.Context, p P) (any, error) {
		if isNilStore(store) {
			return map[string]any{key: []any{}}, nil
		}
		ctx, cancel := dashCtx()
		defer cancel()
		list, err := query(ctx, p)
		if err != nil {
			logger.Warn("dashboard/"+key+" failed", logger.FieldError, err)
			return map[string]any{key: []any{}}, nil
		}
		return map[string]any{key: list}, nil
	})
}

// clampLimit 统一 dashboard 分页限制 (默认 defaultVal, 最大 2000)。
func clampLimit(v, defaultVal int) int {
	if v <= 0 || v > 2000 {
		return defaultVal
	}
	return v
}

// ========================================
// § 12. Dashboard 数据查询 — typed params
// ========================================

type dashAgentStatusParams struct {
	Status string `json:"status"`
}

type dashDAGParams struct {
	Keyword string `json:"keyword"`
	Status  string `json:"status"`
	Limit   int    `json:"limit"`
}

type dashTaskAckParams struct {
	Keyword    string `json:"keyword"`
	Status     string `json:"status"`
	Priority   string `json:"priority"`
	AssignedTo string `json:"assignedTo"`
	Limit      int    `json:"limit"`
}

type dashTaskTraceParams struct {
	AgentID string `json:"agentId"`
	Keyword string `json:"keyword"`
	Limit   int    `json:"limit"`
}

type dashCommandCardParams struct {
	Keyword string `json:"keyword"`
	Limit   int    `json:"limit"`
}

type dashPromptParams struct {
	AgentKey string `json:"agentKey"`
	Keyword  string `json:"keyword"`
	Limit    int    `json:"limit"`
}

type dashSharedFileParams struct {
	Prefix string `json:"prefix"`
	Limit  int    `json:"limit"`
}

type dashAuditLogParams struct {
	EventType string `json:"eventType"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Keyword   string `json:"keyword"`
	Limit     int    `json:"limit"`
}

type dashAILogParams struct {
	Category string `json:"category"`
	Keyword  string `json:"keyword"`
	Limit    int    `json:"limit"`
}

type dashBusLogParams struct {
	Category string `json:"category"`
	Severity string `json:"severity"`
	Keyword  string `json:"keyword"`
	Limit    int    `json:"limit"`
}

// ========================================
// § 12. Dashboard 方法注册
// ========================================

// registerDashboardMethods 注册所有 dashboard/* 方法。
func (s *Server) registerDashboardMethods() {
	// — 列表查询 (全部使用 dashList 模板) —

	s.methods["dashboard/agentStatus"] = dashList[dashAgentStatusParams]("agents", s.agentStatusStore,
		func(ctx context.Context, p dashAgentStatusParams) (any, error) {
			return s.agentStatusStore.List(ctx, p.Status)
		})

	s.methods["dashboard/dags"] = dashList[dashDAGParams]("dags", s.dagStore,
		func(ctx context.Context, p dashDAGParams) (any, error) {
			return s.dagStore.ListDAGs(ctx, p.Keyword, p.Status, clampLimit(p.Limit, 100))
		})

	s.methods["dashboard/taskAcks"] = dashList[dashTaskAckParams]("acks", s.taskAckStore,
		func(ctx context.Context, p dashTaskAckParams) (any, error) {
			return s.taskAckStore.List(ctx, p.Keyword, p.Status, p.Priority, p.AssignedTo, clampLimit(p.Limit, 100))
		})

	s.methods["dashboard/taskTraces"] = dashList[dashTaskTraceParams]("traces", s.taskTraceStore,
		func(ctx context.Context, p dashTaskTraceParams) (any, error) {
			return s.taskTraceStore.List(ctx, p.AgentID, p.Keyword, nil, clampLimit(p.Limit, 100))
		})

	s.methods["dashboard/commandCards"] = dashList[dashCommandCardParams]("cards", s.cmdStore,
		func(ctx context.Context, p dashCommandCardParams) (any, error) {
			return s.cmdStore.List(ctx, p.Keyword, clampLimit(p.Limit, 100))
		})

	s.methods["dashboard/prompts"] = dashList[dashPromptParams]("prompts", s.promptStore,
		func(ctx context.Context, p dashPromptParams) (any, error) {
			return s.promptStore.List(ctx, p.AgentKey, p.Keyword, clampLimit(p.Limit, 100))
		})

	s.methods["dashboard/sharedFiles"] = dashList[dashSharedFileParams]("files", s.fileStore,
		func(ctx context.Context, p dashSharedFileParams) (any, error) {
			return s.fileStore.List(ctx, p.Prefix, clampLimit(p.Limit, 500))
		})

	s.methods["dashboard/auditLogs"] = dashList[dashAuditLogParams]("logs", s.auditLogStore,
		func(ctx context.Context, p dashAuditLogParams) (any, error) {
			return s.auditLogStore.List(ctx, p.EventType, p.Action, p.Actor, p.Keyword, clampLimit(p.Limit, 100))
		})

	s.methods["dashboard/aiLogs"] = dashList[dashAILogParams]("logs", s.aiLogStore,
		func(ctx context.Context, p dashAILogParams) (any, error) {
			return s.aiLogStore.Query(ctx, p.Category, p.Keyword, clampLimit(p.Limit, 100))
		})

	s.methods["dashboard/busLogs"] = dashList[dashBusLogParams]("logs", s.busLogStore,
		func(ctx context.Context, p dashBusLogParams) (any, error) {
			return s.busLogStore.List(ctx, p.Category, p.Severity, p.Keyword, clampLimit(p.Limit, 100))
		})

	// — Skills (无 DB store, 不走 dashList) —

	s.methods["dashboard/skills"] = func(_ context.Context, _ json.RawMessage) (any, error) {
		if s.skillSvc == nil {
			return map[string]any{"skills": []any{}}, nil
		}
		list, err := s.skillSvc.ListSkills()
		if err != nil {
			logger.Warn("dashboard/skills failed", logger.FieldError, err)
			return map[string]any{"skills": []any{}}, nil
		}
		return map[string]any{"skills": list}, nil
	}

	// — DAG Detail (非列表, 不走 dashList) —

	s.methods["dashboard/dagDetail"] = s.dashDAGDetail
}

// ========================================
// Dashboard 详情方法
// ========================================

// dashDAGDetail 查询 DAG 详情 (含节点)。
func (s *Server) dashDAGDetail(_ context.Context, params json.RawMessage) (any, error) {
	if s.dagStore == nil {
		return nil, apperrors.New("Server.dashDAGDetail", "dag store not initialized")
	}
	var p struct {
		DAGKey string `json:"dagKey"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.dashDAGDetail", "unmarshal params")
	}
	if p.DAGKey == "" {
		return nil, apperrors.New("Server.dashDAGDetail", "dagKey is required")
	}
	ctx, cancel := dashCtx()
	defer cancel()
	dag, nodes, err := s.dagStore.GetDAGDetail(ctx, p.DAGKey)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.dashDAGDetail", "get DAG detail")
	}
	return map[string]any{"dag": dag, "nodes": nodes}, nil
}
