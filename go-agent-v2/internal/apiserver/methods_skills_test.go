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
		skillSvc:  service.NewSkillService(tmp),
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

func TestSkillsConfigWriteAgentSkillsLifecycle(t *testing.T) {
	srv := &Server{agentSkills: make(map[string][]string)}
	ctx := context.Background()

	raw, err := srv.skillsConfigWriteTyped(ctx, skillsConfigWriteParams{
		AgentID: "thread-1",
		Skills:  []string{" backend ", "TDD", "backend"},
	})
	if err != nil {
		t.Fatalf("skillsConfigWriteTyped set error: %v", err)
	}
	resp := raw.(map[string]any)
	gotSkills := resp["skills"].([]string)
	wantSkills := []string{"backend", "TDD"}
	if !reflect.DeepEqual(gotSkills, wantSkills) {
		t.Fatalf("configured skills=%v, want=%v", gotSkills, wantSkills)
	}

	readonly := srv.GetAgentSkills("thread-1")
	readonly[0] = "mutated"
	after := srv.GetAgentSkills("thread-1")
	if !reflect.DeepEqual(after, wantSkills) {
		t.Fatalf("GetAgentSkills should return copy, got=%v", after)
	}

	_, err = srv.skillsConfigWriteTyped(ctx, skillsConfigWriteParams{
		AgentID: "thread-1",
		Skills:  []string{},
	})
	if err != nil {
		t.Fatalf("skillsConfigWriteTyped clear error: %v", err)
	}
	if got := srv.GetAgentSkills("thread-1"); len(got) != 0 {
		t.Fatalf("skills should be cleared, got=%v", got)
	}
}

