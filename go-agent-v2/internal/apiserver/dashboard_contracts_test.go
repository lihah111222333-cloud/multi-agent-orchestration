package apiserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

func TestDashboardContractsStable(t *testing.T) {
	t.Run("dashboard list methods keep stable empty shape", func(t *testing.T) {
		s := &Server{methods: make(map[string]Handler)}
		s.registerDashboardMethods()

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
			h, ok := s.methods[tc.method]
			if !ok {
				t.Fatalf("missing method: %s", tc.method)
			}
			got, err := h(context.Background(), json.RawMessage(`{}`))
			if err != nil {
				t.Fatalf("%s returned error: %v", tc.method, err)
			}
			requireEmptyListField(t, got, tc.field)
		}
	})

	t.Run("dashboard dag detail keeps stable error semantics", func(t *testing.T) {
		t.Run("store missing", func(t *testing.T) {
			s := &Server{methods: make(map[string]Handler)}
			s.registerDashboardMethods()

			h := s.methods["dashboard/dagDetail"]
			got, err := h(context.Background(), json.RawMessage(`{}`))
			if got != nil {
				t.Fatalf("unexpected result: %#v", got)
			}
			requireErrContains(t, err, "dag store not initialized")
		})

		t.Run("dagKey required", func(t *testing.T) {
			s := &Server{
				methods:  make(map[string]Handler),
				dagStore: &store.TaskDAGStore{},
			}
			s.registerDashboardMethods()

			h := s.methods["dashboard/dagDetail"]
			got, err := h(context.Background(), json.RawMessage(`{}`))
			if got != nil {
				t.Fatalf("unexpected result: %#v", got)
			}
			requireErrContains(t, err, "dagKey is required")
		})

		t.Run("unmarshal params", func(t *testing.T) {
			s := &Server{
				methods:  make(map[string]Handler),
				dagStore: &store.TaskDAGStore{},
			}
			s.registerDashboardMethods()

			h := s.methods["dashboard/dagDetail"]
			got, err := h(context.Background(), json.RawMessage(`{`))
			if got != nil {
				t.Fatalf("unexpected result: %#v", got)
			}
			requireErrContains(t, err, "unmarshal params")
		})
	})

	t.Run("ui dashboard keeps stable empty payload shape", func(t *testing.T) {
		s := &Server{methods: make(map[string]Handler)}
		s.registerDashboardMethods()

		out, err := s.uiDashboardGet(context.Background(), uiDashboardGetParams{Page: "unknown"})
		if err != nil {
			t.Fatalf("uiDashboardGet returned error: %v", err)
		}

		fields := []string{
			"agents",
			"dags",
			"taskAcks",
			"taskTraces",
			"skills",
			"commandCards",
			"prompts",
			"memory",
		}
		for _, field := range fields {
			requireEmptyListField(t, out, field)
		}
	})
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
