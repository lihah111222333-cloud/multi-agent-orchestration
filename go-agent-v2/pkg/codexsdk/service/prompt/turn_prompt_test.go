package prompt

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuildSelectedSkillPrompt(t *testing.T) {
	t.Run("dedupe and skip missing skills", func(t *testing.T) {
		readSkill := func(skillName string) (string, error) {
			switch strings.ToLower(strings.TrimSpace(skillName)) {
			case "skill-a":
				return "content-a", nil
			case "skill-b":
				return "", errors.New("missing")
			default:
				return "", errors.New("unknown")
			}
		}

		prompt, count := buildSelectedSkillPrompt(
			[]string{"skill-a", "SKILL-A", "skill-b"},
			readSkill,
			nil,
		)
		if count != 1 {
			t.Fatalf("buildSelectedSkillPrompt() count = %d, want 1", count)
		}
		if !strings.Contains(prompt, "content-a") {
			t.Fatalf("buildSelectedSkillPrompt() prompt = %q, want contains content-a", prompt)
		}
		if strings.Contains(prompt, "skill-b") {
			t.Fatalf("buildSelectedSkillPrompt() prompt = %q, want no missing skill content", prompt)
		}
	})

	t.Run("custom skillInputText", func(t *testing.T) {
		prompt, count := buildSelectedSkillPrompt(
			[]string{"skill-a"},
			func(skillName string) (string, error) { return "raw", nil },
			func(name, content string) string { return name + ":" + content },
		)
		if count != 1 {
			t.Fatalf("buildSelectedSkillPrompt() count = %d, want 1", count)
		}
		if prompt != "skill-a:raw" {
			t.Fatalf("buildSelectedSkillPrompt() prompt = %q, want skill-a:raw", prompt)
		}
	})
}

func TestResolveLSPUsagePromptHint(t *testing.T) {
	t.Run("nil getter uses default", func(t *testing.T) {
		got := resolveLSPUsagePromptHint(context.Background(), "default-hint", 100, nil)
		if got != "default-hint" {
			t.Fatalf("resolveLSPUsagePromptHint() = %q, want default-hint", got)
		}
	})

	t.Run("too long hint fallback", func(t *testing.T) {
		got := resolveLSPUsagePromptHint(
			context.Background(),
			"default-hint",
			8,
			func(context.Context, string) (any, error) {
				return "this-hint-is-too-long", nil
			},
		)
		if got != "default-hint" {
			t.Fatalf("resolveLSPUsagePromptHint() = %q, want default-hint", got)
		}
	})

	t.Run("valid hint returned", func(t *testing.T) {
		got := resolveLSPUsagePromptHint(
			context.Background(),
			"default-hint",
			64,
			func(context.Context, string) (any, error) {
				return "  real-hint  ", nil
			},
		)
		if got != "real-hint" {
			t.Fatalf("resolveLSPUsagePromptHint() = %q, want real-hint", got)
		}
	})
}
