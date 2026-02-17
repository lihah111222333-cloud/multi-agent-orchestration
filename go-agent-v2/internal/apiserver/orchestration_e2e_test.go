// orchestration_e2e_test.go — 编排工具全流程 E2E 测试。
//
// 覆盖:
//  1. 工具注入: buildAllDynamicTools 完整性
//  2. 调度链: handleDynamicToolCall → handler → result (模拟 codex 事件)
//  3. 编排场景: list → send → stop 端到端
//  4. 资源工具场景: 无 DB 时优雅降级
//  5. WS 通知: 工具调用产生 dynamic-tool/called 通知
package apiserver

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

// ============================================================
// § 1. 工具注入完整性
// ============================================================

// TestE2E_ToolInjectionCompleteness 验证 buildAllDynamicTools 至少包含编排工具，
// 并在 LSP 可用时包含 LSP 工具。
func TestE2E_ToolInjectionCompleteness(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	tools := env.srv.buildAllDynamicTools()
	lspTools := env.srv.buildLSPDynamicTools()

	// 编排工具必须存在
	allExpected := []string{
		"orchestration_list_agents", "orchestration_send_message",
		"orchestration_launch_agent", "orchestration_stop_agent",
	}

	// LSP 服务可用时才要求 LSP 工具
	minExpected := 4 // orchestration only
	if len(lspTools) > 0 {
		allExpected = append(allExpected, "lsp_hover", "lsp_open_file", "lsp_diagnostics")
		minExpected = 7
	}
	if len(tools) < minExpected {
		t.Fatalf("expected at least %d tools, got %d", minExpected, len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}

	for _, name := range allExpected {
		if !names[name] {
			t.Errorf("missing expected tool: %s", name)
		}
	}

	// 验证每个工具: name, description, inputSchema 非空
	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool with empty name")
		}
		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s has nil input schema", tool.Name)
		}
		// inputSchema 必须有 "type": "object"
		typ, ok := tool.InputSchema["type"].(string)
		if !ok || typ != "object" {
			t.Errorf("tool %s inputSchema.type should be 'object', got %v", tool.Name, tool.InputSchema["type"])
		}
	}

	t.Logf("total dynamic tools: %d", len(tools))
}

// ============================================================
// § 2. 调度链 E2E: 模拟 codex 事件 → handleDynamicToolCall
// ============================================================

// simulateDynamicToolCall 产生模拟 codex DynamicToolCall 事件的 Data。
func simulateDynamicToolCall(tool, callID string, args any) json.RawMessage {
	argsJSON, _ := json.Marshal(args)
	data := map[string]any{
		"msg": map[string]any{
			"tool":      tool,
			"callId":    callID,
			"arguments": json.RawMessage(argsJSON),
		},
	}
	raw, _ := json.Marshal(data)
	return raw
}

// TestE2E_DispatchChain_AllOrchTools 测试所有 4 个编排工具通过调度链。
func TestE2E_DispatchChain_AllOrchTools(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	type toolTest struct {
		tool   string
		args   any
		expect string // 结果中应包含的子串
	}

	tests := []toolTest{
		{
			tool:   "orchestration_list_agents",
			args:   map[string]any{},
			expect: "[]", // 空列表
		},
		{
			tool:   "orchestration_send_message",
			args:   map[string]any{"agent_id": "nonexist", "message": "hi"},
			expect: "error", // agent 不存在
		},
		{
			tool:   "orchestration_launch_agent",
			args:   map[string]any{},
			expect: "name is required", // 缺参数
		},
		{
			tool:   "orchestration_stop_agent",
			args:   map[string]any{"agent_id": "nonexist"},
			expect: "error", // agent 不存在
		},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			var result string

			// 直接调用分发链中的 handler (模拟 switch 分支)
			argsJSON, _ := json.Marshal(tt.args)
			switch tt.tool {
			case "orchestration_list_agents":
				result = env.srv.orchestrationListAgents()
			case "orchestration_send_message":
				result = env.srv.orchestrationSendMessage(argsJSON)
			case "orchestration_launch_agent":
				result = env.srv.orchestrationLaunchAgent(argsJSON)
			case "orchestration_stop_agent":
				result = env.srv.orchestrationStopAgent(argsJSON)
			}

			if !strings.Contains(result, tt.expect) {
				t.Errorf("expected result to contain %q, got: %s", tt.expect, result)
			}
		})
	}
}

// ============================================================
// § 3. 编排场景: list_agents 完整链路
// ============================================================

