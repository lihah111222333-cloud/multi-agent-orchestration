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
summary: "用于后端实现与代码审查的摘要"
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
	if got.Summary != "用于后端实现与代码审查的摘要" {
		t.Fatalf("summary=%q", got.Summary)
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
	if got.Summary != "grpc skill" {
		t.Fatalf("summary=%q, want=%q", got.Summary, "grpc skill")
	}
	wantWords := []string{"gRPC", "protobuf", "微服务", "@gRPC", "@grpc-service"}
	if !reflect.DeepEqual(got.TriggerWords, wantWords) {
		t.Fatalf("trigger_words=%v, want=%v", got.TriggerWords, wantWords)
	}
}

func TestListSkillsAddsExplicitTriggerFromFrontmatterName(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "go-backend-tdd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
name: "测试驱动开发"
description: "tdd skill"
aliases: ["@TDD", "@测试驱动"]
---
# TDD`
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
	wantWords := []string{"@TDD", "@测试驱动", "@测试驱动开发", "[skill:测试驱动开发]"}
	if !reflect.DeepEqual(got.TriggerWords, wantWords) {
		t.Fatalf("trigger_words=%v, want=%v", got.TriggerWords, wantWords)
	}
}

func TestListSkillsUsesFrontmatterNameAsDisplayName(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "go-backend-development")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
name: "测试驱动开发"
description: "tdd skill"
---
# TDD`
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
	if skills[0].Name != "测试驱动开发" {
		t.Fatalf("skill name=%q, want=%q", skills[0].Name, "测试驱动开发")
	}
}

func TestReadSkillContentResolvesFrontmatterName(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "go-backend-development")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
name: "测试驱动开发"
description: "tdd skill"
---
# TDD
Body`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	svc := NewSkillService(tmp)

	got, err := svc.ReadSkillContent("测试驱动开发")
	if err != nil {
		t.Fatalf("ReadSkillContent by frontmatter name error: %v", err)
	}
	if !strings.Contains(got, "# TDD") {
		t.Fatalf("content=%q, want include # TDD", got)
	}

	got, err = svc.ReadSkillContent("go-backend-development")
	if err != nil {
		t.Fatalf("ReadSkillContent by dir name error: %v", err)
	}
	if !strings.Contains(got, "Body") {
		t.Fatalf("content=%q, want include Body", got)
	}
}

func TestReadSkillDigestIncludesSectionRefs(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "brand")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `---
summary: "品牌摘要"
---
# 品牌设计规范
## 第一部分：色彩系统
## 第二部分：字体系统
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	svc := NewSkillService(tmp)
	digest, err := svc.ReadSkillDigest("brand")
	if err != nil {
		t.Fatalf("ReadSkillDigest error: %v", err)
	}
	if len(digest.SectionRefs) != 3 {
		t.Fatalf("section refs len=%d, want=3", len(digest.SectionRefs))
	}
	if digest.SectionRefs[0].File != "SKILL.md" || digest.SectionRefs[0].Line != 4 {
		t.Fatalf("section ref[0]=%+v, want file SKILL.md line 4", digest.SectionRefs[0])
	}
}

func TestSummarizeSkillContentSourcePriority(t *testing.T) {
	contentWithSummary := `---
description: "desc text"
summary: "frontmatter summary"
---
# Title
Body`
	summary, source := SummarizeSkillContent(contentWithSummary)
	if summary != "frontmatter summary" {
		t.Fatalf("summary=%q", summary)
	}
	if source != "frontmatter" {
		t.Fatalf("source=%q, want=frontmatter", source)
	}

	contentWithDescriptionOnly := `---
description: "only description"
---
# Title
Body`
	summary, source = SummarizeSkillContent(contentWithDescriptionOnly)
	if summary != "only description" {
		t.Fatalf("summary=%q", summary)
	}
	if source != "description" {
		t.Fatalf("source=%q, want=description", source)
	}
}

func TestUpsertSkillSummaryFrontmatterUpdatesExistingSummary(t *testing.T) {
	content := `---
name: "backend"
description: "backend helper"
summary: "old summary"
---
# Backend
Body`
	updated := UpsertSkillSummaryFrontmatter(content, "new summary")
	if strings.Contains(updated, `summary: "old summary"`) {
		t.Fatalf("old summary should be removed, got=%q", updated)
	}
	if !strings.Contains(updated, `summary: "new summary"`) {
		t.Fatalf("new summary missing, got=%q", updated)
	}
	if !strings.Contains(updated, "# Backend\nBody") {
		t.Fatalf("body should be preserved, got=%q", updated)
	}
}

func TestUpsertSkillSummaryFrontmatterAddsHeaderWhenMissing(t *testing.T) {
	content := "# Skill\nBody"
	updated := UpsertSkillSummaryFrontmatter(content, "generated summary")
	if !strings.HasPrefix(updated, "---\nsummary: \"generated summary\"\n---\n\n") {
		t.Fatalf("should prepend frontmatter summary, got=%q", updated)
	}
	if !strings.Contains(updated, "# Skill\nBody") {
		t.Fatalf("body should remain, got=%q", updated)
	}
}

func TestUpsertSkillSummaryFrontmatterClearsSummary(t *testing.T) {
	content := `---
summary: "old summary"
---
# Skill
Body`
	updated := UpsertSkillSummaryFrontmatter(content, "")
	if strings.Contains(strings.ToLower(updated), "summary:") {
		t.Fatalf("summary should be removed when empty, got=%q", updated)
	}
	if strings.HasPrefix(updated, "---\n") {
		t.Fatalf("frontmatter should be removed when only summary existed, got=%q", updated)
	}
	if !strings.HasPrefix(updated, "# Skill") {
		t.Fatalf("body should remain as markdown body, got=%q", updated)
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
