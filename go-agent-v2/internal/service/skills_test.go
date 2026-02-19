package service

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestListSkillsParsesTagsAndAliasesAsTriggerWords(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "grpc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
description: "grpc skill"
tags: [gRPC, protobuf, 微服务]
aliases:
  - "@gRPC"
  - "@grpc-service"
---
# gRPC`
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
	wantWords := []string{"gRPC", "protobuf", "微服务", "@gRPC", "@grpc-service"}
	if !reflect.DeepEqual(got.TriggerWords, wantWords) {
		t.Fatalf("trigger_words=%v, want=%v", got.TriggerWords, wantWords)
	}
}

func TestParseSkillMetadataDescriptionTruncatesByRunes(t *testing.T) {
	desc := strings.Repeat("这是一段用于截断验证的描述文本", 12)
	content := fmt.Sprintf(`---
description: "%s"
---`, desc)
	meta := parseSkillMetadata(content)
	if meta.Description == "" {
		t.Fatal("description should not be empty")
	}
	if !utf8.ValidString(meta.Description) {
		t.Fatalf("description should be valid utf8, got=%q", meta.Description)
	}
	if !strings.HasSuffix(meta.Description, "...") {
		t.Fatalf("description should be truncated with ellipsis, got=%q", meta.Description)
	}
}

func TestParseSkillMetadataDescriptionKeepsRuneBoundedText(t *testing.T) {
	desc := strings.Repeat("汉", 60)
	content := fmt.Sprintf(`---
description: "%s"
---`, desc)
	meta := parseSkillMetadata(content)
	if meta.Description != desc {
		t.Fatalf("description should keep full text, got=%q", meta.Description)
	}
}
