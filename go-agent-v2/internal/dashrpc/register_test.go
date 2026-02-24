package dashrpc

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

type stubProvider struct {
	hasDAGStore bool

	listAgentStatusFn  func(ctx context.Context, status string) (any, bool, error)
	listDAGsFn         func(ctx context.Context, keyword, status string, limit int) (any, bool, error)
	listTaskAcksFn     func(ctx context.Context, keyword, status, priority, assignedTo string, limit int) (any, bool, error)
	listTaskTracesFn   func(ctx context.Context, agentID, keyword string, since *time.Time, limit int) (any, bool, error)
	listCommandCardsFn func(ctx context.Context, keyword string, limit int) (any, bool, error)
	listPromptsFn      func(ctx context.Context, agentKey, keyword string, limit int) (any, bool, error)
	listSharedFilesFn  func(ctx context.Context, prefix string, limit int) (any, bool, error)
	listAuditLogsFn    func(ctx context.Context, eventType, action, actor, keyword string, limit int) (any, bool, error)
	queryAILogsFn      func(ctx context.Context, category, keyword string, limit int) (any, bool, error)
	listBusLogsFn      func(ctx context.Context, category, severity, keyword string, limit int) (any, bool, error)
	listSkillsFn       func() (any, bool, error)
	getDAGDetailFn     func(ctx context.Context, dagKey string) (any, any, bool, error)
}

func (p stubProvider) HasDAGStore() bool {
	return p.hasDAGStore
}

func (p stubProvider) ListAgentStatus(ctx context.Context, status string) (any, bool, error) {
	if p.listAgentStatusFn == nil {
		return nil, false, nil
	}
	return p.listAgentStatusFn(ctx, status)
}

func (p stubProvider) ListDAGs(ctx context.Context, keyword, status string, limit int) (any, bool, error) {
	if p.listDAGsFn == nil {
		return nil, false, nil
	}
	return p.listDAGsFn(ctx, keyword, status, limit)
}

func (p stubProvider) ListTaskAcks(ctx context.Context, keyword, status, priority, assignedTo string, limit int) (any, bool, error) {
	if p.listTaskAcksFn == nil {
		return nil, false, nil
	}
	return p.listTaskAcksFn(ctx, keyword, status, priority, assignedTo, limit)
}

func (p stubProvider) ListTaskTraces(ctx context.Context, agentID, keyword string, since *time.Time, limit int) (any, bool, error) {
	if p.listTaskTracesFn == nil {
		return nil, false, nil
	}
	return p.listTaskTracesFn(ctx, agentID, keyword, since, limit)
}

func (p stubProvider) ListCommandCards(ctx context.Context, keyword string, limit int) (any, bool, error) {
	if p.listCommandCardsFn == nil {
		return nil, false, nil
	}
	return p.listCommandCardsFn(ctx, keyword, limit)
}

func (p stubProvider) ListPrompts(ctx context.Context, agentKey, keyword string, limit int) (any, bool, error) {
	if p.listPromptsFn == nil {
		return nil, false, nil
	}
	return p.listPromptsFn(ctx, agentKey, keyword, limit)
}

func (p stubProvider) ListSharedFiles(ctx context.Context, prefix string, limit int) (any, bool, error) {
	if p.listSharedFilesFn == nil {
		return nil, false, nil
	}
	return p.listSharedFilesFn(ctx, prefix, limit)
}

func (p stubProvider) ListAuditLogs(ctx context.Context, eventType, action, actor, keyword string, limit int) (any, bool, error) {
	if p.listAuditLogsFn == nil {
		return nil, false, nil
	}
	return p.listAuditLogsFn(ctx, eventType, action, actor, keyword, limit)
}

func (p stubProvider) QueryAILogs(ctx context.Context, category, keyword string, limit int) (any, bool, error) {
	if p.queryAILogsFn == nil {
		return nil, false, nil
	}
	return p.queryAILogsFn(ctx, category, keyword, limit)
}

func (p stubProvider) ListBusLogs(ctx context.Context, category, severity, keyword string, limit int) (any, bool, error) {
	if p.listBusLogsFn == nil {
		return nil, false, nil
	}
	return p.listBusLogsFn(ctx, category, severity, keyword, limit)
}

func (p stubProvider) ListSkills() (any, bool, error) {
	if p.listSkillsFn == nil {
		return nil, false, nil
	}
	return p.listSkillsFn()
}

func (p stubProvider) GetDAGDetail(ctx context.Context, dagKey string) (any, any, bool, error) {
	if p.getDAGDetailFn == nil {
		return nil, nil, false, nil
	}
	return p.getDAGDetailFn(ctx, dagKey)
}

func TestRegisterRegistersDashboardMethods(t *testing.T) {
	methods := make(map[string]MethodHandler)
	Register(func(name string, h MethodHandler) {
		methods[name] = h
	}, stubProvider{}, nil)

	expected := []string{
		"dashboard/agentStatus",
		"dashboard/dags",
		"dashboard/taskAcks",
		"dashboard/taskTraces",
		"dashboard/commandCards",
		"dashboard/prompts",
		"dashboard/sharedFiles",
		"dashboard/auditLogs",
		"dashboard/aiLogs",
		"dashboard/busLogs",
		"dashboard/skills",
		"dashboard/dagDetail",
	}

	if len(methods) != len(expected) {
		actual := make([]string, 0, len(methods))
		for name := range methods {
			actual = append(actual, name)
		}
		sort.Strings(actual)
		t.Fatalf("registered method count mismatch: got=%d want=%d actual=%v", len(methods), len(expected), actual)
	}

	for _, name := range expected {
		h, ok := methods[name]
		if !ok {
			t.Fatalf("missing method registration: %s", name)
		}
		if h == nil {
			t.Fatalf("method handler is nil: %s", name)
		}
	}
}