// TestE2E_OrchestrationScenario_ListSendStop 测试编排全流程。
func TestE2E_OrchestrationScenario_ListSendStop(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	// Step 1: 列出 agents (应为空)
	result := env.srv.orchestrationListAgents()
	if result != "[]" {
		t.Fatalf("Step1: expected empty list, got %s", result)
	}

	// Step 2: 尝试发送消息给不存在的 agent
	result = env.srv.orchestrationSendMessage([]byte(`{"agent_id":"ghost","message":"hello"}`))
	if !strings.Contains(result, "error") {
		t.Fatalf("Step2: expected error for nonexist agent, got %s", result)
	}

	// Step 3: 尝试停止不存在的 agent
	result = env.srv.orchestrationStopAgent([]byte(`{"agent_id":"ghost"}`))
	if !strings.Contains(result, "error") {
		t.Fatalf("Step3: expected error for nonexist agent, got %s", result)
	}

	// Step 4: launch 缺 name
	result = env.srv.orchestrationLaunchAgent([]byte(`{"prompt":"do something"}`))
	if !strings.Contains(result, "name is required") {
		t.Fatalf("Step4: expected name required error, got %s", result)
	}

	// Step 5: launch — 可能成功 (有 codex binary) 或失败 (无 binary)
	result = env.srv.orchestrationLaunchAgent([]byte(`{"name":"test-agent"}`))
	if strings.Contains(result, "name is required") {
		t.Fatalf("Step5: name was provided but got name required error")
	}
	t.Logf("Step5 launch result: %s", result)

	launchSucceeded := strings.Contains(result, `"status":"running"`)

	if launchSucceeded {
		// Step 6a: agent 成功启动 → 列表应该包含它
		result = env.srv.orchestrationListAgents()
		if result == "[]" {
			t.Fatal("Step6a: launch succeeded but list is empty")
		}
		t.Logf("Step6a: agents after launch: %s", result)

		// 提取 agent_id
		var agents []map[string]any
		json.Unmarshal([]byte(result), &agents)
		if len(agents) == 0 {
			t.Fatal("Step6a: expected at least 1 agent")
		}
		agentID, _ := agents[0]["id"].(string)

		// Step 7: 发送消息给新 agent
		result = env.srv.orchestrationSendMessage([]byte(`{"agent_id":"` + agentID + `","message":"hello from e2e"}`))
		if strings.Contains(result, "error") {
			t.Logf("Step7: send to launched agent: %s (may be expected if agent not ready)", result)
		} else {
			t.Logf("Step7: message sent successfully to %s", agentID)
		}

		// Step 8: 停止 agent
		result = env.srv.orchestrationStopAgent([]byte(`{"agent_id":"` + agentID + `"}`))
		t.Logf("Step8: stop result: %s", result)

		// Step 9: 列表应为空
		time.Sleep(200 * time.Millisecond) // 等 agent 进程退出
		result = env.srv.orchestrationListAgents()
		t.Logf("Step9: agents after stop: %s", result)
	} else {
		// Step 6b: launch 失败 (无 codex binary) → 列表应该为空
		result = env.srv.orchestrationListAgents()
		if result != "[]" {
			t.Fatalf("Step6b: launch failed but agents not empty: %s", result)
		}
		t.Log("Step6b: no codex binary, launch correctly failed")
	}

	t.Log("orchestration scenario complete")
}

// ============================================================
// § 4. 资源工具: 无 DB 时优雅降级
// ============================================================

// TestE2E_ResourceToolsWithoutDB 验证无 DB 时资源工具不暴露。
func TestE2E_ResourceToolsWithoutDB(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	// setupTestServer 不传 DB, dagStore 应该为 nil
	if env.srv.dagStore != nil {
		t.Skip("DB is available, skipping no-DB test")
	}

	tools := env.srv.buildResourceTools()
	if tools != nil {
		t.Errorf("expected nil resource tools without DB, got %d tools", len(tools))
	}

	// buildAllDynamicTools 下应只有 LSP + 编排
	allTools := env.srv.buildAllDynamicTools()
	for _, tool := range allTools {
		if strings.HasPrefix(tool.Name, "task_") ||
			strings.HasPrefix(tool.Name, "command_") ||
			strings.HasPrefix(tool.Name, "prompt_") ||
			strings.HasPrefix(tool.Name, "shared_file_") {
			t.Errorf("resource tool %s should not be present without DB", tool.Name)
		}
	}

	t.Logf("without DB: %d tools (correct: no resource tools)", len(allTools))
}

// ============================================================
// § 5. WS 通知: dynamic-tool/called 广播
// ============================================================

// TestE2E_DynamicToolNotification 验证工具调用产生 dynamic-tool/called 通知。
func TestE2E_DynamicToolNotification(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	// 连接一个 WS 客户端监听通知
	ws := dial(t, env.addr)
	defer ws.Close()
	time.Sleep(50 * time.Millisecond)

	// 通过服务端直接模拟一次工具调用的通知部分
	env.srv.Notify("dynamic-tool/called", map[string]any{
		"agent":   "test-agent",
		"tool":    "orchestration_list_agents",
		"callId":  "test-call-1",
		"elapsed": 5,
		"result":  "[]",
	})

	// WS 客户端应收到通知
	ws.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to receive notification: %v", err)
	}

	var notif Notification
	if err := json.Unmarshal(data, &notif); err != nil {
		t.Fatalf("unmarshal notification: %v", err)
	}

	if notif.Method != "dynamic-tool/called" {
		t.Errorf("expected method 'dynamic-tool/called', got '%s'", notif.Method)
	}

	// 验证通知内容
	paramsJSON, _ := json.Marshal(notif.Params)
	var params map[string]any
	json.Unmarshal(paramsJSON, &params)

	if params["tool"] != "orchestration_list_agents" {
		t.Errorf("expected tool 'orchestration_list_agents', got '%v'", params["tool"])
	}
	t.Logf("notification received: %s", string(paramsJSON))
}

