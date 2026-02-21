package codex

import (
	"os"
	"path/filepath"
	"testing"
)

// ── extractRolloutText ──────────────────────────────────────

func TestExtractRolloutText_SingleItem(t *testing.T) {
	items := []rolloutContentItem{{Type: "output_text", Text: "hello world"}}
	got := extractRolloutText(items)
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}

func TestExtractRolloutText_MultipleItems(t *testing.T) {
	items := []rolloutContentItem{
		{Type: "output_text", Text: "hello "},
		{Type: "output_text", Text: "world"},
	}
	got := extractRolloutText(items)
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}

func TestExtractRolloutText_Empty(t *testing.T) {
	got := extractRolloutText(nil)
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// ── isSystemNoise ───────────────────────────────────────────

func TestIsSystemNoise_AgentsMd(t *testing.T) {
	if !isSystemNoise("# AGENTS.md\nsome content") {
		t.Fatal("expected AGENTS.md to be noise")
	}
}

func TestIsSystemNoise_EnvironmentContext(t *testing.T) {
	if !isSystemNoise("<environment_context>data</environment_context>") {
		t.Fatal("expected environment_context to be noise")
	}
}

func TestIsSystemNoise_PermissionsInstructions(t *testing.T) {
	if !isSystemNoise("<permissions instructions>do something</permissions instructions>") {
		t.Fatal("expected permissions instructions to be noise")
	}
}

func TestIsSystemNoise_Instructions(t *testing.T) {
	if !isSystemNoise("<INSTRUCTIONS>something</INSTRUCTIONS>") {
		t.Fatal("expected INSTRUCTIONS to be noise")
	}
}

func TestIsSystemNoise_NormalText(t *testing.T) {
	if isSystemNoise("help me write tests") {
		t.Fatal("normal text should not be noise")
	}
}

// ── trimLSPInjection ────────────────────────────────────────

func TestTrimLSPInjection_WithMarker(t *testing.T) {
	input := "user question\n已注入 LSP context data"
	got := trimLSPInjection(input)
	if got != "user question" {
		t.Fatalf("got %q, want %q", got, "user question")
	}
}

func TestTrimLSPInjection_WithoutMarker(t *testing.T) {
	input := "user question without injection"
	got := trimLSPInjection(input)
	if got != input {
		t.Fatalf("got %q, want %q", got, input)
	}
}

func TestTrimSkillInjection_WithGeneratedDigest(t *testing.T) {
	input := "UI 需要改\n[skill:品牌设计规范] 摘要: 品牌规范摘要\n可选段落: 色彩系统 (SKILL.md:12)\n使用方式: 按任务选择相关段落，忽略无关内容。"
	got := trimSkillInjection(input)
	if got != "UI 需要改" {
		t.Fatalf("got %q, want %q", got, "UI 需要改")
	}
}

func TestTrimSkillInjection_ManualSkillTagShouldKeep(t *testing.T) {
	input := "请按 [skill:品牌设计规范] 的思路输出，但不要自动注入"
	got := trimSkillInjection(input)
	if got != input {
		t.Fatalf("got %q, want %q", got, input)
	}
}

// ── ReadRolloutMessages ─────────────────────────────────────

func TestReadRolloutMessages_BasicParsing(t *testing.T) {
	content := `{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}
{"timestamp":"2026-02-20T01:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi there"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("msg[0] = %+v, want user/hello", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hi there" {
		t.Fatalf("msg[1] = %+v, want assistant/'hi there'", msgs[1])
	}
}

func TestReadRolloutMessages_FiltersDeveloperRole(t *testing.T) {
	content := `{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"system prompt"}]}}
{"timestamp":"2026-02-20T01:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"real question"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1 (developer filtered)", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Fatalf("msg[0].Role = %q, want user", msgs[0].Role)
	}
}

func TestReadRolloutMessages_FiltersSystemNoise(t *testing.T) {
	content := `{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md\nsome content"}]}}
{"timestamp":"2026-02-20T01:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"actual question"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1 (noise filtered)", len(msgs))
	}
	if msgs[0].Content != "actual question" {
		t.Fatalf("msg[0].Content = %q, want 'actual question'", msgs[0].Content)
	}
}

func TestReadRolloutMessages_TrimsLSPInjection(t *testing.T) {
	content := `{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"my question\n已注入 LSP context"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Content != "my question" {
		t.Fatalf("msg[0].Content = %q, want 'my question'", msgs[0].Content)
	}
}

func TestReadRolloutMessages_TrimsSkillInjection(t *testing.T) {
	content := `{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"前面一句话\n[skill:品牌设计规范] 摘要: 品牌规范摘要\n可选段落: 品牌设计规范 (SKILL.md:4)\n使用方式: 按任务选择相关段落，忽略无关内容。"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Content != "前面一句话" {
		t.Fatalf("msg[0].Content = %q, want '前面一句话'", msgs[0].Content)
	}
}

func TestReadRolloutMessages_SkipsNonResponseItem(t *testing.T) {
	content := `{"timestamp":"2026-02-20T01:00:00Z","type":"session_start","payload":{}}
{"timestamp":"2026-02-20T01:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1 (non-response_item skipped)", len(msgs))
	}
}

func TestReadRolloutMessages_SkipsEmptyContent(t *testing.T) {
	content := `{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[]}}
{"timestamp":"2026-02-20T01:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1 (empty content skipped)", len(msgs))
	}
}

func TestReadRolloutMessages_FileNotFound(t *testing.T) {
	_, err := ReadRolloutMessages("/nonexistent/path.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadRolloutMessages_SkipsMalformedJSON(t *testing.T) {
	content := `not json at all
{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"valid"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1 (malformed skipped)", len(msgs))
	}
}

func TestReadRolloutMessages_UserOnlyLSPInjection_SkippedAsEmpty(t *testing.T) {
	// user 消息去除 LSP 注入后为空 → 跳过
	content := `{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"\n已注入 LSP context"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("got %d messages, want 0 (LSP-only user msg skipped)", len(msgs))
	}
}

func TestReadRolloutMessages_UserOnlySkillInjection_SkippedAsEmpty(t *testing.T) {
	content := `{"timestamp":"2026-02-20T01:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"[skill:技能查找] 摘要: 用于搜索技能\n可选段落: 技能查找 (SKILL.md:4)\n使用方式: 按任务选择相关段落，忽略无关内容。"}]}}
`
	path := writeTemp(t, content)
	msgs, err := ReadRolloutMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("got %d messages, want 0 (skill-only user msg skipped)", len(msgs))
	}
}

// ── FindRolloutPath ─────────────────────────────────────────

func TestFindRolloutPath_EmptyThreadID(t *testing.T) {
	_, err := FindRolloutPath("")
	if err == nil {
		t.Fatal("expected error for empty thread id")
	}
}

// ── writeTemp helper ────────────────────────────────────────

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-test.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
