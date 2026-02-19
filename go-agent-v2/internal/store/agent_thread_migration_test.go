package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// migrationDir 返回 migrations 目录的绝对路径 (基于源文件位置)。
func migrationDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/store → ../../migrations
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

// TestAgentThreadMigration_FileExists 验证 0012_agent_threads.sql 迁移文件存在。
func TestAgentThreadMigration_FileExists(t *testing.T) {
	path := filepath.Join(migrationDir(t), "0012_agent_threads.sql")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("migration file does not exist: %s", path)
	}
}

// TestAgentThreadMigration_ContainsCreateTable 验证迁移包含 CREATE TABLE。
func TestAgentThreadMigration_ContainsCreateTable(t *testing.T) {
	path := filepath.Join(migrationDir(t), "0012_agent_threads.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := strings.ToLower(string(data))
	if !strings.Contains(sql, "create table") {
		t.Fatal("migration does not contain CREATE TABLE")
	}
	if !strings.Contains(sql, "agent_threads") {
		t.Fatal("migration does not reference agent_threads table")
	}
}

// TestAgentThreadMigration_ColumnsMatchGoCode 验证迁移 SQL 包含 Go 代码引用的所有列。
//
// atCols 常量定义了 Go 代码使用的列名列表,
// 迁移 SQL 必须包含所有这些列的定义。
func TestAgentThreadMigration_ColumnsMatchGoCode(t *testing.T) {
	path := filepath.Join(migrationDir(t), "0012_agent_threads.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := strings.ToLower(string(data))

	// atCols = "thread_id, prompt, model, cwd, status, port, pid, created_at, updated_at, finished_at, last_event_type, error_message"
	expectedCols := strings.Split(atCols, ",")
	for _, col := range expectedCols {
		col = strings.TrimSpace(col)
		if col == "" {
			continue
		}
		if !strings.Contains(sql, col) {
			t.Errorf("migration missing column referenced by Go code: %q", col)
		}
	}
}

// TestAgentThreadMigration_HasPrimaryKey 验证 thread_id 有 PRIMARY KEY (ON CONFLICT 依赖)。
func TestAgentThreadMigration_HasPrimaryKey(t *testing.T) {
	path := filepath.Join(migrationDir(t), "0012_agent_threads.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := strings.ToLower(string(data))
	if !strings.Contains(sql, "primary key") {
		t.Fatal("migration does not define a PRIMARY KEY (required for ON CONFLICT)")
	}
}