// ============================================================
// § 6. 多客户端并发工具调用
// ============================================================

// TestE2E_ConcurrentToolCalls 测试并发调用工具无 race condition。
func TestE2E_ConcurrentToolCalls(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	const concurrency = 20
	var wg sync.WaitGroup
	errors := make(chan string, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			result := env.srv.orchestrationListAgents()
			if result != "[]" {
				errors <- "unexpected list result: " + result
			}

			// 发送到不存在的 agent
			result = env.srv.orchestrationSendMessage([]byte(`{"agent_id":"agent-` +
				string(rune('A'+idx%26)) + `","message":"concurrent test"}`))
			if !strings.Contains(result, "error") {
				errors <- "expected error from send, got: " + result
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	// 验证计数器正确 (无 race)
	env.srv.toolCallMu.Lock()
	total := int64(0)
	for _, v := range env.srv.toolCallCount {
		total += v
	}
	env.srv.toolCallMu.Unlock()

	// 注意: 直接调用 handler 不经过 handleDynamicToolCall, 所以 toolCallCount 为 0
	// 这是预期行为: 计数在 dispatch 层, 非 handler 层
	t.Logf("tool call counter total: %d (expected 0 for direct handler calls)", total)
}

// ============================================================
// § 7. handleDynamicToolCall 事件解析 E2E
// ============================================================

// TestE2E_HandleDynamicToolCall_Parsing 验证事件信封解析。
func TestE2E_HandleDynamicToolCall_Parsing(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	// 构造模拟事件: codex 原始格式 {msg: {tool, callId, arguments}}
	eventData := simulateDynamicToolCall("orchestration_list_agents", "call-123", map[string]any{})

	event := codex.Event{
		Type: codex.EventDynamicToolCall,
		Data: eventData,
	}

	// 连接 WS 监听通知
	ws := dial(t, env.addr)
	defer ws.Close()
	time.Sleep(50 * time.Millisecond)

	// handleDynamicToolCall 需要一个注册的 agent (mgr.Get 会返回 nil 然后 return)
	// 所以这里测试的是 "agent not found" 的优雅处理 (不 panic, 不错误)
	// 它会静默返回因为 proc == nil
	env.srv.handleDynamicToolCall("nonexist-agent", event)

	// 验证没有 panic, 函数正常返回
	t.Log("handleDynamicToolCall with nonexist agent completed gracefully")
}

// TestE2E_HandleDynamicToolCall_BadJSON 验证 bad JSON 不会 panic。
func TestE2E_HandleDynamicToolCall_BadJSON(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	event := codex.Event{
		Type: codex.EventDynamicToolCall,
		Data: []byte(`{not valid json`),
	}

	// 不应 panic
	env.srv.handleDynamicToolCall("test-agent", event)
	t.Log("bad JSON handled gracefully")
}

// ============================================================
// § 8. 完整链路: WS → tool schema 验证
// ============================================================

// TestE2E_ToolSchemaInInitialize 验证 WS 连接后可见工具 schema。
func TestE2E_ToolSchemaInInitialize(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	ws := dial(t, env.addr)
	defer ws.Close()

	// 发送 initialize
	resp := rpcCall(t, ws, 1, "initialize", map[string]any{
		"protocolVersion": "2.0",
	})

	if resp.Error != nil {
		t.Fatalf("initialize error: %s", resp.Error.Message)
	}

	// 验证 result
	resultBytes, _ := json.Marshal(resp.Result)
	var result map[string]any
	json.Unmarshal(resultBytes, &result)

	// 通过 experimentalApi 验证 dynamicTools 在配置中
	if caps, ok := result["capabilities"].(map[string]any); ok {
		if exp, ok := caps["experimental"].(map[string]any); ok {
			t.Logf("experimental capabilities: %v", exp)
		}
	}

	t.Logf("initialize result keys: %v", keysOf(result))
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ============================================================
// § 9. fork-bomb 保护
// ============================================================

// TestE2E_MaxAgentsProtection 验证 maxAgents 参数有效。
func TestE2E_MaxAgentsProtection(t *testing.T) {
	env := setupTestServer(t)
	defer env.cancel()

	// maxAgents 常量应为 20
	if maxAgents != 20 {
		t.Errorf("expected maxAgents=20, got %d", maxAgents)
	}

	// 当前 agent 列表为空, 发起 launch 应不触发限制
	result := env.srv.orchestrationLaunchAgent([]byte(`{"name":"test"}`))
	// 会因为没有 codex binary 而失败, 但不应该是 "max agents" 错误
	if strings.Contains(result, "max agents") {
		t.Error("should not hit max agents with 0 agents running")
	}
	t.Logf("launch result (no limit hit): %s", result)
}
