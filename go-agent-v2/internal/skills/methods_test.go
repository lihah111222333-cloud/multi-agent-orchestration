package skills

import (
	"context"
	"os"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/service"
)

func TestSkillsConfigReadValidation(t *testing.T) {
	t.Parallel()

	m := NewManager(nil, nil)

	if _, err := m.SkillsConfigRead(context.Background(), SkillsConfigReadParams{}); err == nil {
		t.Fatal("SkillsConfigRead expected error when agent_id is empty")
	}

	out, err := m.SkillsConfigRead(context.Background(), SkillsConfigReadParams{AgentID: " agent-1 "})
	if err != nil {
		t.Fatalf("SkillsConfigRead returned error: %v", err)
	}
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("SkillsConfigRead result type mismatch: %T", out)
	}
	if got, _ := result["agent_id"].(string); got != "agent-1" {
		t.Fatalf("agent_id mismatch: got %q", got)
	}
	if got, _ := result["session_bound"].(bool); got {
		t.Fatal("session_bound mismatch: got true want false")
	}
}

func TestSkillsConfigWriteValidationAndUnavailable(t *testing.T) {
	t.Parallel()

	t.Run("service unavailable", func(t *testing.T) {
		m := NewManager(nil, nil)
		if _, err := m.SkillsConfigWrite(context.Background(), SkillsConfigWriteParams{Name: "demo", Content: "x"}); err == nil {
			t.Fatal("SkillsConfigWrite expected unavailable error")
		}
	})

	t.Run("name required when service exists", func(t *testing.T) {
		tmp := t.TempDir()
		svc := service.NewSkillService(tmp)
		m := NewManager(SkillServiceProviderFunc(func() *service.SkillService { return svc }), nil)
		if _, err := m.SkillsConfigWrite(context.Background(), SkillsConfigWriteParams{Name: "   ", Content: "x"}); err == nil {
			t.Fatal("SkillsConfigWrite expected name required error")
		}
	})

	t.Run("write success", func(t *testing.T) {
		tmp := t.TempDir()
		svc := service.NewSkillService(tmp)
		m := NewManager(SkillServiceProviderFunc(func() *service.SkillService { return svc }), nil)

		out, err := m.SkillsConfigWrite(context.Background(), SkillsConfigWriteParams{Name: "demo", Content: "# demo"})
		if err != nil {
			t.Fatalf("SkillsConfigWrite returned error: %v", err)
		}
		result, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("SkillsConfigWrite result type mismatch: %T", out)
		}
		if got, _ := result["ok"].(bool); !got {
			t.Fatal("SkillsConfigWrite ok mismatch: got false")
		}
		path, _ := result["path"].(string)
		if path == "" {
			t.Fatal("SkillsConfigWrite path is empty")
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("SkillsConfigWrite path stat failed: %v", statErr)
		}
	})
}

func TestSkillsMatchPreviewUsesCollectorContract(t *testing.T) {
	t.Parallel()

	var capturedThreadID string
	var capturedPrompt string
	var capturedOptions AutoSkillMatchOptions

	collector := AutoMatchCollectorFunc(func(
		threadID string,
		prompt string,
		input []UserInput,
		options AutoSkillMatchOptions,
	) []AutoMatchedSkillMatch {
		capturedThreadID = threadID
		capturedPrompt = prompt
		capturedOptions = options
		return []AutoMatchedSkillMatch{
			{Name: "   ", MatchedBy: "force"},
			{Name: "skill-a", MatchedBy: "explicit", MatchedTerms: []string{"@skill-a"}},
		}
	})

	m := NewManager(nil, collector)
	out, err := m.SkillsMatchPreview(context.Background(), SkillsMatchPreviewParams{
		AgentID: " agent-42 ",
		Text:    "hello world",
	})
	if err != nil {
		t.Fatalf("SkillsMatchPreview returned error: %v", err)
	}
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("SkillsMatchPreview result type mismatch: %T", out)
	}
	if got, _ := result["thread_id"].(string); got != "agent-42" {
		t.Fatalf("thread_id mismatch: got %q", got)
	}
	if capturedThreadID != "agent-42" {
		t.Fatalf("collector threadID mismatch: got %q", capturedThreadID)
	}
	if capturedPrompt != "hello world" {
		t.Fatalf("collector prompt mismatch: got %q", capturedPrompt)
	}
	if !capturedOptions.IncludeConfiguredExplicit || !capturedOptions.IncludeConfiguredForce {
		t.Fatalf("collector options mismatch: %+v", capturedOptions)
	}

	matches, ok := result["matches"].([]skillsMatchPreviewItem)
	if !ok {
		t.Fatalf("matches type mismatch: %T", result["matches"])
	}
	if len(matches) != 1 {
		t.Fatalf("matches len mismatch: got %d", len(matches))
	}
	if matches[0].Name != "skill-a" {
		t.Fatalf("matches[0].Name mismatch: got %q", matches[0].Name)
	}
	if matches[0].MatchedBy != "explicit" {
		t.Fatalf("matches[0].MatchedBy mismatch: got %q", matches[0].MatchedBy)
	}
}
