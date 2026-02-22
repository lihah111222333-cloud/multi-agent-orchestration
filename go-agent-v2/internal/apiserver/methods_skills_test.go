package apiserver

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/service"
)

func seededSkillService(t *testing.T, root string) *service.SkillService {
	t.Helper()
	svc := service.NewSkillService(root)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("seededSkillService readdir %s: %v", root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || name == "by-id" || strings.HasPrefix(name, ".") {
			continue
		}
		sourceDir := filepath.Join(root, name)
		if _, statErr := os.Stat(filepath.Join(sourceDir, "SKILL.md")); statErr != nil {
			continue
		}
		if _, importErr := svc.ImportSkillDirectory(sourceDir, name); importErr != nil {
			t.Fatalf("seededSkillService import %s: %v", sourceDir, importErr)
		}
	}
	return svc
}

func TestSkillsListUsesConfiguredDirectory(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "backend"), 0o755); err != nil {
		t.Fatalf("mkdir backend: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "backend", "SKILL.md"), []byte(`---
description: "backend helper"
summary: "backend summary"
---
# backend`), 0o644); err != nil {
		t.Fatalf("write backend SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "tdd"), 0o755); err != nil {
		t.Fatalf("mkdir tdd: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "tdd", "SKILL.md"), []byte(`---
description: "tdd helper"
summary: "tdd summary"
---
# tdd`), 0o644); err != nil {
		t.Fatalf("write tdd SKILL.md: %v", err)
	}

	srv := &Server{
		skillsDir: tmp,
		skillSvc:  seededSkillService(t, tmp),
	}
	raw, err := srv.skillsList(context.Background(), nil)
	if err != nil {
		t.Fatalf("skillsList error: %v", err)
	}
	resp := raw.(map[string]any)
	skills := resp["skills"].([]map[string]any)
	got := []string{skills[0]["name"].(string), skills[1]["name"].(string)}
	want := []string{"backend", "tdd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skillsList names=%v, want=%v", got, want)
	}
	if skills[0]["summary"] != "backend summary" || skills[1]["summary"] != "tdd summary" {
		t.Fatalf("skillsList summaries=%v", []any{skills[0]["summary"], skills[1]["summary"]})
	}
}

func TestThreadSkillsListUsesLocalSkillService(t *testing.T) {
	tmp := t.TempDir()
	svc := service.NewSkillService(tmp)
	if _, err := svc.WriteSkillContent("backend", "# backend"); err != nil {
		t.Fatalf("WriteSkillContent backend: %v", err)
	}
	if _, err := svc.WriteSkillContent("ops", "# ops"); err != nil {
		t.Fatalf("WriteSkillContent ops: %v", err)
	}

	srv := &Server{skillSvc: svc}
	raw, err := srv.threadSkillsList(context.Background(), nil)
	if err != nil {
		t.Fatalf("threadSkillsList error: %v", err)
	}
	resp := raw.(map[string]any)
	skills, ok := resp["skills"].([]string)
	if !ok {
		t.Fatalf("skills type=%T, want []string", resp["skills"])
	}
	want := []string{"backend", "ops"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("threadSkillsList skills=%v, want=%v", skills, want)
	}
}

func TestThreadSkillsListWithoutServiceReturnsEmpty(t *testing.T) {
	srv := &Server{}
	raw, err := srv.threadSkillsList(context.Background(), nil)
	if err != nil {
		t.Fatalf("threadSkillsList error: %v", err)
	}
	resp := raw.(map[string]any)
	skills, ok := resp["skills"].([]string)
	if !ok {
		t.Fatalf("skills type=%T, want []string", resp["skills"])
	}
	if len(skills) != 0 {
		t.Fatalf("threadSkillsList empty skills=%v, want=[]", skills)
	}
}

func TestSkillsDirectoryDefaultsToAppCache(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	srv := &Server{}
	got := srv.skillsDirectory()
	want := filepath.Join(tmpHome, ".multi-agent", "skills-cache")
	if got != want {
		t.Fatalf("skillsDirectory=%q, want=%q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("default app cache skills dir should exist: %v", err)
	}
}

func TestSkillsListUsesFrontmatterNameWhenProvided(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "go-backend-development"), 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "go-backend-development", "SKILL.md"), []byte(`---
name: "测试驱动开发"
description: "tdd helper"
summary: "tdd summary"
---
# tdd`), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	srv := &Server{
		skillsDir: tmp,
		skillSvc:  seededSkillService(t, tmp),
	}
	raw, err := srv.skillsList(context.Background(), nil)
	if err != nil {
		t.Fatalf("skillsList error: %v", err)
	}
	resp := raw.(map[string]any)
	skills := resp["skills"].([]map[string]any)
	if len(skills) != 1 {
		t.Fatalf("len(skills)=%d, want=1", len(skills))
	}
	if got := skills[0]["name"].(string); got != "测试驱动开发" {
		t.Fatalf("skillsList name=%q, want=%q", got, "测试驱动开发")
	}
}

