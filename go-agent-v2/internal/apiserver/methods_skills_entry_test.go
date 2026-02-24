package apiserver

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSkillsContractsStable(t *testing.T) {
	s := &Server{methods: make(map[string]Handler)}
	s.registerMethods()

	requiredMethods := []string{
		"skills/list",
		"skills/local/read",
		"skills/local/importDir",
		"skills/local/delete",
		"skills/remote/read",
		"skills/remote/write",
		"skills/config/read",
		"skills/config/write",
		"skills/summary/write",
		"skills/match/preview",
		"app/list",
	}
	for _, method := range requiredMethods {
		if _, ok := s.methods[method]; !ok {
			t.Fatalf("missing method: %s", method)
		}
	}

	{
		out, err := s.methods["skills/list"](context.Background(), nil)
		if err != nil {
			t.Fatalf("skills/list returned error: %v", err)
		}
		requireSliceFieldLen(t, out, "skills", 0)
	}
	{
		out, err := s.methods["app/list"](context.Background(), nil)
		if err != nil {
			t.Fatalf("app/list returned error: %v", err)
		}
		requireSliceFieldLen(t, out, "apps", 0)
	}
	{
		out, err := s.methods["skills/config/read"](context.Background(), json.RawMessage(`{}`))
		if out != nil {
			t.Fatalf("skills/config/read expected nil result on invalid params, got: %#v", out)
		}
		if err == nil || !strings.Contains(err.Error(), "agent_id is required") {
			t.Fatalf("skills/config/read error mismatch, got: %v", err)
		}
	}
	{
		out, err := s.methods["skills/match/preview"](context.Background(), json.RawMessage(`{"threadId":"thread-1","text":"hello"}`))
		if err != nil {
			t.Fatalf("skills/match/preview returned error: %v", err)
		}
		requireStringField(t, out, "thread_id", "thread-1")
		requireSliceFieldLen(t, out, "matches", 0)
	}
}

func requireSliceFieldLen(t *testing.T, out any, field string, wantLen int) {
	t.Helper()
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("result is not map[string]any: %#v", out)
	}
	v, ok := m[field]
	if !ok {
		t.Fatalf("missing field %q in result: %#v", field, out)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		t.Fatalf("field %q is not slice, got %T", field, v)
	}
	if rv.Len() != wantLen {
		t.Fatalf("field %q expected len=%d, got len=%d", field, wantLen, rv.Len())
	}
}

func requireStringField(t *testing.T, out any, field, want string) {
	t.Helper()
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("result is not map[string]any: %#v", out)
	}
	v, ok := m[field]
	if !ok {
		t.Fatalf("missing field %q in result: %#v", field, out)
	}
	got, ok := v.(string)
	if !ok {
		t.Fatalf("field %q is not string, got %T", field, v)
	}
	if got != want {
		t.Fatalf("field %q expected %q, got %q", field, want, got)
	}
}
