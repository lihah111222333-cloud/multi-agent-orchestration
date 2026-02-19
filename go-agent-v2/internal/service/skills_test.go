package service

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListSkillsParsesMetadata(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "backend")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
description: "backend skill"
trigger_words: [go, api]
force_words:
  - test
  - tdd
---
# Backend`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	svc := NewSkillService(tmp)
	skills, err := svc.ListSkills()
	if err != nil {
		t.Fatalf("ListSkills error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("len(skills)=%d, want=1", len(skills))
	}
	got := skills[0]
	if got.Description != "backend skill" {
		t.Fatalf("description=%q", got.Description)
	}
	if !reflect.DeepEqual(got.TriggerWords, []string{"go", "api"}) {
		t.Fatalf("trigger_words=%v", got.TriggerWords)
	}
	if !reflect.DeepEqual(got.ForceWords, []string{"test", "tdd"}) {
		t.Fatalf("force_words=%v", got.ForceWords)
	}
}
