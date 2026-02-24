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

func listHandler[P any](logKey, responseKey string, query func(ctx context.Context, p P) (any, bool, error)) MethodHandler {
	return typedMethod(func(_ context.Context, p P) (any, error) {
		ctx, cancel := dashCtx()
		defer cancel()

		list, ok, err := query(ctx, p)
		if !ok {
			return map[string]any{responseKey: []any{}}, nil
		}
		if err != nil {
			logger.Warn("dashboard/"+logKey+" failed", logger.FieldError, err)
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
func Register(register RegisterFn, provider DashboardProvider, caller MethodCaller) {
	if register == nil {
		return
	}
	_ = caller

	register("dashboard/agentStatus", listHandler("agents", "agents",
		func(ctx context.Context, p dashAgentStatusParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.ListAgentStatus(ctx, p.Status)
		}))

	register("dashboard/dags", listHandler("dags", "dags",
		func(ctx context.Context, p dashDAGParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.ListDAGs(ctx, p.Keyword, p.Status, clampLimit(p.Limit, 100))
		}))

	register("dashboard/taskAcks", listHandler("acks", "acks",
		func(ctx context.Context, p dashTaskAckParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.ListTaskAcks(ctx, p.Keyword, p.Status, p.Priority, p.AssignedTo, clampLimit(p.Limit, 100))
		}))

	register("dashboard/taskTraces", listHandler("traces", "traces",
		func(ctx context.Context, p dashTaskTraceParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.ListTaskTraces(ctx, p.AgentID, p.Keyword, nil, clampLimit(p.Limit, 100))
		}))

	register("dashboard/commandCards", listHandler("cards", "cards",
		func(ctx context.Context, p dashCommandCardParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.ListCommandCards(ctx, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/prompts", listHandler("prompts", "prompts",
		func(ctx context.Context, p dashPromptParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.ListPrompts(ctx, p.AgentKey, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/sharedFiles", listHandler("files", "files",
		func(ctx context.Context, p dashSharedFileParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.ListSharedFiles(ctx, p.Prefix, clampLimit(p.Limit, 500))
		}))

	register("dashboard/auditLogs", listHandler("logs", "logs",
		func(ctx context.Context, p dashAuditLogParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.ListAuditLogs(ctx, p.EventType, p.Action, p.Actor, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/aiLogs", listHandler("logs", "logs",
		func(ctx context.Context, p dashAILogParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.QueryAILogs(ctx, p.Category, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/busLogs", listHandler("logs", "logs",
		func(ctx context.Context, p dashBusLogParams) (any, bool, error) {
			if provider == nil {
				return nil, false, nil
			}
			return provider.ListBusLogs(ctx, p.Category, p.Severity, p.Keyword, clampLimit(p.Limit, 100))
		}))

	register("dashboard/skills", func(_ context.Context, _ json.RawMessage) (any, error) {
		if provider == nil {
			return map[string]any{"skills": []any{}}, nil
		}
		list, ok, err := provider.ListSkills()
		if !ok {
			return map[string]any{"skills": []any{}}, nil
		}
		if err != nil {
			logger.Warn("dashboard/skills failed", logger.FieldError, err)
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