func TestSkillsConfigWriteRejectsDeprecatedAgentMode(t *testing.T) {
	tmp := t.TempDir()
	srv := &Server{
		skillsDir: tmp,
		skillSvc:  service.NewSkillService(tmp),
	}
	ctx := context.Background()

	_, err := srv.skillsConfigWriteTyped(ctx, skillsConfigWriteParams{
		Content: "name: backend",
	})
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("expected name validation error, got=%v", err)
	}
}

func TestSkillsConfigWriteAndRemoteWriteUseConfiguredDirectory(t *testing.T) {
	tmp := t.TempDir()
	srv := &Server{
		skillsDir:   tmp,
		skillSvc:    service.NewSkillService(tmp),
		agentSkills: make(map[string][]string),
	}
	ctx := context.Background()

	raw, err := srv.skillsConfigWriteTyped(ctx, skillsConfigWriteParams{
		Name:    "backend",
		Content: "name: backend",
	})
	if err != nil {
		t.Fatalf("skillsConfigWriteTyped file mode error: %v", err)
	}
	path := raw.(map[string]any)["path"].(string)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected skill file in configured dir: %v", err)
	}
	if !strings.Contains(path, filepath.Join(tmp, "by-id")) {
		t.Fatalf("skillsConfigWriteTyped path=%q, want under by-id", path)
	}

	raw, err = srv.skillsRemoteWriteTyped(ctx, skillsRemoteWriteParams{
		Name:    "qa/tdd",
		Content: "x",
	})
	if err != nil {
		t.Fatalf("skillsRemoteWriteTyped nested path error: %v", err)
	}
	path = raw.(map[string]any)["path"].(string)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected nested skill file for slash name: %v", err)
	}
	if !strings.Contains(path, filepath.Join(tmp, "by-id")) {
		t.Fatalf("skillsRemoteWriteTyped slash path=%q, want under by-id", path)
	}

	raw, err = srv.skillsRemoteWriteTyped(ctx, skillsRemoteWriteParams{
		Name:    "tdd",
		Content: "name: tdd",
	})
	if err != nil {
		t.Fatalf("skillsRemoteWriteTyped valid name error: %v", err)
	}
	path = raw.(map[string]any)["path"].(string)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected remote skill file in configured dir: %v", err)
	}

	raw, err = srv.skillsRemoteWriteTyped(ctx, skillsRemoteWriteParams{
		Name:    "../escape",
		Content: "name: traversal-safe",
	})
	if err != nil {
		t.Fatalf("skillsRemoteWriteTyped traversal-like name error: %v", err)
	}
	path = raw.(map[string]any)["path"].(string)
	if !strings.Contains(path, filepath.Join(tmp, "by-id")) {
		t.Fatalf("skillsRemoteWriteTyped traversal path=%q, want under by-id", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected traversal skill file in configured dir: %v", err)
	}
}

func TestSkillsSummaryWriteTypedUpdatesFrontmatterSummary(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "backend")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	content := `---
name: "backend"
description: "backend helper"
---
# Backend
Body`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	srv := &Server{
		skillsDir: tmp,
		skillSvc:  seededSkillService(t, tmp),
	}
	raw, err := srv.skillsSummaryWriteTyped(context.Background(), skillsSummaryWriteParams{
		Name:    "backend",
		Summary: "inline edited summary",
	})
	if err != nil {
		t.Fatalf("skillsSummaryWriteTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	if resp["summary"] != "inline edited summary" {
		t.Fatalf("response summary=%v, want=%q", resp["summary"], "inline edited summary")
	}
	updatedPath, _ := resp["path"].(string)
	if updatedPath == "" {
		t.Fatalf("response path should not be empty: %v", resp)
	}
	updated, err := os.ReadFile(updatedPath)
	if err != nil {
		t.Fatalf("read updated SKILL.md: %v", err)
	}
	text := string(updated)
	if !strings.Contains(text, `summary: "inline edited summary"`) {
		t.Fatalf("updated content missing summary, got=%q", text)
	}
	if !strings.Contains(text, "# Backend\nBody") {
		t.Fatalf("updated content should keep body, got=%q", text)
	}
}

func TestBuildConfiguredSkillPrompt(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("backend", `---
summary: "backend-summary"
---
# backend
FULL BACKEND DETAIL SHOULD NOT INJECT`)
	writeSkill("tdd", `---
summary: "tdd-summary"
---
# tdd
FULL TDD DETAIL SHOULD NOT INJECT`)
	writeSkill("ops", `---
summary: "ops-summary"
---
# ops
FULL OPS DETAIL SHOULD NOT INJECT`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
		agentSkills: map[string][]string{
			"thread-1": {"backend", "tdd", "missing"},
		},
	}

	input := []UserInput{
		{Type: "skill", Name: "tdd", Content: "manual tdd"},
	}
	prompt, count := srv.buildConfiguredSkillPrompt("thread-1", input)
	if count != 0 {
		t.Fatalf("configured skill count=%d, want=0", count)
	}
	if strings.TrimSpace(prompt) != "" {
		t.Fatalf("configured skill prompt should be disabled, got=%q", prompt)
	}
}

