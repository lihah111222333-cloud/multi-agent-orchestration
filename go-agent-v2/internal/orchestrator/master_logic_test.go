package orchestrator

import "testing"

func TestSanitizeGateway(t *testing.T) {
	seen := map[string]bool{}
	raw := map[string]any{
		"id":          "gw-1",
		"name":        "Gateway",
		"description": "desc",
		"agents":      []any{map[string]any{"id": "a-1", "name": "agent"}},
	}
	gateway, ok := sanitizeGateway(raw, 0, seen)
	if !ok {
		t.Fatal("expected gateway to be accepted")
	}
	if gateway["id"] != "gw-1" {
		t.Fatalf("id = %v, want gw-1", gateway["id"])
	}
}

func TestSanitizeAgent(t *testing.T) {
	seen := map[string]bool{}
	raw := map[string]any{
		"id":           "agent-1",
		"name":         "Agent 1",
		"capabilities": []any{"x"},
		"depends_on":   []any{"y"},
	}
	agent, ok := sanitizeAgent(raw, "gw-1", 0, seen)
	if !ok {
		t.Fatal("expected agent to be accepted")
	}
	if agent["id"] != "agent-1" {
		t.Fatalf("id = %v, want agent-1", agent["id"])
	}
}

func TestScoreLengthDim(t *testing.T) {
	if got := scoreLengthDim(""); got != 0 {
		t.Fatalf("scoreLengthDim(empty) = %d, want 0", got)
	}
	if got := scoreLengthDim("12345678901234567890"); got != 1 {
		t.Fatalf("scoreLengthDim(20 chars) = %d, want 1", got)
	}
}

func TestPenalizeErrorKeywords(t *testing.T) {
	if got := penalizeErrorKeywords("all good"); got != 0 {
		t.Fatalf("penalty = %d, want 0", got)
	}
	if got := penalizeErrorKeywords("error: failed"); got != -20 {
		t.Fatalf("penalty = %d, want -20", got)
	}
}

func TestScoreLineDim(t *testing.T) {
	score, lines := scoreLineDim("a\n\nb")
	if score != 4 {
		t.Fatalf("score = %d, want 4", score)
	}
	if len(lines) != 2 {
		t.Fatalf("lines len = %d, want 2", len(lines))
	}
}
