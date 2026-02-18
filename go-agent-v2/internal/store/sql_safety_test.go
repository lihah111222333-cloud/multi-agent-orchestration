package store

import (
	"testing"
)

// ────────────────────────────────────────────────────
// ValidateSingleStatement
// ────────────────────────────────────────────────────

func TestValidateSingleStatement_AcceptsSingle(t *testing.T) {
	if err := ValidateSingleStatement("SELECT 1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateSingleStatement_RejectsMulti(t *testing.T) {
	if err := ValidateSingleStatement("SELECT 1; DROP TABLE foo"); err == nil {
		t.Fatal("expected error for multi-statement SQL")
	}
}

// ────────────────────────────────────────────────────
// ValidateReadOnlyQuery — 写入关键词拦截
// ────────────────────────────────────────────────────

func TestValidateReadOnlyQuery_AcceptsSelect(t *testing.T) {
	if err := ValidateReadOnlyQuery("SELECT * FROM users WHERE id = 1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsInsert(t *testing.T) {
	err := ValidateReadOnlyQuery("INSERT INTO users (name) VALUES ('bob')")
	if err != ErrReadOnlyViolation {
		t.Fatalf("expected ErrReadOnlyViolation, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsUpdate(t *testing.T) {
	err := ValidateReadOnlyQuery("UPDATE users SET name='x'")
	if err != ErrReadOnlyViolation {
		t.Fatalf("expected ErrReadOnlyViolation, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsDelete(t *testing.T) {
	err := ValidateReadOnlyQuery("DELETE FROM users")
	if err != ErrReadOnlyViolation {
		t.Fatalf("expected ErrReadOnlyViolation, got %v", err)
	}
}

// ────────────────────────────────────────────────────
// ValidateReadOnlyQuery — 危险函数拦截
// ────────────────────────────────────────────────────

func TestValidateReadOnlyQuery_RejectsPgReadFile(t *testing.T) {
	err := ValidateReadOnlyQuery("SELECT pg_read_file('/etc/passwd')")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsPgReadBinaryFile(t *testing.T) {
	err := ValidateReadOnlyQuery("SELECT pg_read_binary_file('/etc/shadow')")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsPgLsDir(t *testing.T) {
	err := ValidateReadOnlyQuery("SELECT pg_ls_dir('/tmp')")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsLoImport(t *testing.T) {
	err := ValidateReadOnlyQuery("SELECT lo_import('/etc/passwd')")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsDblink(t *testing.T) {
	err := ValidateReadOnlyQuery("SELECT * FROM dblink('host=evil', 'SELECT 1')")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsDblink_CaseInsensitive(t *testing.T) {
	err := ValidateReadOnlyQuery("SELECT * FROM DBLINK('host=evil', 'SELECT 1')")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsPgStatFile(t *testing.T) {
	err := ValidateReadOnlyQuery("SELECT pg_stat_file('/etc/hosts')")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateReadOnlyQuery_RejectsDblink_exec(t *testing.T) {
	err := ValidateReadOnlyQuery("SELECT dblink_exec('host=evil', 'DROP TABLE users')")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateReadOnlyQuery_AllowsNormalFunctions(t *testing.T) {
	// These should NOT be blocked — they are safe PostgreSQL functions
	safeCases := []string{
		"SELECT count(*) FROM users",
		"SELECT now(), current_timestamp",
		"SELECT upper(name), lower(name) FROM users",
		"SELECT string_agg(name, ',') FROM users",
		"SELECT json_agg(row_to_json(t)) FROM users t",
	}
	for _, sql := range safeCases {
		if err := ValidateReadOnlyQuery(sql); err != nil {
			t.Errorf("safe SQL wrongly blocked: %q → %v", sql, err)
		}
	}
}

// ────────────────────────────────────────────────────
// StripSQLLiterals
// ────────────────────────────────────────────────────

func TestStripSQLLiterals_RemovesStrings(t *testing.T) {
	got := StripSQLLiterals("SELECT * WHERE name = 'DELETE'")
	//  'DELETE' should be stripped so it doesn't trigger write check
	if ValidateReadOnlyQuery("SELECT * WHERE name = 'DELETE'") != nil {
		t.Fatal("string literal 'DELETE' should not trigger write keyword detection")
	}
	_ = got
}

// ────────────────────────────────────────────────────
// ValidateExecuteQuery
// ────────────────────────────────────────────────────

func TestValidateExecuteQuery_AcceptsInsert(t *testing.T) {
	if err := ValidateExecuteQuery("INSERT INTO logs (msg) VALUES ('hello')"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateExecuteQuery_AcceptsUpdate(t *testing.T) {
	if err := ValidateExecuteQuery("UPDATE users SET name = 'bob' WHERE id = 1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateExecuteQuery_AcceptsDelete(t *testing.T) {
	if err := ValidateExecuteQuery("DELETE FROM logs WHERE id = 1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateExecuteQuery_RejectsDrop(t *testing.T) {
	err := ValidateExecuteQuery("DROP TABLE users")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateExecuteQuery_RejectsTruncate(t *testing.T) {
	err := ValidateExecuteQuery("TRUNCATE users")
	if err != ErrDangerousSQL {
		t.Fatalf("expected ErrDangerousSQL, got %v", err)
	}
}

func TestValidateExecuteQuery_RejectsSelect(t *testing.T) {
	err := ValidateExecuteQuery("SELECT * FROM users")
	if err == nil {
		t.Fatal("expected error — SELECT not in execute whitelist")
	}
}
