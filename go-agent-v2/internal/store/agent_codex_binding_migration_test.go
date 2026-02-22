package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentCodexBindingImmutableMigration_FileExists(t *testing.T) {
	path := filepath.Join(migrationDir(t), "0016_agent_codex_binding_immutable.sql")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("migration file does not exist: %s", path)
	}
}

func TestAgentCodexBindingImmutableMigration_ContainsTrigger(t *testing.T) {
	path := filepath.Join(migrationDir(t), "0016_agent_codex_binding_immutable.sql")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := strings.ToLower(string(b))
	if !strings.Contains(sql, "prevent_agent_codex_binding_rebind") {
		t.Fatal("migration missing immutable trigger function")
	}
	if !strings.Contains(sql, "before update on agent_codex_binding") {
		t.Fatal("migration missing before update trigger on agent_codex_binding")
	}
	if !strings.Contains(sql, "codex_thread_id is immutable") {
		t.Fatal("migration missing immutable codex_thread_id guard")
	}
}
