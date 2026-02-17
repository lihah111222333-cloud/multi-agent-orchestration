# 编排工具 (Orchestration Tools) 实现计划 v2

> **给 Claude:** 必须使用 @执行计划 逐任务实现此计划。

**目标:** 通过 Dynamic Tool 注入让 AI Agent 原生调用编排能力 (list/send/launch/stop)

**架构:** 复用 LSP 工具注入路径 — `buildOrchestrationTools()` 定义 + `handleDynamicToolCall()` 分发 + `AgentManager` 执行

**技术栈:** Go, codex DynamicTool API, JSON-RPC

**审查修订:** 解决了 7 个问题 (fork-bomb, blocking ctx, 缺工具注入, 日志/通知命名, 测试覆盖, 字段重命名)

---

### 任务 0: 重命名 lsp 专用字段为通用名 (server.go)

**文件:**
- 修改: `go-agent-v2/internal/apiserver/server.go`

**步骤 1: 重命名 struct 字段**

```diff
-lspCallMu    sync.Mutex
-lspCallCount map[string]int
+toolCallMu    sync.Mutex
+toolCallCount map[string]int
```

**步骤 2: 更新所有引用 (3 处)**

`handleDynamicToolCall` 中:
```diff
-s.lspCallMu.Lock()
-s.lspCallCount[call.Tool]++
-count := s.lspCallCount[call.Tool]
-s.lspCallMu.Unlock()
+s.toolCallMu.Lock()
+s.toolCallCount[call.Tool]++
+count := s.toolCallCount[call.Tool]
+s.toolCallMu.Unlock()
```

**步骤 3: 日志 + 通知通用化**

```diff
-slog.Info("lsp: tool called", ...)
-slog.Info("lsp: tool completed", ...)
-s.Notify("lsp/tool/called", ...)
+slog.Info("dynamic-tool: called", ...)
+slog.Info("dynamic-tool: completed", ...)
+s.Notify("dynamic-tool/called", ...)
```

**步骤 4: New() 初始化**

```diff
-lspCallCount: make(map[string]int),
+toolCallCount: make(map[string]int),
```

运行: `go build ./...`
预期: PASS

---

### 任务 1: 编排工具定义与处理 (orchestration_tools.go)

**文件:**
- 创建: `go-agent-v2/internal/apiserver/orchestration_tools.go`
- 测试: `go-agent-v2/internal/apiserver/orchestration_tools_test.go`

**步骤 1: 写测试**

```go
func TestOrchestrationToolDefinitions(t *testing.T) {
    env := setupTestServer(t)
    defer env.cancel()
    tools := env.srv.buildOrchestrationTools()
    if len(tools) != 4 { t.Fatalf("expected 4, got %d", len(tools)) }
    expected := []string{
        "orchestration_list_agents",
        "orchestration_send_message",
        "orchestration_launch_agent",
        "orchestration_stop_agent",
    }
    names := map[string]bool{}
    for _, tool := range tools { names[tool.Name] = true }
    for _, name := range expected {
        if !names[name] { t.Errorf("missing: %s", name) }
    }
}

func TestOrchestrationListAgents(t *testing.T) {
    env := setupTestServer(t)
    defer env.cancel()
    result := env.srv.orchestrationListAgents()
    // 无 agent 时应返回空数组
    if result != "[]" { t.Errorf("expected empty array, got %s", result) }
}
```

**步骤 2: 实现**

`orchestration_tools.go` 包含:

```go
const maxAgents = 20 // fork-bomb 保护

func (s *Server) buildOrchestrationTools() []codex.DynamicTool { ... }

func (s *Server) orchestrationListAgents() string {
    infos := s.mgr.List()
    data, _ := json.Marshal(infos)
    return string(data)
}

func (s *Server) orchestrationSendMessage(args json.RawMessage) string {
    var p struct {
        AgentID string `json:"agent_id"`
        Message string `json:"message"`
    }
    // parse → s.mgr.Submit(p.AgentID, p.Message, nil, nil)
}

func (s *Server) orchestrationLaunchAgent(args json.RawMessage) string {
    // 检查 maxAgents
    if len(s.mgr.List()) >= maxAgents {
        return `{"error":"max agents reached"}`
    }
    // 30s timeout context
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    // 构建完整工具列表 (LSP + 编排)
    tools := s.buildLSPDynamicTools()
    tools = append(tools, s.buildOrchestrationTools()...)
    // s.mgr.Launch(ctx, id, name, prompt, cwd, tools)
}

func (s *Server) orchestrationStopAgent(args json.RawMessage) string {
    // parse → s.mgr.Stop(p.AgentID)
}
```

运行: `go test -run "TestOrchestration" -v ./internal/apiserver/`
预期: PASS

---

### 任务 2: 注入编排工具到 threadStart

**文件:**
- 修改: `go-agent-v2/internal/apiserver/methods.go:154`

```diff
 dynamicTools := s.buildLSPDynamicTools()
+dynamicTools = append(dynamicTools, s.buildOrchestrationTools()...)
```

运行: `go test -run "TestAllMethodsWired" -v ./internal/apiserver/`
预期: PASS

---

### 任务 3: 扩展 handleDynamicToolCall (server.go switch)

**文件:**
- 修改: `go-agent-v2/internal/apiserver/server.go:750-758`

```diff
 switch call.Tool {
 case "lsp_hover":
     result = s.lspHover(call.Arguments)
 case "lsp_open_file":
     result = s.lspOpenFile(call.Arguments)
 case "lsp_diagnostics":
     result = s.lspDiagnostics(call.Arguments)
+case "orchestration_list_agents":
+    result = s.orchestrationListAgents()
+case "orchestration_send_message":
+    result = s.orchestrationSendMessage(call.Arguments)
+case "orchestration_launch_agent":
+    result = s.orchestrationLaunchAgent(call.Arguments)
+case "orchestration_stop_agent":
+    result = s.orchestrationStopAgent(call.Arguments)
 default:
     result = fmt.Sprintf("unknown tool: %s", call.Tool)
 }
```

---

### 任务 4: 全量验证

运行: `go build ./... && go vet ./...`
预期: PASS

运行: `go test -v -count=1 -timeout 120s ./internal/apiserver/ ./internal/runner/... ./internal/codex/...`
预期: ALL PASS
