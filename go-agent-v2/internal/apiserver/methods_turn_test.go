// methods_turn_test.go — 重构护栏: turn 相关纯函数的行为基线测试。
package apiserver

import (
	"errors"
	"testing"
)

// ========================================
// normalizeInterruptState
// ========================================

func TestNormalizeInterruptState(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", "idle"},
		{"whitespace", "  ", "idle"},
		{"completed", "completed", "idle"},
		{"complete", "Complete", "idle"},
		{"done", "done", "idle"},
		{"success", "SUCCESS", "idle"},
		{"succeeded", "succeeded", "idle"},
		{"ready", "READY", "idle"},
		{"stopped", "stopped", "idle"},
		{"ended", "ended", "idle"},
		{"closed", "CLOSED", "idle"},
		{"failed", "failed", "error"},
		{"fail", "FAIL", "error"},
		{"thinking", "thinking", "thinking"},
		{"running", "Running", "running"},
		{"unknown_state", "foobar", "foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeInterruptState(tt.raw)
			if got != tt.want {
				t.Errorf("normalizeInterruptState(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// ========================================
// isInterruptActiveState
// ========================================

func TestIsInterruptActiveState(t *testing.T) {
	actives := []string{"starting", "thinking", "responding", "running", "editing", "waiting", "syncing"}
	for _, s := range actives {
		if !isInterruptActiveState(s) {
			t.Errorf("isInterruptActiveState(%q) = false, want true", s)
		}
	}
	inactives := []string{"", "idle", "completed", "failed", "error", "unknown"}
	for _, s := range inactives {
		if isInterruptActiveState(s) {
			t.Errorf("isInterruptActiveState(%q) = true, want false", s)
		}
	}
}

// ========================================
// isInterruptNoActiveTurnError
// ========================================

func TestIsInterruptNoActiveTurnError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"no_active_turn", errors.New("No active turn"), true},
		{"nothing_to_interrupt", errors.New("nothing to interrupt here"), true},
		{"not_interruptible", errors.New("state is NOT INTERRUPTIBLE"), true},
		{"random_error", errors.New("network timeout"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isInterruptNoActiveTurnError(tt.err)
			if got != tt.want {
				t.Errorf("isInterruptNoActiveTurnError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ========================================
// interruptSettleMode
// ========================================

func TestInterruptSettleMode(t *testing.T) {
	tests := []struct {
		name       string
		confirmed  bool
		afterState string
		want       string
	}{
		{"confirmed", true, "anything", "interrupt_confirmed"},
		{"terminal_failed", false, "failed", "interrupt_terminal_failed"},
		{"terminal_completed", false, "completed", "interrupt_terminal_completed"},
		{"terminal_idle", false, "idle", "interrupt_terminal_completed"},
		{"timeout_running", false, "running", "interrupt_timeout"},
		{"timeout_unknown", false, "foobar", "interrupt_timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interruptSettleMode(tt.confirmed, tt.afterState)
			if got != tt.want {
				t.Errorf("interruptSettleMode(%v, %q) = %q, want %q", tt.confirmed, tt.afterState, got, tt.want)
			}
		})
	}
}

// ========================================
// fuzzyMatch
// ========================================

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		pattern string
		want    bool
	}{
		{"exact", "hello", "hello", true},
		{"subsequence", "hello world", "hlo", true},
		{"no_match", "hello", "xyz", false},
		{"empty_pattern", "hello", "", true},
		{"empty_text", "", "a", false},
		{"both_empty", "", "", true},
		{"partial", "abcdefg", "aceg", true},
		{"order_matters", "abc", "cba", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fuzzyMatch(tt.text, tt.pattern)
			if got != tt.want {
				t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.text, tt.pattern, got, tt.want)
			}
		})
	}
}

// ========================================
// normalizeSkillName / normalizeSkillNames
// ========================================

func TestNormalizeSkillName(t *testing.T) {
	// valid
	if name, err := normalizeSkillName("  MySkill  "); err != nil || name != "MySkill" {
		t.Errorf("normalizeSkillName(\"  MySkill  \") = %q, %v; want \"MySkill\", nil", name, err)
	}
	// empty
	if _, err := normalizeSkillName(""); err == nil {
		t.Error("normalizeSkillName(\"\") expected error, got nil")
	}
	// whitespace only
	if _, err := normalizeSkillName("   "); err == nil {
		t.Error("normalizeSkillName(\"   \") expected error, got nil")
	}
}

func TestNormalizeSkillNames(t *testing.T) {
	// empty input
	names, err := normalizeSkillNames(nil)
	if err != nil || len(names) != 0 {
		t.Errorf("normalizeSkillNames(nil) = %v, %v; want [], nil", names, err)
	}
	// dedup
	names, err = normalizeSkillNames([]string{"A", "a", "B"})
	if err != nil {
		t.Fatalf("normalizeSkillNames: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("normalizeSkillNames dedup: got %d names, want 2", len(names))
	}
	// error propagation
	_, err = normalizeSkillNames([]string{"ok", ""})
	if err == nil {
		t.Error("normalizeSkillNames with empty name expected error, got nil")
	}
}

// ========================================
// mergePromptText
// ========================================

func TestMergePromptText(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		extra  string
		want   string
	}{
		{"both_empty", "", "", ""},
		{"prompt_only", "hello", "", "hello"},
		{"extra_only", "", "world", "world"},
		{"both", "hello", "world", "hello\nworld"},
		{"whitespace_extra", "hello", "   ", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergePromptText(tt.prompt, tt.extra)
			if got != tt.want {
				t.Errorf("mergePromptText(%q, %q) = %q, want %q", tt.prompt, tt.extra, got, tt.want)
			}
		})
	}
}

// ========================================
// collectInputSkillNames
// ========================================

func TestCollectInputSkillNames(t *testing.T) {
	// empty
	if got := collectInputSkillNames(nil); got != nil {
		t.Errorf("collectInputSkillNames(nil) = %v, want nil", got)
	}
	// mixed
	inputs := []UserInput{
		{Type: "skill", Name: "MySkill"},
		{Type: "text", Text: "hello"},
		{Type: "skill", Name: "myskill"}, // duplicate (case-insensitive)
		{Type: "skill", Name: ""},        // empty name
		{Type: "Skill", Name: "OtherSkill"},
	}
	got := collectInputSkillNames(inputs)
	if len(got) != 2 {
		t.Errorf("collectInputSkillNames: got %d entries, want 2", len(got))
	}
	if _, ok := got["myskill"]; !ok {
		t.Error("collectInputSkillNames: missing 'myskill'")
	}
	if _, ok := got["otherskill"]; !ok {
		t.Error("collectInputSkillNames: missing 'otherskill'")
	}
}

// ========================================
// collectSkillNameSet
// ========================================

func TestCollectSkillNameSet(t *testing.T) {
	if got := collectSkillNameSet(nil); got != nil {
		t.Errorf("collectSkillNameSet(nil) = %v, want nil", got)
	}
	got := collectSkillNameSet([]string{"A", "b", " A ", "", "C"})
	if len(got) != 3 {
		t.Errorf("collectSkillNameSet: got %d, want 3", len(got))
	}
}

// ========================================
// composeUserTimelineTextForTurn
// ========================================

func TestComposeUserTimelineTextForTurn(t *testing.T) {
	// showInjected=false
	got := composeUserTimelineTextForTurn("user", "submit", "hint", false)
	if got != "user" {
		t.Errorf("showInjected=false: got %q, want %q", got, "user")
	}
	// showInjected=true, hint empty
	got = composeUserTimelineTextForTurn("user", "submit", "", true)
	if got != "submit" {
		t.Errorf("showInjected=true, empty hint: got %q, want %q", got, "submit")
	}
	// showInjected=true, hint already in submitPrompt
	got = composeUserTimelineTextForTurn("user", "submit includes hint", "hint", true)
	if got != "submit includes hint" {
		t.Errorf("hint in submitPrompt: got %q, want %q", got, "submit includes hint")
	}
	// showInjected=true, hint not in submitPrompt
	got = composeUserTimelineTextForTurn("user", "submit", "extra_hint", true)
	if got != "submit\nextra_hint" {
		t.Errorf("hint not in submitPrompt: got %q, want %q", got, "submit\nextra_hint")
	}
}

// ========================================
// lowerMatchedTerms
// ========================================

func TestLowerMatchedTerms(t *testing.T) {
	if got := lowerMatchedTerms("", []string{"a"}); got != nil {
		t.Errorf("empty text: got %v, want nil", got)
	}
	if got := lowerMatchedTerms("text", nil); got != nil {
		t.Errorf("nil candidates: got %v, want nil", got)
	}
	got := lowerMatchedTerms("hello world foo", []string{"hello", "bar", "World", "HELLO"})
	if len(got) != 2 {
		t.Errorf("matched: got %d, want 2: %v", len(got), got)
	}
}

// ========================================
// skillInputText / fileContentInputText
// ========================================

func TestSkillInputText(t *testing.T) {
	got := skillInputText("  mySkill  ", "content here")
	want := "[skill:mySkill] content here"
	if got != want {
		t.Errorf("skillInputText = %q, want %q", got, want)
	}
}

func TestFileContentInputText(t *testing.T) {
	// both present
	got := fileContentInputText("file.go", "package main")
	want := "[file:file.go]\npackage main"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// empty content
	if got := fileContentInputText("file.go", "  "); got != "" {
		t.Errorf("empty content: got %q, want empty", got)
	}
	// empty name
	got = fileContentInputText("", "content")
	if got != "content" {
		t.Errorf("empty name: got %q, want %q", got, "content")
	}
}