func TestBuildSelectedSkillPrompt(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("backend", `---
summary: "backend-summary"
---
# backend
FULL BACKEND DETAIL SHOULD NOT INJECT`)
	writeSkill("tdd", `---
summary: "tdd-summary"
---
# tdd
FULL TDD DETAIL SHOULD NOT INJECT`)
	writeSkill("ops", `---
summary: "ops-summary"
---
# ops
FULL OPS DETAIL SHOULD NOT INJECT`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
	}
	prompt, count := srv.buildSelectedSkillPrompt([]string{"backend", "tdd", "missing"})
	if count != 2 {
		t.Fatalf("selected skill count=%d, want=2", count)
	}
	if !strings.Contains(prompt, "[skill:backend]") {
		t.Fatalf("expected backend skill prompt, got=%q", prompt)
	}
	if !strings.Contains(prompt, "[skill:tdd]") {
		t.Fatalf("expected tdd skill prompt, got=%q", prompt)
	}
	if !strings.Contains(prompt, "FULL BACKEND DETAIL SHOULD NOT INJECT") {
		t.Fatalf("selected skill should inject full content, got=%q", prompt)
	}
	if !strings.Contains(prompt, "FULL TDD DETAIL SHOULD NOT INJECT") {
		t.Fatalf("selected skill should inject full content, got=%q", prompt)
	}
	if strings.Contains(prompt, "[skill:ops]") {
		t.Fatalf("non-selected skill should not be injected, got=%q", prompt)
	}
}

func TestBuildTurnSkillPromptAutoInjectsExplicitSkillWhenNoManualSelection(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("测试驱动开发", `---
description: TDD
aliases: ["@TDD", "@测试驱动"]
---
tdd skill`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
	}

	skillPrompt, selectedCount, autoCount := srv.buildTurnSkillPrompt(
		"thread-1",
		"请按@测试驱动开发执行",
		nil,
		nil,
		false,
	)
	if selectedCount != 0 {
		t.Fatalf("selectedCount=%d, want=0", selectedCount)
	}
	if autoCount != 1 {
		t.Fatalf("autoCount=%d, want=1", autoCount)
	}
	if !strings.Contains(skillPrompt, "[skill:测试驱动开发]") {
		t.Fatalf("expected auto injected skill prompt, got=%q", skillPrompt)
	}
}

func TestBuildTurnSkillPromptAutoInjectsExplicitFrontmatterNameWhenDirDiffers(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("go-backend-development", `---
name: 测试驱动开发
description: TDD
aliases: ["@TDD", "@测试驱动"]
---
tdd skill`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
	}

	skillPrompt, selectedCount, autoCount := srv.buildTurnSkillPrompt(
		"thread-1",
		"请按@测试驱动开发执行",
		nil,
		nil,
		false,
	)
	if selectedCount != 0 {
		t.Fatalf("selectedCount=%d, want=0", selectedCount)
	}
	if autoCount != 1 {
		t.Fatalf("autoCount=%d, want=1", autoCount)
	}
	if !strings.Contains(skillPrompt, "[skill:测试驱动开发]") {
		t.Fatalf("expected auto injected skill prompt by logical skill name, got=%q", skillPrompt)
	}
}

func TestBuildTurnSkillPromptManualSelectionDisablesAutoMatch(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("测试驱动开发", `---
description: TDD
---
tdd skill`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
	}

	skillPrompt, selectedCount, autoCount := srv.buildTurnSkillPrompt(
		"thread-1",
		"请按@测试驱动开发执行",
		nil,
		nil,
		true,
	)
	if selectedCount != 0 {
		t.Fatalf("selectedCount=%d, want=0", selectedCount)
	}
	if autoCount != 0 {
		t.Fatalf("autoCount=%d, want=0", autoCount)
	}
	if strings.TrimSpace(skillPrompt) != "" {
		t.Fatalf("expected no skill prompt when manual selection is enabled, got=%q", skillPrompt)
	}
}

func TestBuildAutoMatchedSkillPrompt(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("backend", `---
description: backend helper
trigger_words: [golang, api]
---
backend skill`)
	writeSkill("tdd", `---
description: test helper
force_words:
  - test
---
tdd skill`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
		agentSkills: map[string][]string{
			"thread-1": {"backend"},
		},
	}

	prompt, count := srv.buildAutoMatchedSkillPrompt("thread-1", "please write API test", nil)
	if count != 1 {
		t.Fatalf("auto matched skill count=%d, want=1", count)
	}
	if !strings.Contains(prompt, "[skill:tdd]") {
		t.Fatalf("expected tdd skill in auto matched prompt, got=%q", prompt)
	}
	if !strings.Contains(prompt, "强制触发词: test") {
		t.Fatalf("expected force instruction in auto matched prompt, got=%q", prompt)
	}
	if !strings.Contains(prompt, "tdd skill") {
		t.Fatalf("force matched skill should inject full skill body, got=%q", prompt)
	}
	if strings.Contains(prompt, "摘要: test helper") {
		t.Fatalf("force matched skill should not inject digest, got=%q", prompt)
	}
	if strings.Contains(prompt, "[skill:backend]") {
		t.Fatalf("configured skill should be skipped in auto match, got=%q", prompt)
	}
}

