package apiserver

import (
	"testing"
)

// TestOrchestrationToolDefinitions 验证 4 个编排工具定义的完整性。
func TestOrchestrationToolDefinitions(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	tools := env.srv.buildOrchestrationTools()
	if len(tools) != 4 {
		t.Fatalf("expected 4 orchestration tools, got %d", len(tools))
	}

	expected := map[string]bool{
		"orchestration_list_agents":  false,
		"orchestration_send_message": false,
		"orchestration_launch_agent": false,
		"orchestration_stop_agent":   false,
	}

	for _, tool := range tools {
		if _, ok := expected[tool.Name]; !ok {
			t.Errorf("unexpected tool: %s", tool.Name)
			continue
		}
		expected[tool.Name] = true

		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s has nil input schema", tool.Name)
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}
}

// TestResourceToolDefinitions 验证 9 个资源工具定义 (需要 DB)。
func TestResourceToolDefinitions(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	// 无 DB 时 buildResourceTools 返回 nil
	tools := env.srv.buildResourceTools()
	if tools != nil {
		// 有 DB 时检查 9 个工具
		if len(tools) != 9 {
			t.Fatalf("expected 9 resource tools, got %d", len(tools))
		}
		expected := []string{
			"task_create_dag", "task_get_dag", "task_update_node",
			"command_list", "command_get",
			"prompt_list", "prompt_get",
			"shared_file_read", "shared_file_write",
		}
		names := map[string]bool{}
		for _, tool := range tools {
			names[tool.Name] = true
		}
		for _, name := range expected {
			if !names[name] {
				t.Errorf("missing resource tool: %s", name)
			}
		}
	}
}

// TestBuildAllDynamicTools 验证 buildAllDynamicTools 聚合。
func TestBuildAllDynamicTools(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	tools := env.srv.buildAllDynamicTools()
	// 至少有编排工具 (LSP 和资源可能因无 lsp/DB 而为空)
	if len(tools) < 4 {
		t.Errorf("expected at least 4 tools (orchestration), got %d", len(tools))
	}
}

// TestOrchestrationListAgentsEmpty 无 agent 时返回空数组。
func TestOrchestrationListAgentsEmpty(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	result := env.srv.orchestrationListAgents()
	if result != "[]" {
		t.Errorf("expected empty array, got %s", result)
	}
}

// TestOrchestrationSendMessageInvalid 发送给不存在的 agent 返回错误。
func TestOrchestrationSendMessageInvalid(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	result := env.srv.orchestrationSendMessage([]byte(`{"agent_id":"nonexistent","message":"hello"}`))
	if result == "" || result[0] != '{' {
		t.Fatal("expected JSON error response")
	}
	if !contains(result, "error") {
		t.Errorf("expected error in response, got: %s", result)
	}
}

// TestOrchestrationStopAgentInvalid 停止不存在的 agent 返回错误。
func TestOrchestrationStopAgentInvalid(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	result := env.srv.orchestrationStopAgent([]byte(`{"agent_id":"nonexistent"}`))
	if !contains(result, "error") {
		t.Errorf("expected error, got: %s", result)
	}
}

// TestOrchestrationLaunchMissingName 缺少 name 返回错误。
func TestOrchestrationLaunchMissingName(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	result := env.srv.orchestrationLaunchAgent([]byte(`{}`))
	if !contains(result, "name is required") {
		t.Errorf("expected 'name is required' error, got: %s", result)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
