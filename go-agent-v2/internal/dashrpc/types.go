package dashrpc

import (
	"context"
	"encoding/json"
	"time"
)

// MethodHandler handles one JSON-RPC method call.
type MethodHandler func(ctx context.Context, params json.RawMessage) (any, error)

// RegisterFn receives callback-based method registration.
type RegisterFn func(name string, h MethodHandler)

// MethodCaller calls an already-registered JSON-RPC method.
type MethodCaller func(ctx context.Context, method string, params json.RawMessage) (any, error)

// DashboardProvider exposes dashboard data via callback injection.
type DashboardProvider interface {
	HasDAGStore() bool
	ListAgentStatus(ctx context.Context, status string) (list any, ok bool, err error)
	ListDAGs(ctx context.Context, keyword, status string, limit int) (list any, ok bool, err error)
	ListTaskAcks(ctx context.Context, keyword, status, priority, assignedTo string, limit int) (list any, ok bool, err error)
	ListTaskTraces(ctx context.Context, agentID, keyword string, since *time.Time, limit int) (list any, ok bool, err error)
	ListCommandCards(ctx context.Context, keyword string, limit int) (list any, ok bool, err error)
	ListPrompts(ctx context.Context, agentKey, keyword string, limit int) (list any, ok bool, err error)
	ListSharedFiles(ctx context.Context, prefix string, limit int) (list any, ok bool, err error)
	ListAuditLogs(ctx context.Context, eventType, action, actor, keyword string, limit int) (list any, ok bool, err error)
	QueryAILogs(ctx context.Context, category, keyword string, limit int) (list any, ok bool, err error)
	ListBusLogs(ctx context.Context, category, severity, keyword string, limit int) (list any, ok bool, err error)
	ListSkills() (list any, ok bool, err error)
	GetDAGDetail(ctx context.Context, dagKey string) (dag any, nodes any, ok bool, err error)
}

// UIGetParams is ui/dashboard/get request payload.
type UIGetParams struct {
	Page string `json:"page"`
}
