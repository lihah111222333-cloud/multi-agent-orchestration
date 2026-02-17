# 编排工具实现计划 — 批判审查

## 发现的 7 个问题

### 🔴 P0: launch_agent 阻塞事件循环

**问题**: `handleDynamicToolCall` 已经在 `go` goroutine 中运行 (server.go:493)，但 `mgr.Launch()` 内部 `SpawnAndConnect()` 是阻塞调用 (spawn 进程 + WS 握手 + initialize + thread/start)，可能耗时 5-10 秒。这不会死锁（因为已经在独立 goroutine），但 codex 对 tool result 有超时，可能超时失败。

**修复**: `orchestrationLaunchAgent` 使用带 **30s timeout 的 context**，而非 `context.Background()`。

---

### 🔴 P0: 新 Agent 缺少编排工具

**问题**: 计划中 `orchestrationLaunchAgent` 调用 `s.mgr.Launch(ctx, id, name, prompt, cwd, dynamicTools)` — 但 `dynamicTools` 从哪来？如果传 `nil`，新 agent 只有对话能力，没有编排能力。

**修复**: `orchestrationLaunchAgent` 内部调用 `s.buildLSPDynamicTools()` + `s.buildOrchestrationTools()` 构建完整工具列表传入 Launch。

---

### 🟡 P1: Fork-bomb 风险

**问题**: Agent 可以无限调用 `orchestration_launch_agent`，没有数量限制。恶意/失控 agent 可能创建 100+ 子进程耗尽系统资源。

**修复**: 在 `orchestrationLaunchAgent` 中检查 `len(s.mgr.List())` ≥ `maxAgents`（建议 20），超限返回错误。

---

### 🟡 P1: 日志标签错误

**问题**: `handleDynamicToolCall` 日志写死了 `"lsp: tool called"` / `"lsp: tool completed"`，编排工具也会显示为 lsp 前缀，误导排查。

**修复**: 日志前缀改为 `"dynamic-tool:"` 或按 tool name prefix 动态判断 (`lsp_*` → `"lsp:"`, `orchestration_*` → `"orch:"`)。

---

### 🟡 P1: 前端通知 channel 命名

**问题**: `s.Notify("lsp/tool/called", ...)` 硬编码为 `lsp/tool/called`。编排工具也走这个通知，前端按 lsp 过滤会收到编排事件、按 orchestration 过滤却收不到。

**修复**: 通知 channel 改为通用的 `"dynamic-tool/called"` 或按工具前缀分路。

---

### 🟢 P2: 测试覆盖不足

**问题**: 计划只有 `TestOrchestrationToolDefinitions` 测试 schema，没有测试实际执行 (list/send/launch/stop)。

**修复**: 新增 `TestOrchestrationHandlers` — mock agent manager, 测试各 handler 返回值。

---

### 🟢 P2: lspCallCount / lspCallMu 命名

**问题**: 这两个字段名绑定了 "lsp" 语义，现在混用于编排工具计数。

**修复**: 重命名为 `toolCallCount` / `toolCallMu`（涉及 server.go struct 定义 + 3 处引用）。

---

## 修订后的实现计划

见更新后的 [orchestration-tools.md](file:///Users/mima0000/Desktop/wj/multi-agent-orchestration/docs/plans/2026-02-17-orchestration-tools.md)

| 原计划 | 修订 |
|--------|------|
| `context.Background()` | `context.WithTimeout(30s)` |
| 新 agent 不传 tools | 构建完整 dynamicTools 传入 |
| 无 agent 数量限制 | `maxAgents = 20` 检查 |
| lsp 前缀日志 | `dynamic-tool:` 通用前缀 |
| `lsp/tool/called` 通知 | `dynamic-tool/called` |
| `lspCallCount` 字段名 | `toolCallCount` |
| 只测 schema | 增加 handler 执行测试 |
