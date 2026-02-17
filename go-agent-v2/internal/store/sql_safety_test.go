// sql_safety_test.go — SQL 安全验证 5 函数的表驱动测试。
// Python 对应: test_agent_ops_store.py → test_db_query_and_db_execute_sql_guards + 4 execute tests。
package store

import (
	"errors"
	"testing"
)

func TestStripSQLLiterals(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"removes_string_content", "WHERE x = 'DROP TABLE users'", "WHERE x = ''"},
		{"preserves_non_strings", "SELECT id FROM t", "SELECT id FROM t"},
		{"multiple_literals", "SELECT 'a', 'b'", "SELECT '', ''"},
		{"empty_literal", "x = ''", "x = ''"},
		{"no_closing_quote", "x = 'unfinished", "x = 'unfinished"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripSQLLiterals(tt.in)
			if got != tt.want {
				t.Errorf("StripSQLLiterals(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateSingleStatement(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr error
	}{
		{"accepts_single", "SELECT 1", nil},
		{"accepts_trailing_semicolon", "SELECT 1;", nil},
		{"accepts_trailing_semicolon_with_spaces", "SELECT 1;  ", nil},
		{"rejects_multi", "SELECT 1; DROP TABLE users", ErrMultiStatement},
		{"rejects_two_selects", "SELECT 1; SELECT 2", ErrMultiStatement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSingleStatement(tt.sql)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateSingleStatement(%q) = %v, want %v", tt.sql, err, tt.wantErr)
			}
		})
	}
}

func TestFirstSQLKeyword(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"SELECT", "SELECT * FROM t", "SELECT"},
		{"INSERT", "INSERT INTO t VALUES (1)", "INSERT"},
		{"lowercase", "select 1", "SELECT"},
		{"leading_space", "  UPDATE t SET x=1", "UPDATE"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FirstSQLKeyword(tt.sql)
			if got != tt.want {
				t.Errorf("FirstSQLKeyword(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

func TestValidateReadOnlyQuery(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr error
	}{
		{"accepts_select", "SELECT * FROM users", nil},
		{"rejects_insert", "INSERT INTO users VALUES (1)", ErrReadOnlyViolation},
		{"rejects_delete", "DELETE FROM users", ErrReadOnlyViolation},
		{"rejects_update", "UPDATE users SET x=1", ErrReadOnlyViolation},
		{"rejects_drop", "DROP TABLE users", ErrReadOnlyViolation},
		{"ignores_write_in_string_literal", "SELECT * FROM t WHERE x = 'INSERT INTO'", nil},
		{"rejects_multi_statement", "SELECT 1; DROP TABLE users", ErrMultiStatement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReadOnlyQuery(tt.sql)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateReadOnlyQuery(%q) = %v, want %v", tt.sql, err, tt.wantErr)
			}
		})
	}
}

func TestValidateExecuteQuery(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr error
	}{
		{"accepts_insert", "INSERT INTO t VALUES (1)", nil},
		{"accepts_update", "UPDATE t SET x=1", nil},
		{"accepts_delete", "DELETE FROM t WHERE id=1", nil},
		{"rejects_select", "SELECT * FROM t", ErrDangerousSQL},
		{"rejects_drop_table", "DROP TABLE users", ErrDangerousSQL},
		{"rejects_truncate", "TRUNCATE users", ErrDangerousSQL},
		{"rejects_alter", "ALTER TABLE users ADD COLUMN x TEXT", ErrDangerousSQL},
		{"rejects_multi", "INSERT INTO t VALUES (1); DROP TABLE t", ErrMultiStatement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExecuteQuery(tt.sql)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateExecuteQuery(%q) = %v, want %v", tt.sql, err, tt.wantErr)
			}
		})
	}
}
