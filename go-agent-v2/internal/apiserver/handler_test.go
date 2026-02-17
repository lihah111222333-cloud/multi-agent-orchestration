// handler_test.go — typedHandler 泛型参数解析测试 (TDD RED→GREEN)。
package apiserver

import (
	"context"
	"encoding/json"
	"testing"
)

// TestTypedHandler_ValidParams 验证参数正确解析。
func TestTypedHandler_ValidParams(t *testing.T) {
	type params struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	h := typedHandler(func(_ context.Context, p params) (any, error) {
		return map[string]any{"echo": p.Name, "v": p.Value}, nil
	})

	raw := json.RawMessage(`{"name":"hello","value":42}`)
	got, err := h(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", got)
	}
	if m["echo"] != "hello" {
		t.Errorf("echo = %v, want %q", m["echo"], "hello")
	}
	if m["v"] != 42 {
		t.Errorf("v = %v, want 42", m["v"])
	}
}

// TestTypedHandler_InvalidJSON 无效 JSON 应返回错误。
func TestTypedHandler_InvalidJSON(t *testing.T) {
	type params struct {
		Name string `json:"name"`
	}

	h := typedHandler(func(_ context.Context, p params) (any, error) {
		return nil, nil
	})

	raw := json.RawMessage(`{invalid}`)
	_, err := h(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestTypedHandler_NilParams nil 参数应使用零值 struct。
func TestTypedHandler_NilParams(t *testing.T) {
	type params struct {
		Limit int `json:"limit"`
	}

	h := typedHandler(func(_ context.Context, p params) (any, error) {
		return map[string]any{"limit": p.Limit}, nil
	})

	got, err := h(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := got.(map[string]any)
	if m["limit"] != 0 {
		t.Errorf("limit = %v, want 0 (zero value)", m["limit"])
	}
}

// TestTypedHandler_EmptyJSON 空 JSON {} 应使用零值 struct。
func TestTypedHandler_EmptyJSON(t *testing.T) {
	type params struct {
		Name string `json:"name"`
	}

	h := typedHandler(func(_ context.Context, p params) (any, error) {
		return map[string]any{"name": p.Name}, nil
	})

	raw := json.RawMessage(`{}`)
	got, err := h(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := got.(map[string]any)
	if m["name"] != "" {
		t.Errorf("name = %v, want empty string", m["name"])
	}
}

// TestTypedHandler_PartialParams 仅部分字段提供时其余用零值。
func TestTypedHandler_PartialParams(t *testing.T) {
	type params struct {
		Name  string `json:"name"`
		Limit int    `json:"limit"`
	}

	h := typedHandler(func(_ context.Context, p params) (any, error) {
		return map[string]any{"name": p.Name, "limit": p.Limit}, nil
	})

	raw := json.RawMessage(`{"name":"test"}`)
	got, err := h(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := got.(map[string]any)
	if m["name"] != "test" {
		t.Errorf("name = %v, want %q", m["name"], "test")
	}
	if m["limit"] != 0 {
		t.Errorf("limit = %v, want 0", m["limit"])
	}
}

// TestNoopHandler_ReturnsEmptyMap 验证 noopHandler 返回非 nil 空 map。
func TestNoopHandler_ReturnsEmptyMap(t *testing.T) {
	h := noopHandler()
	got, err := h(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", got)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}