func TestSkillsConfigWriteAndRemoteWriteUseConfiguredDirectory(t *testing.T) {
	tmp := t.TempDir()
	srv := &Server{
		skillsDir:   tmp,
		agentSkills: make(map[string][]string),
	}
	ctx := context.Background()

	_, err := srv.skillsConfigWriteTyped(ctx, skillsConfigWriteParams{
		Name:    "backend",
		Content: "name: backend",
	})
	if err != nil {
		t.Fatalf("skillsConfigWriteTyped file mode error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "backend", "SKILL.md")); err != nil {
		t.Fatalf("expected skill file in configured dir: %v", err)
	}

	if _, err := srv.skillsRemoteWriteTyped(ctx, skillsRemoteWriteParams{
		Name:    "../bad",
		Content: "x",
	}); err == nil {
		t.Fatalf("skillsRemoteWriteTyped should reject invalid skill name")
	}

	_, err = srv.skillsRemoteWriteTyped(ctx, skillsRemoteWriteParams{
		Name:    "tdd",
		Content: "name: tdd",
	})
	if err != nil {
		t.Fatalf("skillsRemoteWriteTyped valid name error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "tdd", "SKILL.md")); err != nil {
		t.Fatalf("expected remote skill file in configured dir: %v", err)
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

	srv := &Server{skillsDir: tmp}
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
	updated, err := os.ReadFile(path)
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

	srv := &Server{
		skillSvc:  service.NewSkillService(tmp),
		skillsDir: tmp,
		agentSkills: map[string][]string{
			"thread-1": {"backend", "tdd", "missing"},
		},
	}

	input := []UserInput{
		{Type: "skill", Name: "tdd", Content: "manual tdd"},
	}
	prompt, count := srv.buildConfiguredSkillPrompt("thread-1", input)
	if count != 1 {
		t.Fatalf("configured skill count=%d, want=1", count)
	}
	if !strings.Contains(prompt, "[skill:backend]") {
		t.Fatalf("expected backend skill in prompt, got=%q", prompt)
	}
	if !strings.Contains(prompt, "摘要: backend-summary") {
		t.Fatalf("expected backend summary in prompt, got=%q", prompt)
	}
	if strings.Contains(prompt, "FULL BACKEND DETAIL SHOULD NOT INJECT") {
		t.Fatalf("full skill body should not be injected, got=%q", prompt)
	}
	if strings.Contains(prompt, "[skill:tdd]") {
		t.Fatalf("input skill should skip configured duplicate, got=%q", prompt)
	}
	if strings.Contains(prompt, "[skill:missing]") {
		t.Fatalf("missing skill should be skipped, got=%q", prompt)
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

	srv := &Server{
		skillSvc:  service.NewSkillService(tmp),
		skillsDir: tmp,
	}
	input := []UserInput{
		{Type: "skill", Name: "tdd", Content: "manual tdd"},
	}
	prompt, count := srv.buildSelectedSkillPrompt([]string{"backend", "tdd", "missing"}, input)
	if count != 1 {
		t.Fatalf("selected skill count=%d, want=1", count)
	}
	if !strings.Contains(prompt, "[skill:backend]") {
		t.Fatalf("expected backend selected skill in prompt, got=%q", prompt)
	}
	if !strings.Contains(prompt, "摘要: backend-summary") {
		t.Fatalf("expected backend summary in prompt, got=%q", prompt)
	}
	if strings.Contains(prompt, "FULL BACKEND DETAIL SHOULD NOT INJECT") {
		t.Fatalf("full skill body should not be injected, got=%q", prompt)
	}
	if strings.Contains(prompt, "[skill:tdd]") {
		t.Fatalf("input skill should skip selected duplicate, got=%q", prompt)
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
		skillSvc:  service.NewSkillService(tmp),
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
	if !strings.Contains(prompt, "摘要: test helper") {
		t.Fatalf("expected summary-based auto matched prompt, got=%q", prompt)
	}
	if strings.Contains(prompt, "tdd skill") {
		t.Fatalf("full skill body should not be injected, got=%q", prompt)
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
		skillSvc:  service.NewSkillService(tmp),
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
		skillSvc:  service.NewSkillService(tmp),
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
		skillSvc:  service.NewSkillService(tmp),
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
	if strings.Contains(prompt, "FULL BACKEND DETAIL SHOULD NOT INJECT") {
		t.Fatalf("full skill body should not be injected, got=%q", prompt)
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
		skillSvc:  service.NewSkillService(tmp),
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
	if strings.Contains(prompt, "FULL BACKEND DETAIL SHOULD NOT INJECT") {
		t.Fatalf("full skill body should not be injected, got=%q", prompt)
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
		skillSvc:  service.NewSkillService(tmp),
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
		skillSvc:  service.NewSkillService(tmp),
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
		skillSvc:  service.NewSkillService(tmp),
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
		skillSvc:  service.NewSkillService(tmp),
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
		skillSvc:  service.NewSkillService(tmp),
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
		agentSkills: map[string][]string{
			"thread-1": {"backend"},
		},
	}
	ctx := context.Background()

	raw, err := srv.skillsConfigReadTyped(ctx, skillsConfigReadParams{AgentID: "thread-1"})
	if err != nil {
		t.Fatalf("skillsConfigReadTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	gotSkills := resp["skills"].([]string)
	if !reflect.DeepEqual(gotSkills, []string{"backend"}) {
		t.Fatalf("skillsConfigReadTyped skills=%v", gotSkills)
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
	if err := os.MkdirAll(filepath.Join(destRoot, "backend"), 0o755); err != nil {
		t.Fatalf("mkdir backend dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destRoot, "backend", "SKILL.md"), []byte("# backend"), 0o644); err != nil {
		t.Fatalf("write backend SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(destRoot, "ops"), 0o755); err != nil {
		t.Fatalf("mkdir ops dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destRoot, "ops", "SKILL.md"), []byte("# ops"), 0o644); err != nil {
		t.Fatalf("write ops SKILL.md: %v", err)
	}

	srv := &Server{
		skillsDir: destRoot,
		agentSkills: map[string][]string{
			"thread-1": {"backend", "ops"},
			"thread-2": {"BACKEND", "review"},
			"thread-3": {"ops"},
		},
	}

	raw, err := srv.skillsLocalDeleteTyped(context.Background(), skillsLocalDeleteParams{Name: "backend"})
	if err != nil {
		t.Fatalf("skillsLocalDeleteTyped error: %v", err)
	}
	resp := raw.(map[string]any)
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("delete response ok=%v, want=true", resp["ok"])
	}
	if removed, _ := resp["removed_agent_bindings"].(int); removed != 2 {
		t.Fatalf("removed_agent_bindings=%v, want=2", resp["removed_agent_bindings"])
	}

	if _, err := os.Stat(filepath.Join(destRoot, "backend")); !os.IsNotExist(err) {
		t.Fatalf("backend directory should be removed, stat err=%v", err)
	}
	if got := srv.GetAgentSkills("thread-1"); !reflect.DeepEqual(got, []string{"ops"}) {
		t.Fatalf("thread-1 skills=%v, want=[ops]", got)
	}
	if got := srv.GetAgentSkills("thread-2"); !reflect.DeepEqual(got, []string{"review"}) {
		t.Fatalf("thread-2 skills=%v, want=[review]", got)
	}
	if got := srv.GetAgentSkills("thread-3"); !reflect.DeepEqual(got, []string{"ops"}) {
		t.Fatalf("thread-3 skills=%v, want=[ops]", got)
	}
}

func TestSkillsLocalDeleteTypedReturnsNotFound(t *testing.T) {
	srv := &Server{skillsDir: t.TempDir(), agentSkills: make(map[string][]string)}
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
	targetRoot := filepath.Join(destRoot, name)
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
	srv := &Server{skillsDir: destRoot}
	_, err := srv.skillsLocalImportDirTyped(context.Background(), skillsLocalImportDirParams{
		Path: sourceRoot,
	})
	if err == nil {
		t.Fatal("skillsLocalImportDirTyped should fail when SKILL.md is missing")
	}
}

func TestSkillsLocalImportDirSinglePathsRespectsName(t *testing.T) {
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	destRoot := t.TempDir()
	srv := &Server{skillsDir: destRoot}
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
	if _, err := os.Stat(filepath.Join(destRoot, "backend-custom", "SKILL.md")); err != nil {
		t.Fatalf("missing imported SKILL.md: %v", err)
	}
}

func TestSkillsLocalImportDirKeepsExistingSkillWhenImportFails(t *testing.T) {
	destRoot := t.TempDir()
	existingDir := filepath.Join(destRoot, "backend")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("mkdir existing skill dir: %v", err)
	}
	existingSkillPath := filepath.Join(existingDir, "SKILL.md")
	if err := os.WriteFile(existingSkillPath, []byte("old-version"), 0o644); err != nil {
		t.Fatalf("write existing SKILL.md: %v", err)
	}

	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatalf("write source SKILL.md: %v", err)
	}
	tooLarge := make([]byte, maxSkillImportSingleFileSize+1)
	if err := os.WriteFile(filepath.Join(sourceRoot, "huge.bin"), tooLarge, 0o644); err != nil {
		t.Fatalf("write huge file: %v", err)
	}

	srv := &Server{skillsDir: destRoot}
	_, err := srv.skillsLocalImportDirTyped(context.Background(), skillsLocalImportDirParams{
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
	srv := &Server{skillsDir: destRoot}
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
	srv := &Server{skillsDir: destRoot}
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