func TestBuildAutoMatchedSkillPromptUsesTagsAndAliases(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("brand-guide", `---
description: 品牌设计规范
tags: [品牌, 设计, 视觉]
aliases:
  - "@品牌"
---
brand guide`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
	}

	prompt, count := srv.buildAutoMatchedSkillPrompt("thread-1", "请输出品牌视觉规范", nil)
	if count != 1 {
		t.Fatalf("auto matched skill count=%d, want=1", count)
	}
	if !strings.Contains(prompt, "[skill:brand-guide]") {
		t.Fatalf("expected brand-guide skill in auto matched prompt, got=%q", prompt)
	}
}

func TestBuildAutoMatchedSkillPromptMatchesExplicitSkillName(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("后端", `---
description: Go后端规范
---
backend guide`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
	}

	prompt, count := srv.buildAutoMatchedSkillPrompt("thread-1", "请按@后端规范实现接口", nil)
	if count != 1 {
		t.Fatalf("auto matched skill count=%d, want=1", count)
	}
	if !strings.Contains(prompt, "[skill:后端]") {
		t.Fatalf("expected explicit skill match, got=%q", prompt)
	}
}

func TestClassifyAutoSkillMatchForcePrecedesExplicit(t *testing.T) {
	normalized := strings.ToLower("请按@后端规范实现接口")
	matchedBy, matchedTerms := classifyAutoSkillMatch(normalized, "后端", []string{"@后端"}, nil)
	if matchedBy != "force" {
		t.Fatalf("matchedBy=%q, want=force", matchedBy)
	}
	if !reflect.DeepEqual(matchedTerms, []string{"@后端"}) {
		t.Fatalf("matchedTerms=%v, want=[@后端]", matchedTerms)
	}
}

func TestBuildAutoMatchedSkillPromptIncludesConfiguredForceInstruction(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("后端", `---
summary: "backend-summary"
force_words: ["@后端"]
---
# backend
FULL BACKEND DETAIL SHOULD NOT INJECT`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
		agentSkills: map[string][]string{
			"thread-1": {"后端"},
		},
	}

	prompt, count := srv.buildAutoMatchedSkillPrompt("thread-1", "请按@后端实现", nil)
	if count != 1 {
		t.Fatalf("auto matched skill count=%d, want=1", count)
	}
	if !strings.Contains(prompt, "[skill:后端]") {
		t.Fatalf("expected backend skill in auto matched prompt, got=%q", prompt)
	}
	if !strings.Contains(prompt, "强制触发词: @后端") {
		t.Fatalf("expected force instruction, got=%q", prompt)
	}
	if !strings.Contains(prompt, "执行要求: 本轮必须遵循该技能。") {
		t.Fatalf("expected mandatory instruction, got=%q", prompt)
	}
	if !strings.Contains(prompt, "FULL BACKEND DETAIL SHOULD NOT INJECT") {
		t.Fatalf("force matched skill should inject full body even when configured, got=%q", prompt)
	}
}

func TestBuildForcedOrExplicitMatchedSkillPromptSkipsTriggerWhenManual(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("后端", `---
summary: "backend-summary"
---
# backend
FULL BACKEND DETAIL SHOULD NOT INJECT`)
	writeSkill("api-helper", `---
description: API helper
trigger_words: [api]
---
api helper`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
		agentSkills: map[string][]string{
			"thread-1": {"后端"},
		},
	}

	prompt, count := srv.buildForcedOrExplicitMatchedSkillPrompt("thread-1", "请按@后端实现 api", nil)
	if count != 1 {
		t.Fatalf("manual auto matched skill count=%d, want=1", count)
	}
	if !strings.Contains(prompt, "[skill:后端]") {
		t.Fatalf("expected explicit @skill to be included, got=%q", prompt)
	}
	if strings.Contains(prompt, "[skill:api-helper]") {
		t.Fatalf("trigger-only skill should be skipped in manual mode, got=%q", prompt)
	}
	if !strings.Contains(prompt, "FULL BACKEND DETAIL SHOULD NOT INJECT") {
		t.Fatalf("full skill body should be injected, got=%q", prompt)
	}
}

