package store

import (
	"strings"
	"testing"
)

// ========================================
// D3: QueryBuilder.Gte — 新方法测试
// ========================================

func TestQueryBuilder_Gte_AddsCondition(t *testing.T) {
	q := NewQueryBuilder().Gte("started_at", "2024-01-01")
	sql, params := q.Build("SELECT * FROM t", "id", 10)

	wantSQL := "SELECT * FROM t WHERE (\"started_at\" >= $1) ORDER BY id LIMIT $2"
	if sql != wantSQL {
		t.Fatalf("SQL mismatch:\n got:  %s\n want: %s", sql, wantSQL)
	}
	if len(params) != 2 {
		t.Fatalf("params count: got %d, want 2", len(params))
	}
	if params[0] != "2024-01-01" {
		t.Errorf("params[0]: got %v, want 2024-01-01", params[0])
	}
}

func TestQueryBuilder_Gte_NilSkipped(t *testing.T) {
	q := NewQueryBuilder().Gte("started_at", nil)
	sql, params := q.Build("SELECT * FROM t", "id", 10)

	wantSQL := "SELECT * FROM t ORDER BY id LIMIT $1"
	if sql != wantSQL {
		t.Fatalf("SQL mismatch:\n got:  %s\n want: %s", sql, wantSQL)
	}
	if len(params) != 1 {
		t.Fatalf("params count: got %d, want 1", len(params))
	}
}

func TestQueryBuilder_Gte_CombinesWithEq(t *testing.T) {
	q := NewQueryBuilder().
		Eq("component", "agent-x").
		Gte("started_at", "2024-01-01")
	sql, params := q.Build("SELECT * FROM t", "id", 10)

	wantSQL := "SELECT * FROM t WHERE (\"component\" = $1 AND \"started_at\" >= $2) ORDER BY id LIMIT $3"
	if sql != wantSQL {
		t.Fatalf("SQL mismatch:\n got:  %s\n want: %s", sql, wantSQL)
	}
	if len(params) != 3 {
		t.Fatalf("params count: got %d, want 3", len(params))
	}
}

func TestQueryBuilder_Gte_HighParamIndex(t *testing.T) {
	// Regression: ensures Gte works correctly even when param index > 9.
	q := NewQueryBuilder()
	for i := 0; i < 10; i++ {
		q.Eq("col", "val")
	}
	q.Gte("ts", "2024-01-01")
	sql, params := q.Build("SELECT * FROM t", "", 10)

	// $11 for Gte, $12 for LIMIT
	if len(params) != 12 {
		t.Fatalf("params count: got %d, want 12", len(params))
	}
	// The critical check: param index must be numeric $11, not a broken char.
	if !strings.Contains(sql, "\"ts\" >= $11") {
		t.Fatalf("SQL should contain '\"ts\" >= $11' for high param index, got: %s", sql)
	}
}

// ========================================
// D2: defaultStr — 确认从 helpers.go 可用
// ========================================

func TestDefaultStr_NonEmpty(t *testing.T) {
	if got := defaultStr("hello", "default"); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestDefaultStr_Empty(t *testing.T) {
	if got := defaultStr("", "fallback"); got != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}
}

func TestDefaultStr_BothEmpty(t *testing.T) {
	if got := defaultStr("", ""); got != "" {
		t.Errorf("got %q, want %q", got, "")
	}
}
