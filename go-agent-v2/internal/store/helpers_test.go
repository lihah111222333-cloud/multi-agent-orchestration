// helpers_test.go — QueryBuilder + mustMarshalJSON 表驱动测试。
package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQueryBuilderEq(t *testing.T) {
	t.Run("skips_empty", func(t *testing.T) {
		qb := NewQueryBuilder()
		qb.Eq("status", "")
		clause := qb.WhereClause()
		if clause != "" {
			t.Errorf("expected empty WHERE, got %q", clause)
		}
	})

	t.Run("adds_condition", func(t *testing.T) {
		qb := NewQueryBuilder()
		qb.Eq("status", "active")
		clause := qb.WhereClause()
		if !strings.Contains(clause, "status = $1") {
			t.Errorf("expected 'status = $1' in WHERE, got %q", clause)
		}
		params := qb.Params()
		if len(params) != 1 || params[0] != "active" {
			t.Errorf("expected params [active], got %v", params)
		}
	})

	t.Run("multiple_conditions", func(t *testing.T) {
		qb := NewQueryBuilder()
		qb.Eq("status", "active").Eq("level", "ERROR")
		clause := qb.WhereClause()
		if !strings.Contains(clause, "status = $1") || !strings.Contains(clause, "level = $2") {
			t.Errorf("expected both conditions, got %q", clause)
		}
	})
}

func TestQueryBuilderKeywordLike(t *testing.T) {
	t.Run("ESCAPE_clause", func(t *testing.T) {
		qb := NewQueryBuilder()
		qb.KeywordLike("test", "message")
		clause := qb.WhereClause()
		if !strings.Contains(clause, `ESCAPE E'\\'`) {
			t.Errorf("expected ESCAPE clause, got %q", clause)
		}
	})

	t.Run("escapes_percent", func(t *testing.T) {
		qb := NewQueryBuilder()
		qb.KeywordLike("100%", "message")
		params := qb.Params()
		if len(params) != 1 {
			t.Fatalf("expected 1 param, got %d", len(params))
		}
		p := params[0].(string)
		if !strings.Contains(p, `100\%`) {
			t.Errorf("expected escaped percent in param, got %q", p)
		}
	})

	t.Run("skips_empty_keyword", func(t *testing.T) {
		qb := NewQueryBuilder()
		qb.KeywordLike("", "message")
		clause := qb.WhereClause()
		if clause != "" {
			t.Errorf("expected empty WHERE for empty keyword, got %q", clause)
		}
	})

	t.Run("multi_column", func(t *testing.T) {
		qb := NewQueryBuilder()
		qb.KeywordLike("test", "message", "detail")
		clause := qb.WhereClause()
		if !strings.Contains(clause, "LOWER(message)") || !strings.Contains(clause, "LOWER(detail)") {
			t.Errorf("expected both columns in LIKE, got %q", clause)
		}
		if !strings.Contains(clause, " OR ") {
			t.Errorf("expected OR between columns, got %q", clause)
		}
	})
}

func TestQueryBuilderBuild(t *testing.T) {
	t.Run("limit_clamped_zero", func(t *testing.T) {
		qb := NewQueryBuilder()
		sql, params := qb.Build("SELECT * FROM t", "", 0)
		if !strings.Contains(sql, "LIMIT $1") {
			t.Errorf("expected LIMIT clause, got %q", sql)
		}
		// limit=0 → clamped to 1
		if params[0] != 1 {
			t.Errorf("expected limit=1, got %v", params[0])
		}
	})

	t.Run("limit_clamped_high", func(t *testing.T) {
		qb := NewQueryBuilder()
		_, params := qb.Build("SELECT * FROM t", "", 9999)
		if params[0] != 2000 {
			t.Errorf("expected limit=2000, got %v", params[0])
		}
	})

	t.Run("full_query", func(t *testing.T) {
		qb := NewQueryBuilder()
		qb.Eq("status", "active")
		sql, params := qb.Build("SELECT * FROM t", "created_at DESC", 10)
		if !strings.Contains(sql, "WHERE status = $1") {
			t.Errorf("expected WHERE clause, got %q", sql)
		}
		if !strings.Contains(sql, "ORDER BY created_at DESC") {
			t.Errorf("expected ORDER BY clause, got %q", sql)
		}
		if !strings.Contains(sql, "LIMIT $2") {
			t.Errorf("expected LIMIT $2, got %q", sql)
		}
		if len(params) != 2 || params[0] != "active" || params[1] != 10 {
			t.Errorf("expected params [active, 10], got %v", params)
		}
	})
}

// TestMustMarshalJSON 验证 mustMarshalJSON 对各种输入的安全序列化行为。
func TestMustMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		wantJSON string // 期望 json.Valid 且值相等
	}{
		{
			name:     "normal_map",
			input:    map[string]any{"key": "value", "n": 42},
			wantJSON: `{"key":"value","n":42}`,
		},
		{
			name:     "nil_input",
			input:    nil,
			wantJSON: `null`,
		},
		{
			name:     "string_slice",
			input:    []string{"a", "b"},
			wantJSON: `["a","b"]`,
		},
		{
			name:     "empty_map",
			input:    map[string]any{},
			wantJSON: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustMarshalJSON(tt.input)

			if !json.Valid(got) {
				t.Fatalf("mustMarshalJSON returned invalid JSON: %q", got)
			}

			// 比较反序列化后的值 (忽略 key 顺序)
			var gotVal, wantVal any
			if err := json.Unmarshal(got, &gotVal); err != nil {
				t.Fatalf("unmarshal got: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.wantJSON), &wantVal); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}

			gotRe, _ := json.Marshal(gotVal)
			wantRe, _ := json.Marshal(wantVal)
			if string(gotRe) != string(wantRe) {
				t.Errorf("mustMarshalJSON(%v) = %s, want %s", tt.input, got, tt.wantJSON)
			}
		})
	}
}

// TestMustMarshalJSON_Unmarshalable 验证不可序列化输入回退为 "{}"。
func TestMustMarshalJSON_Unmarshalable(t *testing.T) {
	// chan 不可 JSON 序列化
	ch := make(chan int)
	got := mustMarshalJSON(ch)

	if string(got) != "{}" {
		t.Errorf("mustMarshalJSON(chan) = %s, want {}", got)
	}
}
