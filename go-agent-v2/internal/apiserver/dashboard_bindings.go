package apiserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/dashrpc"
)

type uiDashboardGetParams struct {
	Page string `json:"page"`
}

type dashboardProvider struct {
	s *Server
}

func (p dashboardProvider) callIfAvailable(available bool, call func() (any, error)) (any, bool, error) {
	if !available { return nil, false, nil }
	result, err := call(); return result, true, err
}

func (p dashboardProvider) HasDAGStore() bool { return p.s != nil && p.s.dagStore != nil }

func (p dashboardProvider) ListAgentStatus(ctx context.Context, status string) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.agentStatusStore != nil, func() (any, error) { return p.s.agentStatusStore.List(ctx, status) })
}
func (p dashboardProvider) ListDAGs(ctx context.Context, keyword, status string, limit int) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.dagStore != nil, func() (any, error) { return p.s.dagStore.ListDAGs(ctx, keyword, status, limit) })
}
func (p dashboardProvider) ListTaskAcks(ctx context.Context, keyword, status, priority, assignedTo string, limit int) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.taskAckStore != nil, func() (any, error) { return p.s.taskAckStore.List(ctx, keyword, status, priority, assignedTo, limit) })
}
func (p dashboardProvider) ListTaskTraces(ctx context.Context, agentID, keyword string, since *time.Time, limit int) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.taskTraceStore != nil, func() (any, error) { return p.s.taskTraceStore.List(ctx, agentID, keyword, since, limit) })
}
func (p dashboardProvider) ListCommandCards(ctx context.Context, keyword string, limit int) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.cmdStore != nil, func() (any, error) { return p.s.cmdStore.List(ctx, keyword, limit) })
}
func (p dashboardProvider) ListPrompts(ctx context.Context, agentKey, keyword string, limit int) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.promptStore != nil, func() (any, error) { return p.s.promptStore.List(ctx, agentKey, keyword, limit) })
}
func (p dashboardProvider) ListSharedFiles(ctx context.Context, prefix string, limit int) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.fileStore != nil, func() (any, error) { return p.s.fileStore.List(ctx, prefix, limit) })
}
func (p dashboardProvider) ListAuditLogs(ctx context.Context, eventType, action, actor, keyword string, limit int) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.auditLogStore != nil, func() (any, error) { return p.s.auditLogStore.List(ctx, eventType, action, actor, keyword, limit) })
}
func (p dashboardProvider) QueryAILogs(ctx context.Context, category, keyword string, limit int) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.aiLogStore != nil, func() (any, error) { return p.s.aiLogStore.Query(ctx, category, keyword, limit) })
}
func (p dashboardProvider) ListBusLogs(ctx context.Context, category, severity, keyword string, limit int) (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.busLogStore != nil, func() (any, error) { return p.s.busLogStore.List(ctx, category, severity, keyword, limit) })
}
func (p dashboardProvider) ListSkills() (any, bool, error) {
	return p.callIfAvailable(p.s != nil && p.s.skillSvc != nil, func() (any, error) { return p.s.skillSvc.ListSkills() })
}
func (p dashboardProvider) GetDAGDetail(ctx context.Context, dagKey string) (any, any, bool, error) {
	if p.s == nil || p.s.dagStore == nil { return nil, nil, false, nil }
	dag, nodes, err := p.s.dagStore.GetDAGDetail(ctx, dagKey); return dag, nodes, true, err
}

func dashboardMethodCaller(s *Server) dashrpc.MethodCaller {
	return func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if s == nil { return nil, nil }
		h, ok := s.methods[method]
		if !ok { return nil, nil }
		if ctx == nil { ctx = context.Background() }
		return h(ctx, params)
	}
}

func registerDashboardMethods(s *Server) {
	if s == nil { return }
	if s.methods == nil { s.methods = make(map[string]Handler) }
	dashrpc.Register(func(name string, h dashrpc.MethodHandler) {
		s.methods[name] = Handler(h)
	}, dashboardProvider{s: s}, dashboardMethodCaller(s))
}

func uiDashboardGet(s *Server, ctx context.Context, p uiDashboardGetParams) (any, error) {
	return dashrpc.UIDashboardGet(ctx, dashboardMethodCaller(s), dashrpc.UIGetParams{Page: p.Page})
}