func TestSkillsMatchPreviewTypedReturnsReasonAndTerms(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("backend", `---
description: backend helper
trigger_words: [api]
---
backend skill`)
	writeSkill("brand-guide", `---
description: 品牌设计规范
tags: [品牌, 设计]
force_words: ["@品牌", "@brand"]
---
brand guide`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
		agentSkills: map[string][]string{
			"thread-1": {"backend"},
		},
	}

	raw, err := srv.skillsMatchPreviewTyped(context.Background(), skillsMatchPreviewParams{
		ThreadID: "thread-1",
		Text:     "请按@品牌输出 api 相关规范",
	})
	if err != nil {
		t.Fatalf("skillsMatchPreviewTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	threadID, _ := resp["thread_id"].(string)
	if threadID != "thread-1" {
		t.Fatalf("thread_id=%q, want=thread-1", threadID)
	}
	matches, ok := resp["matches"].([]skillsMatchPreviewItem)
	if !ok {
		t.Fatalf("matches type=%T, want=[]skillsMatchPreviewItem", resp["matches"])
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches)=%d, want=1", len(matches))
	}
	if matches[0].Name != "brand-guide" {
		t.Fatalf("match[0].Name=%q, want=brand-guide", matches[0].Name)
	}
	if matches[0].MatchedBy != "force" {
		t.Fatalf("match[0].MatchedBy=%q, want=force", matches[0].MatchedBy)
	}
	if !reflect.DeepEqual(matches[0].MatchedTerms, []string{"@品牌"}) {
		t.Fatalf("match[0].MatchedTerms=%v, want=[@品牌]", matches[0].MatchedTerms)
	}
}

func TestSkillsMatchPreviewTypedIncludesConfiguredExplicitSkill(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("后端", `---
description: Go后端规范
---
backend guide`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
		agentSkills: map[string][]string{
			"thread-1": {"后端"},
		},
	}

	raw, err := srv.skillsMatchPreviewTyped(context.Background(), skillsMatchPreviewParams{
		ThreadID: "thread-1",
		Text:     "请按@后端实现",
	})
	if err != nil {
		t.Fatalf("skillsMatchPreviewTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	matches, ok := resp["matches"].([]skillsMatchPreviewItem)
	if !ok {
		t.Fatalf("matches type=%T, want=[]skillsMatchPreviewItem", resp["matches"])
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches)=%d, want=1", len(matches))
	}
	if matches[0].Name != "后端" {
		t.Fatalf("match[0].Name=%q, want=后端", matches[0].Name)
	}
	if matches[0].MatchedBy != "explicit" {
		t.Fatalf("match[0].MatchedBy=%q, want=explicit", matches[0].MatchedBy)
	}
	if !reflect.DeepEqual(matches[0].MatchedTerms, []string{"@后端"}) {
		t.Fatalf("match[0].MatchedTerms=%v, want=[@后端]", matches[0].MatchedTerms)
	}
}

func TestSkillsMatchPreviewTypedIncludesConfiguredForceSkill(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("后端", `---
description: Go后端规范
force_words: ["@后端"]
---
backend guide`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
		agentSkills: map[string][]string{
			"thread-1": {"后端"},
		},
	}

	raw, err := srv.skillsMatchPreviewTyped(context.Background(), skillsMatchPreviewParams{
		ThreadID: "thread-1",
		Text:     "请按@后端实现",
	})
	if err != nil {
		t.Fatalf("skillsMatchPreviewTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	matches, ok := resp["matches"].([]skillsMatchPreviewItem)
	if !ok {
		t.Fatalf("matches type=%T, want=[]skillsMatchPreviewItem", resp["matches"])
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches)=%d, want=1", len(matches))
	}
	if matches[0].Name != "后端" {
		t.Fatalf("match[0].Name=%q, want=后端", matches[0].Name)
	}
	if matches[0].MatchedBy != "force" {
		t.Fatalf("match[0].MatchedBy=%q, want=force", matches[0].MatchedBy)
	}
	if !reflect.DeepEqual(matches[0].MatchedTerms, []string{"@后端"}) {
		t.Fatalf("match[0].MatchedTerms=%v, want=[@后端]", matches[0].MatchedTerms)
	}
}

func TestSkillsMatchPreviewTypedAtAliasUsesExplicitMatch(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("skill-finder", `---
description: Skill 能力检索
aliases: ["@Skill", "@skill"]
summary: "这段摘要是否存在不应影响 @ 触发"
---
skill helper`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
	}

	raw, err := srv.skillsMatchPreviewTyped(context.Background(), skillsMatchPreviewParams{
		ThreadID: "thread-3",
		Text:     "请按@skill处理这个问题",
	})
	if err != nil {
		t.Fatalf("skillsMatchPreviewTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	matches, ok := resp["matches"].([]skillsMatchPreviewItem)
	if !ok {
		t.Fatalf("matches type=%T, want=[]skillsMatchPreviewItem", resp["matches"])
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches)=%d, want=1", len(matches))
	}
	if matches[0].Name != "skill-finder" {
		t.Fatalf("match[0].Name=%q, want=skill-finder", matches[0].Name)
	}
	if matches[0].MatchedBy != "explicit" {
		t.Fatalf("match[0].MatchedBy=%q, want=explicit", matches[0].MatchedBy)
	}
	if !reflect.DeepEqual(matches[0].MatchedTerms, []string{"@Skill"}) {
		t.Fatalf("match[0].MatchedTerms=%v, want=[@Skill]", matches[0].MatchedTerms)
	}
}

func TestSkillsMatchPreviewTypedSkipsInputSkillAndSupportsAgentID(t *testing.T) {
	tmp := t.TempDir()
	writeSkill := func(name, content string) {
		t.Helper()
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeSkill("brand-guide", `---
description: 品牌设计规范
force_words: ["@brand"]
---
brand guide`)

	srv := &Server{
		skillSvc:  seededSkillService(t, tmp),
		skillsDir: tmp,
	}

	raw, err := srv.skillsMatchPreviewTyped(context.Background(), skillsMatchPreviewParams{
		AgentID: "thread-2",
		Text:    "请按@brand输出",
		Input: []UserInput{
			{Type: "skill", Name: "brand-guide", Content: "manual"},
		},
	})
	if err != nil {
		t.Fatalf("skillsMatchPreviewTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	threadID, _ := resp["thread_id"].(string)
	if threadID != "thread-2" {
		t.Fatalf("thread_id=%q, want=thread-2", threadID)
	}
	matches, ok := resp["matches"].([]skillsMatchPreviewItem)
	if !ok {
		t.Fatalf("matches type=%T, want=[]skillsMatchPreviewItem", resp["matches"])
	}
	if len(matches) != 0 {
		t.Fatalf("len(matches)=%d, want=0", len(matches))
	}
}

func TestSkillsConfigReadAndLocalRead(t *testing.T) {
	tmp := t.TempDir()
	localFile := filepath.Join(tmp, "backend.md")
	if err := os.WriteFile(localFile, []byte("name: backend"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	srv := &Server{
		skillsDir: tmp,
	}
	ctx := context.Background()

	raw, err := srv.skillsConfigReadTyped(ctx, skillsConfigReadParams{AgentID: "thread-1"})
	if err != nil {
		t.Fatalf("skillsConfigReadTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	gotSkills := resp["skills"].([]string)
	if len(gotSkills) != 0 {
		t.Fatalf("skillsConfigReadTyped skills=%v", gotSkills)
	}
	if bound, _ := resp["session_bound"].(bool); bound {
		t.Fatalf("skillsConfigReadTyped session_bound=%v, want=false", resp["session_bound"])
	}

	localRaw, err := srv.skillsLocalReadTyped(ctx, skillsLocalReadParams{Path: localFile})
	if err != nil {
		t.Fatalf("skillsLocalReadTyped error: %v", err)
	}
	localResp := localRaw.(map[string]any)
	skill := localResp["skill"].(map[string]string)
	if skill["path"] != localFile {
		t.Fatalf("local read path=%q, want=%q", skill["path"], localFile)
	}
	if !strings.Contains(skill["content"], "backend") {
		t.Fatalf("local read content=%q", skill["content"])
	}
	if strings.TrimSpace(skill["summary"]) == "" {
		t.Fatalf("local read summary should not be empty")
	}
	if skill["summary_source"] != "generated" {
		t.Fatalf("local read summary_source=%q, want=generated", skill["summary_source"])
	}
}

func TestSkillsLocalDeleteTypedRemovesDirectoryAndThreadBindings(t *testing.T) {
	destRoot := t.TempDir()
	svc := service.NewSkillService(destRoot)
	if _, err := svc.WriteSkillContent("backend", "# backend"); err != nil {
		t.Fatalf("seed backend skill: %v", err)
	}
	if _, err := svc.WriteSkillContent("ops", "# ops"); err != nil {
		t.Fatalf("seed ops skill: %v", err)
	}

	srv := &Server{
		skillsDir: destRoot,
		skillSvc:  svc,
	}

	raw, err := srv.skillsLocalDeleteTyped(context.Background(), skillsLocalDeleteParams{Name: "backend"})
	if err != nil {
		t.Fatalf("skillsLocalDeleteTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("delete response ok=%v, want=true", resp["ok"])
	}
	if removed, _ := resp["removed_agent_bindings"].(int); removed != 0 {
		t.Fatalf("removed_agent_bindings=%v, want=0", resp["removed_agent_bindings"])
	}

	deletedDir, _ := resp["dir"].(string)
	if deletedDir == "" {
		t.Fatalf("delete response dir should not be empty: %v", resp)
	}
	if _, err := os.Stat(deletedDir); !os.IsNotExist(err) {
		t.Fatalf("backend directory should be removed, stat err=%v", err)
	}
}

func TestSkillsLocalDeleteTypedReturnsNotFound(t *testing.T) {
	destRoot := t.TempDir()
	srv := &Server{
		skillsDir:   destRoot,
		skillSvc:    service.NewSkillService(destRoot),
		agentSkills: make(map[string][]string),
	}
	_, err := srv.skillsLocalDeleteTyped(context.Background(), skillsLocalDeleteParams{Name: "missing"})
	if err == nil {
		t.Fatal("skillsLocalDeleteTyped should fail for missing skill")
	}
}

func TestSkillsLocalImportDirCopiesWholeDirectory(t *testing.T) {
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sourceRoot, "resources"), 0o755); err != nil {
		t.Fatalf("mkdir resources: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "resources", "guide.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write resource: %v", err)
	}

	destRoot := t.TempDir()
	srv := &Server{
		skillsDir:   destRoot,
		skillSvc:    service.NewSkillService(destRoot),
		agentSkills: make(map[string][]string),
	}
	raw, err := srv.skillsLocalImportDirTyped(context.Background(), skillsLocalImportDirParams{
		Path: sourceRoot,
	})
	if err != nil {
		t.Fatalf("skillsLocalImportDirTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	skill := resp["skill"].(map[string]any)
	name, _ := skill["name"].(string)
	if name == "" {
		t.Fatalf("imported skill name should not be empty")
	}
	targetRoot, _ := skill["dir"].(string)
	if targetRoot == "" {
		t.Fatalf("imported skill dir should not be empty: %v", skill)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "SKILL.md")); err != nil {
		t.Fatalf("missing copied SKILL.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "resources", "guide.md")); err != nil {
		t.Fatalf("missing copied resource file: %v", err)
	}
}

func TestSkillsLocalImportDirRequiresSkillFile(t *testing.T) {
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	destRoot := t.TempDir()
	srv := &Server{
		skillsDir: destRoot,
		skillSvc:  service.NewSkillService(destRoot),
	}
	_, err := srv.skillsLocalImportDirTyped(context.Background(), skillsLocalImportDirParams{
		Path: sourceRoot,
	})
	if err == nil {
		t.Fatal("skillsLocalImportDirTyped should fail when SKILL.md is missing")
	}
}

func TestSkillsLocalImportDirExpandsParentDirectory(t *testing.T) {
	sourceRoot := t.TempDir()
	sourceA := filepath.Join(sourceRoot, "backend")
	if err := os.MkdirAll(sourceA, 0o755); err != nil {
		t.Fatalf("mkdir sourceA: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceA, "SKILL.md"), []byte("# backend"), 0o644); err != nil {
		t.Fatalf("write sourceA SKILL.md: %v", err)
	}
	sourceB := filepath.Join(sourceRoot, "testing")
	if err := os.MkdirAll(sourceB, 0o755); err != nil {
		t.Fatalf("mkdir sourceB: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceB, "SKILL.md"), []byte("# testing"), 0o644); err != nil {
		t.Fatalf("write sourceB SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "README.md"), []byte("root"), 0o644); err != nil {
		t.Fatalf("write root README.md: %v", err)
	}

	destRoot := t.TempDir()
	srv := &Server{
		skillsDir: destRoot,
		skillSvc:  service.NewSkillService(destRoot),
	}
	raw, err := srv.skillsLocalImportDirTyped(context.Background(), skillsLocalImportDirParams{
		Path: sourceRoot,
	})
	if err != nil {
		t.Fatalf("skillsLocalImportDirTyped parent dir error: %v", err)
	}
	resp := raw.(map[string]any)
	ok, _ := resp["ok"].(bool)
	if !ok {
		t.Fatalf("parent directory import should be ok=true, got=%v", resp["ok"])
	}
	summary := resp["summary"].(map[string]int)
	if summary["requested"] != 2 || summary["imported"] != 2 || summary["failed"] != 0 {
		t.Fatalf("unexpected summary: %v", summary)
	}
	skills := resp["skills"].([]map[string]any)
	if len(skills) != 2 {
		t.Fatalf("expected 2 imported skills, got=%d", len(skills))
	}
	for _, skill := range skills {
		targetDir, _ := skill["dir"].(string)
		if _, err := os.Stat(filepath.Join(targetDir, "SKILL.md")); err != nil {
			t.Fatalf("imported skill missing SKILL.md: %v", err)
		}
	}
}

func TestSkillsLocalImportDirSinglePathsRespectsName(t *testing.T) {
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	destRoot := t.TempDir()
	srv := &Server{
		skillsDir: destRoot,
		skillSvc:  service.NewSkillService(destRoot),
	}
	raw, err := srv.skillsLocalImportDirTyped(context.Background(), skillsLocalImportDirParams{
		Paths: []string{sourceRoot},
		Name:  "backend-custom",
	})
	if err != nil {
		t.Fatalf("skillsLocalImportDirTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	skill := resp["skill"].(map[string]any)
	if got, _ := skill["name"].(string); got != "backend-custom" {
		t.Fatalf("imported skill name=%q, want=backend-custom", got)
	}
	skillFile, _ := skill["skill_file"].(string)
	if skillFile == "" {
		t.Fatalf("imported skill_file should not be empty: %v", skill)
	}
	if _, err := os.Stat(skillFile); err != nil {
		t.Fatalf("missing imported SKILL.md: %v", err)
	}
}

func TestSkillsLocalImportDirKeepsExistingSkillWhenImportFails(t *testing.T) {
	destRoot := t.TempDir()
	svc := service.NewSkillService(destRoot)
	existingSkillPath, err := svc.WriteSkillContent("backend", "old-version")
	if err != nil {
		t.Fatalf("write existing SKILL.md: %v", err)
	}

	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatalf("write source SKILL.md: %v", err)
	}
	tooLarge := make([]byte, (4<<20)+1)
	if err := os.WriteFile(filepath.Join(sourceRoot, "huge.bin"), tooLarge, 0o644); err != nil {
		t.Fatalf("write huge file: %v", err)
	}

	srv := &Server{
		skillsDir: destRoot,
		skillSvc:  svc,
	}
	_, err = srv.skillsLocalImportDirTyped(context.Background(), skillsLocalImportDirParams{
		Path: sourceRoot,
		Name: "backend",
	})
	if err == nil {
		t.Fatal("skillsLocalImportDirTyped should fail when source file is too large")
	}

	content, readErr := os.ReadFile(existingSkillPath)
	if readErr != nil {
		t.Fatalf("existing skill should remain after failed import: %v", readErr)
	}
	if string(content) != "old-version" {
		t.Fatalf("existing skill content changed after failed import: %q", string(content))
	}
}

func TestSkillsLocalImportDirBatchImportsMultipleDirectories(t *testing.T) {
	sourceA := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceA, "SKILL.md"), []byte("# A"), 0o644); err != nil {
		t.Fatalf("write sourceA SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceA, "README.md"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write sourceA README.md: %v", err)
	}

	sourceB := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceB, "SKILL.md"), []byte("# B"), 0o644); err != nil {
		t.Fatalf("write sourceB SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sourceB, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir sourceB assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceB, "assets", "guide.md"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write sourceB guide.md: %v", err)
	}

	destRoot := t.TempDir()
	srv := &Server{
		skillsDir: destRoot,
		skillSvc:  service.NewSkillService(destRoot),
	}
	raw, err := srv.skillsLocalImportDirTyped(context.Background(), skillsLocalImportDirParams{
		Paths: []string{sourceA, sourceB},
	})
	if err != nil {
		t.Fatalf("skillsLocalImportDirTyped batch error: %v", err)
	}
	resp := raw.(map[string]any)

	ok, _ := resp["ok"].(bool)
	if !ok {
		t.Fatalf("batch import should be ok=true, got=%v", resp["ok"])
	}
	summary := resp["summary"].(map[string]int)
	if summary["requested"] != 2 || summary["imported"] != 2 || summary["failed"] != 0 {
		t.Fatalf("unexpected summary: %v", summary)
	}

	skills := resp["skills"].([]map[string]any)
	if len(skills) != 2 {
		t.Fatalf("expected 2 imported skills, got=%d", len(skills))
	}
	for _, skill := range skills {
		name, _ := skill["name"].(string)
		if name == "" {
			t.Fatalf("skill name should not be empty: %v", skill)
		}
		targetDir, _ := skill["dir"].(string)
		if _, err := os.Stat(filepath.Join(targetDir, "SKILL.md")); err != nil {
			t.Fatalf("imported skill missing SKILL.md: %v", err)
		}
	}

	failures := resp["failures"].([]map[string]string)
	if len(failures) != 0 {
		t.Fatalf("expected no failures, got=%v", failures)
	}
}

func TestSkillsLocalImportDirBatchCollectsFailures(t *testing.T) {
	validSource := t.TempDir()
	if err := os.WriteFile(filepath.Join(validSource, "SKILL.md"), []byte("# Valid"), 0o644); err != nil {
		t.Fatalf("write valid SKILL.md: %v", err)
	}
	invalidSource := t.TempDir()
	if err := os.WriteFile(filepath.Join(invalidSource, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write invalid README.md: %v", err)
	}

	destRoot := t.TempDir()
	srv := &Server{
		skillsDir: destRoot,
		skillSvc:  service.NewSkillService(destRoot),
	}
	raw, err := srv.skillsLocalImportDirTyped(context.Background(), skillsLocalImportDirParams{
		Paths: []string{validSource, invalidSource},
	})
	if err != nil {
		t.Fatalf("skillsLocalImportDirTyped batch should not hard fail: %v", err)
	}
	resp := raw.(map[string]any)

	ok, _ := resp["ok"].(bool)
	if ok {
		t.Fatalf("batch import should be ok=false when there are failures")
	}
	summary := resp["summary"].(map[string]int)
	if summary["requested"] != 2 || summary["imported"] != 1 || summary["failed"] != 1 {
		t.Fatalf("unexpected summary: %v", summary)
	}

	skills := resp["skills"].([]map[string]any)
	if len(skills) != 1 {
		t.Fatalf("expected 1 imported skill, got=%d", len(skills))
	}
	failures := resp["failures"].([]map[string]string)
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure, got=%d", len(failures))
	}
	if failures[0]["source"] != invalidSource {
		t.Fatalf("failure source=%q, want=%q", failures[0]["source"], invalidSource)
	}
}
