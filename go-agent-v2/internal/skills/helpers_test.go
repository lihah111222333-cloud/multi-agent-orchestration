package skills

import (
	"path/filepath"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/skillutil"
)

func TestNormalizeSkillName(t *testing.T) {
	t.Parallel()

	name, err := skillutil.NormalizeName("  DemoSkill  ")
	if err != nil {
		t.Fatalf("normalizeSkillName returned error: %v", err)
	}
	if name != "DemoSkill" {
		t.Fatalf("normalizeSkillName mismatch: got %q", name)
	}

	if _, err := skillutil.NormalizeName(""); err == nil {
		t.Fatal("skillutil.NormalizeName(\"\") expected error")
	}
	if _, err := skillutil.NormalizeName("   "); err == nil {
		t.Fatal("skillutil.NormalizeName(whitespace) expected error")
	}
}

func TestCollectSkillImportSources(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	first := filepath.Clean(filepath.Join(tmp, "skill-a"))
	second := filepath.Clean(filepath.Join(tmp, "skill-b"))

	got := collectSkillImportSources("  "+first+"  ", []string{first, "", "   ", second})
	if len(got) != 2 {
		t.Fatalf("collectSkillImportSources len mismatch: got %d, want 2 (%v)", len(got), got)
	}
	if filepath.Clean(got[0]) != first {
		t.Fatalf("collectSkillImportSources first mismatch: got %q want %q", got[0], first)
	}
	if filepath.Clean(got[1]) != second {
		t.Fatalf("collectSkillImportSources second mismatch: got %q want %q", got[1], second)
	}
}

func TestResolveSkillMatchPreviewThreadID(t *testing.T) {
	t.Parallel()

	got := resolveSkillMatchPreviewThreadID(SkillsMatchPreviewParams{ThreadID: " thread-1 ", AgentID: "agent-1"})
	if got != "thread-1" {
		t.Fatalf("threadID priority mismatch: got %q", got)
	}

	got = resolveSkillMatchPreviewThreadID(SkillsMatchPreviewParams{ThreadID: "   ", AgentID: " agent-2 "})
	if got != "agent-2" {
		t.Fatalf("agentID fallback mismatch: got %q", got)
	}
}
