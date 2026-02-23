package uistate

import "testing"

func TestSanitizeUserMessageText_TrimsInjectedSkillBlock(t *testing.T) {
	input := "前面一句话\n[skill:品牌设计规范] 摘要: 品牌规范摘要\n可选段落: 品牌设计规范 (SKILL.md:4)\n使用方式: 按任务选择相关段落，忽略无关内容。"
	got := sanitizeUserMessageText(input)
	if got != "前面一句话" {
		t.Fatalf("sanitizeUserMessageText=%q, want %q", got, "前面一句话")
	}
}

func TestSanitizeUserMessageText_ManualSkillTagKeepsText(t *testing.T) {
	input := "请按 [skill:品牌设计规范] 处理，但不要自动拼接摘要"
	got := sanitizeUserMessageText(input)
	if got != input {
		t.Fatalf("sanitizeUserMessageText=%q, want original text", got)
	}
}

func TestHandleUserMessageEvent_SkipsSkillOnlyInjectedMessage(t *testing.T) {
	mgr := NewRuntimeManager()
	threadID := "thread-user-sanitize"
	mgr.ApplyAgentEvent(threadID, NormalizedEvent{
		UIType: UITypeUserMessage,
		Text:   "[skill:技能查找] 摘要: 用于搜索技能\n可选段落: 技能查找 (SKILL.md:4)\n使用方式: 按任务选择相关段落，忽略无关内容。",
	}, nil)

	timeline := mgr.ThreadTimeline(threadID)
	if len(timeline) != 0 {
		t.Fatalf("timeline len=%d, want=0", len(timeline))
	}
}

func TestSanitizeUserMessageTextWithMode_NoTrimKeepsInjectedBlocks(t *testing.T) {
	input := "问题本体\n已注入 LSP/Playwright/json-render/code_run 工具。使用规则：\n..."
	got := sanitizeUserMessageTextWithMode(input, false)
	if got != input {
		t.Fatalf("sanitizeUserMessageTextWithMode=%q, want original text", got)
	}
}

func TestHandleUserMessageEvent_NoTrimModeKeepsInjectedBlocks(t *testing.T) {
	mgr := NewRuntimeManager()
	mgr.SetSanitizeInjectedUserMessage(false)
	threadID := "thread-user-no-trim"
	input := "问题本体\n已注入 LSP/Playwright/json-render/code_run 工具。使用规则：\n..."

	mgr.ApplyAgentEvent(threadID, NormalizedEvent{
		UIType: UITypeUserMessage,
		Text:   input,
	}, nil)

	timeline := mgr.ThreadTimeline(threadID)
	if len(timeline) != 1 {
		t.Fatalf("timeline len=%d, want=1", len(timeline))
	}
	if timeline[0].Text != input {
		t.Fatalf("timeline text=%q, want original text", timeline[0].Text)
	}
}