func TestRegisterListMethodsStableEmptyShape(t *testing.T) {
	methods := make(map[string]MethodHandler)
	Register(func(name string, h MethodHandler) {
		methods[name] = h
	}, nil, nil)

	cases := []struct {
		method string
		field  string
	}{
		{method: "dashboard/agentStatus", field: "agents"},
		{method: "dashboard/dags", field: "dags"},
		{method: "dashboard/taskAcks", field: "acks"},
		{method: "dashboard/taskTraces", field: "traces"},
		{method: "dashboard/commandCards", field: "cards"},
		{method: "dashboard/prompts", field: "prompts"},
		{method: "dashboard/sharedFiles", field: "files"},
		{method: "dashboard/auditLogs", field: "logs"},
		{method: "dashboard/aiLogs", field: "logs"},
		{method: "dashboard/busLogs", field: "logs"},
		{method: "dashboard/skills", field: "skills"},
	}

	for _, tc := range cases {
		h := methods[tc.method]
		got, err := h(context.Background(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s returned error: %v", tc.method, err)
		}
		requireEmptyListField(t, got, tc.field)
	}
}

func TestRegisterListMethodErrorFallsBackToEmpty(t *testing.T) {
	methods := make(map[string]MethodHandler)
	provider := stubProvider{
		listAgentStatusFn: func(_ context.Context, _ string) (any, bool, error) {
			return nil, true, errors.New("boom")
		},
	}
	Register(func(name string, h MethodHandler) {
		methods[name] = h
	}, provider, nil)

	got, err := methods["dashboard/agentStatus"](context.Background(), json.RawMessage(`{"status":"running"}`))
	if err != nil {
		t.Fatalf("dashboard/agentStatus returned error: %v", err)
	}
	requireEmptyListField(t, got, "agents")
}

func TestDagDetailErrorSemantics(t *testing.T) {
	t.Run("store missing", func(t *testing.T) {
		methods := make(map[string]MethodHandler)
		Register(func(name string, h MethodHandler) {
			methods[name] = h
		}, stubProvider{hasDAGStore: false}, nil)

		got, err := methods["dashboard/dagDetail"](context.Background(), json.RawMessage(`{}`))
		if got != nil {
			t.Fatalf("unexpected result: %#v", got)
		}
		requireErrContains(t, err, "dag store not initialized")
	})

	t.Run("unmarshal params", func(t *testing.T) {
		methods := make(map[string]MethodHandler)
		Register(func(name string, h MethodHandler) {
			methods[name] = h
		}, stubProvider{hasDAGStore: true}, nil)

		got, err := methods["dashboard/dagDetail"](context.Background(), json.RawMessage(`{`))
		if got != nil {
			t.Fatalf("unexpected result: %#v", got)
		}
		requireErrContains(t, err, "unmarshal params")
	})

	t.Run("dagKey required", func(t *testing.T) {
		methods := make(map[string]MethodHandler)
		Register(func(name string, h MethodHandler) {
			methods[name] = h
		}, stubProvider{hasDAGStore: true}, nil)

		got, err := methods["dashboard/dagDetail"](context.Background(), json.RawMessage(`{}`))
		if got != nil {
			t.Fatalf("unexpected result: %#v", got)
		}
		requireErrContains(t, err, "dagKey is required")
	})

	t.Run("query failed", func(t *testing.T) {
		methods := make(map[string]MethodHandler)
		provider := stubProvider{
			hasDAGStore: true,
			getDAGDetailFn: func(_ context.Context, _ string) (any, any, bool, error) {
				return nil, nil, true, errors.New("lookup failed")
			},
		}
		Register(func(name string, h MethodHandler) {
			methods[name] = h
		}, provider, nil)

		got, err := methods["dashboard/dagDetail"](context.Background(), json.RawMessage(`{"dagKey":"k-1"}`))
		if got != nil {
			t.Fatalf("unexpected result: %#v", got)
		}
		requireErrContains(t, err, "get DAG detail")
	})
}

func TestUIDashboardGetStableEmptyShape(t *testing.T) {
	out, err := UIDashboardGet(context.Background(), nil, UIGetParams{Page: "unknown"})
	if err != nil {
		t.Fatalf("UIDashboardGet returned error: %v", err)
	}
	for _, field := range []string{"agents", "dags", "taskAcks", "taskTraces", "skills", "commandCards", "prompts", "memory"} {
		requireEmptyListField(t, out, field)
	}
}

func requireEmptyListField(t *testing.T, got any, field string) {
	t.Helper()
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("result is not map[string]any: %#v", got)
	}
	value, ok := m[field]
	if !ok {
		t.Fatalf("missing field %q in result: %#v", field, got)
	}
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("field %q is not []any, got %T", field, value)
	}
	if len(list) != 0 {
		t.Fatalf("field %q expected empty list, got len=%d", field, len(list))
	}
}

func requireErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %q", want, err.Error())
	}
}
