// dashboard_template_test.go — dashList 通用模板测试 (TDD RED→GREEN)。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// TestDashList_NilStore nil store 应返回空列表。
func TestDashList_NilStore(t *testing.T) {
	h := dashList[dashLimitParams]("cards", nil, func(_ context.Context, _ dashLimitParams) (any, error) {
		t.Fatal("query should not be called when store is nil")
		return nil, nil
	})

	got, err := h(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", got)
	}
	arr, ok := m["cards"].([]any)
	if !ok {
		t.Fatalf("expected []any for key 'cards', got %T", m["cards"])
	}
	if len(arr) != 0 {
		t.Errorf("expected empty array, got %v", arr)
	}
}

// TestDashList_TypedNilStore typed nil 指针也应返回空列表 (生产防护)。
//
// Go 中 (*SomeStore)(nil) 作为 any 参数传入时 store == nil 返回 false,
// 如果不做特殊处理会导致 nil pointer dereference panic。
func TestDashList_TypedNilStore(t *testing.T) {
	type fakeStore struct{}
	var nilStore *fakeStore // typed nil

	h := dashList[dashLimitParams]("items", nilStore, func(_ context.Context, _ dashLimitParams) (any, error) {
		t.Fatal("query should not be called when store is typed nil")
		return nil, nil
	})

	got, err := h(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := got.(map[string]any)
	arr, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("expected []any for key 'items', got %T", m["items"])
	}
	if len(arr) != 0 {
		t.Errorf("expected empty array, got %v", arr)
	}
}

// TestDashList_ValidQuery 正常查询应包装结果。
func TestDashList_ValidQuery(t *testing.T) {
	store := "not-nil" // 非 nil 即可

	h := dashList[dashLimitParams]("items", store, func(_ context.Context, _ dashLimitParams) (any, error) {
		return []string{"a", "b"}, nil
	})

	got, err := h(context.Background(), json.RawMessage(`{"limit":10}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := got.(map[string]any)
	items, ok := m["items"].([]string)
	if !ok {
		t.Fatalf("expected []string for key 'items', got %T", m["items"])
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

// TestDashList_QueryError 查询失败应返回空列表 + 不返回 error。
func TestDashList_QueryError(t *testing.T) {
	store := "not-nil"

	h := dashList[dashLimitParams]("logs", store, func(_ context.Context, _ dashLimitParams) (any, error) {
		return nil, fmt.Errorf("db connection failed")
	})

	got, err := h(context.Background(), nil)
	if err != nil {
		t.Fatalf("dashList should not propagate error, got: %v", err)
	}

	m := got.(map[string]any)
	arr := m["logs"].([]any)
	if len(arr) != 0 {
		t.Errorf("expected empty array on error, got %v", arr)
	}
}

// TestDashList_ParamsPassthrough typedHandler 参数应正确传递。
func TestDashList_ParamsPassthrough(t *testing.T) {
	type myParams struct {
		Keyword string `json:"keyword"`
		Limit   int    `json:"limit"`
	}
	store := "not-nil"

	var captured myParams
	h := dashList[myParams]("results", store, func(_ context.Context, p myParams) (any, error) {
		captured = p
		return []any{}, nil
	})

	raw := json.RawMessage(`{"keyword":"test","limit":50}`)
	_, err := h(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured.Keyword != "test" {
		t.Errorf("keyword = %q, want %q", captured.Keyword, "test")
	}
	if captured.Limit != 50 {
		t.Errorf("limit = %d, want 50", captured.Limit)
	}
}
