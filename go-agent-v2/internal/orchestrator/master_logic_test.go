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

// ========================================
// Fix 2: bare type assertion 安全检查
// ========================================

func TestSanitizeTopology_MissingKeys_NoPanic(t *testing.T) {
	// sanitizeGateway 当前实现保证返回 ok=true 时 agents_raw 和 id 存在。
	// 但这些 bare type assertions 属于脆弱编码: 如果 sanitizeGateway 行为变更,
	// 第 148-149 行的直接断言会 panic。此测试验证 sanitizeTopology 在
	// sanitizeGateway 返回 false (因 agents 缺失) 时不会 panic。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("sanitizeTopology panicked: %v", r)
		}
	}()

	// 构造一个 gateway 列表，其中 gateway 缺少 agents 字段 →
	// sanitizeGateway 返回 false → 外层 continue, 不应 panic。
	raw := map[string]any{
		"gateways": []any{
			map[string]any{
				"id":   "gw-1",
				"name": "Gateway",
				// 缺少 "agents" → sanitizeGateway 返回 false
			},
		},
	}

	result := sanitizeTopology(raw)
	if result != nil {
		t.Fatal("expected nil result when no agents present")
	}
}
