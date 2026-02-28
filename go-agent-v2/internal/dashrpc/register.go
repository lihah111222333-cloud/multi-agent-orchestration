package dashrpc

import (
	"context"
	"encoding/json"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func dashCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func clampLimit(v, defaultVal int) int {
	if v <= 0 || v > 2000 {
		return defaultVal
	}
	return v
}

func typedMethod[P any](fn func(ctx context.Context, p P) (any, error)) MethodHandler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p P
		if raw != nil {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, apperrors.Wrap(err, "TypedHandler", "invalid params")
			}
		}
		return fn(ctx, p)
	}
}

func listHandler[P any](provider DashboardProvider, logKey, responseKey string, query func(ctx context.Context, provider DashboardProvider, p P) (any, bool, error)) MethodHandler {
	return typedMethod(func(_ context.Context, p P) (any, error) {
		if provider == nil {
			return map[string]any{responseKey: []any{}}, nil
		}

		ctx, cancel := dashCtx()
		defer cancel()

		list, ok, err := query(ctx, provider, p)
		if !ok || err != nil {
			if err != nil {
				logger.Warn("dashboard/"+logKey+" failed", logger.FieldError, err)
			}
			return map[string]any{responseKey: []any{}}, nil
		}
		return map[string]any{responseKey: list}, nil
	})
}

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

// Register registers all dashboard/* methods via callback injection.
func Register(register RegisterFn, provider DashboardProvider, _ MethodCaller) {
	if register == nil {
		return
	}

	register("dashboard/agentStatus", listHandler(provider, "agents", "agents",
		func(ctx context.Context, dp DashboardProvider, p dashAgentStatusParams) (any, bool, error) {
			return dp.ListAgentStatus(ctx, p.Status)
		}))

	register("dashboard/dags", listHandler(provider, "dags", "dags",
		func(ctx context.Context, dp DashboardProvider, p dashDAGParams) (any, bool, error) {
			return dp.ListDAGs(ctx, p.Keyword, p.Status, clampLimit(p.Limit, 100))
		}))

	register("dashboard/taskAcks", listHandler(provider, "acks", "acks",
		func(ctx context.Context, dp DashboardProvider, p dashTaskAckParams) (any, bool, error) {
			return dp.ListTaskAcks(ctx, p.Keyword, p.Status, p.Priority, p.AssignedTo, clampLimit(p.Limit, 100))
		}))

	register("dashboard/taskTraces", listHandler(provider, "traces", "traces",
		func(ctx context.Context, dp DashboardProvider, p dashTaskTraceParams) (any, bool, error) {
			return dp.ListTaskTraces(ctx, p.AgentID, p.Keyword, nil, clampLimit(p.Limit, 100))
		}))

	register("dashboard/commandCards", listHandler(provider, "cards", "cards",
		func(ctx context.Context, dp DashboardProvider, p dashCommandCardParams) (any, bool, error) {
			return dp.ListCommandCards(ctx, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/prompts", listHandler(provider, "prompts", "prompts",
		func(ctx context.Context, dp DashboardProvider, p dashPromptParams) (any, bool, error) {
			return dp.ListPrompts(ctx, p.AgentKey, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/sharedFiles", listHandler(provider, "files", "files",
		func(ctx context.Context, dp DashboardProvider, p dashSharedFileParams) (any, bool, error) {
			return dp.ListSharedFiles(ctx, p.Prefix, clampLimit(p.Limit, 500))
		}))

	register("dashboard/auditLogs", listHandler(provider, "logs", "logs",
		func(ctx context.Context, dp DashboardProvider, p dashAuditLogParams) (any, bool, error) {
			return dp.ListAuditLogs(ctx, p.EventType, p.Action, p.Actor, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/aiLogs", listHandler(provider, "logs", "logs",
		func(ctx context.Context, dp DashboardProvider, p dashAILogParams) (any, bool, error) {
			return dp.QueryAILogs(ctx, p.Category, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/busLogs", listHandler(provider, "logs", "logs",
		func(ctx context.Context, dp DashboardProvider, p dashBusLogParams) (any, bool, error) {
			return dp.ListBusLogs(ctx, p.Category, p.Severity, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/skills", func(_ context.Context, _ json.RawMessage) (any, error) {
		if provider == nil {
			return map[string]any{"skills": []any{}}, nil
		}
		list, ok, err := provider.ListSkills()
		if !ok || err != nil {
			if err != nil {
				logger.Warn("dashboard/skills failed", logger.FieldError, err)
			}
			return map[string]any{"skills": []any{}}, nil
		}
		return map[string]any{"skills": list}, nil
	})

	register("dashboard/dagDetail", dagDetailHandler(provider))
}

func dagDetailHandler(provider DashboardProvider) MethodHandler {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		if provider == nil || !provider.HasDAGStore() {
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
		dag, nodes, ok, err := provider.GetDAGDetail(ctx, p.DAGKey)
		if !ok {
			return nil, apperrors.New("Server.dashDAGDetail", "dag store not initialized")
		}
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.dashDAGDetail", "get DAG detail")
		}
		return map[string]any{"dag": dag, "nodes": nodes}, nil
	}
}
