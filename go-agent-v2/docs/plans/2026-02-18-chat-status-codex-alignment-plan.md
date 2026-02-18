# Chat 状态显示对齐 Codex 客户端修复方案

## 1. 背景与目标

当前 Chat 页“执行中”状态存在残留风险：时间线已无进行中项，但顶部/底部状态仍可能显示“执行中”。目标是将状态体验对齐 Codex 客户端：**基于生命周期派生状态**，并支持“工作中/等待指示/等待后台终端/MCP 启动中/推理标题覆盖”等一致表现。

## 2. 现状问题（Root Cause）

### 2.1 前端显示层

- `cmd/agent-terminal/frontend/vue-app/components/ChatTimeline.js`
  - `presenceLabel()` 先读 `activeStatus`，再回退到 timeline pending。
  - 当 `activeStatus` 滞后为 `running` 时，会持续显示“执行中”。
- `cmd/agent-terminal/frontend/vue-app/stores/threads.js`
  - `getThreadStatus()` 直接返回快照里的 `statuses[threadId]`，未做“当前生命周期”二次派生。

### 2.2 后端状态层

- `internal/uistate/event_normalizer.go`
  - `exec_command_end` / `patch_apply_end` / `mcp_tool_call_end` 当前仍映射到 `UIStatusRunning`。
- `internal/uistate/event_normalizer.go`
  - 未覆盖 `item/commandExecution/terminalInteraction` 与 `mcp_startup_update`，导致“等待后台终端/MCP 启动中”难以稳定派生。
- `internal/uistate/runtime_state.go`
  - `applyAgentEventLocked()` 里按事件直接 `setThreadStateLocked()`，属于“事件覆盖状态”，不是“状态机派生”。
- `internal/uistate/runtime_state.go`
  - `ReplaceThreads()` 会把线程列表的 `state` 回写到 `snapshot.Statuses`，可能把派生状态重新覆盖。

## 3. 对齐 Codex 的原则

对齐目标来自你提供的文档与源码逻辑（`/Users/mima0000/Desktop/wj/codex/docs/tui-status-flow-zh.md`）：

- 运行态是 **turn/mcp/orchestration 多生命周期合并**，不是单事件标签。
- `TurnStarted` 进入工作态，`TurnComplete` 统一收敛。
- reasoning 首个 `**...**` 可覆盖状态标题。
- `TerminalInteraction`（空 stdin）、`BackgroundEvent`、`StreamError` 有高优先级覆盖能力。
- `ReasoningContentDelta` 可由 legacy 事件链消费，不必重复驱动显示层状态机。

## 4. 方案总览

### 4.1 后端：引入派生状态机（核心）

在 `internal/uistate/runtime_state.go` 为每个 thread 维护 lifecycle **深度计数器**（建议扩展 `threadRuntime`）：

- `turnDepth int`
- `approvalDepth int`
- `commandDepth int`
- `fileEditDepth int`
- `toolCallDepth int`
- `collabDepth int`
- `mcpStartupDepth int`
- `terminalWaitDepth int`
- `streamErrorText string`（可选）
- `statusHeader string` / `statusDetails string`（可选）

计数更新规则：

- begin/start 事件 `+1`，end/complete 事件 `-1`，并 `max(0, depth)` 防止并发乱序击穿。
- `turn_complete/task_complete/idle` 统一收敛：将 `turnDepth`、`commandDepth`、`fileEditDepth`、`toolCallDepth`、`approvalDepth` 清零（`mcpStartupDepth` 可按实际事件单独管理）。
- `approvalDepth` 清理条件明确为：
  - `exec_approval_request` / `file_change_approval_request`：`+1`
  - `exec_command_begin` / `patch_apply_begin`：若 `approvalDepth > 0` 则清零（表示审批已被处理并进入执行）
  - `turn_complete/task_complete/idle`：强制清零

新增统一计算函数：

- `deriveThreadStatusLocked(threadID string) string`
- `deriveThreadStatusHeaderLocked(threadID string) string`（可选）

并在每次事件处理后统一调用，而非直接信任某个事件的 `UIStatus`。

`ReplaceThreads()` 约束（必须）：

- 线程列表状态仅用于“线程存在性与初始状态种子”。
- 若 thread 已有 lifecycle 计数器状态（或已处理过事件），`ReplaceThreads()` 不得反向覆盖 `snapshot.Statuses`。
- 仅在“新线程首次出现”或“完全无派生状态上下文”时，才允许用列表 `state` 初始化。

### 4.2 事件归类修正

修改 `internal/uistate/event_normalizer.go`：

- 结束类事件不再硬写 `UIStatusRunning`：
  - `exec_command_end`
  - `patch_apply_end`
  - `mcp_tool_call_end`
  - `collab_*_end`
- 补齐 Codex 对齐事件：
  - `item/commandExecution/terminalInteraction`（含 `exec_terminal_interaction`）→ 终端等待/交互覆盖源
  - `mcp_startup_update`（含 `codex/event/mcp_startup_update`）→ MCP 启动覆盖源
- 保留 `turn_complete/task_complete/idle -> UIStatusIdle`。
- 保留 `stream_error -> UIStatusError` 作为覆盖触发信号。

实现建议：

- classifier 增加“按 `codexType` + `method` 双通道匹配”，避免仅靠原始 type 漏事件。
- 对 terminal/mcp startup 这类“标题覆盖事件”可归为 `UITypeSystem`，由 runtime 层设置 header/overlay，而非硬编码 thread status。

### 4.3 前端：显示优先级调整

`cmd/agent-terminal/frontend/vue-app/components/ChatTimeline.js`：

- `showAgentPresence()` 优先看 timeline 是否有 pending。
- `presenceLabel()` 优先返回 timeline 最新 pending 标签；仅在无 pending 时才使用 `activeStatus`（fallback）。
- fallback 约束：必须保留 `error` / `starting` / `waiting` 的兜底展示；`running` 不可在“无 pending 且无后端 header”时单独常驻。

`cmd/agent-terminal/frontend/vue-app/pages/UnifiedChatPage.js`（可选增强）：

- 若后端提供 `statusHeader`，优先展示 header；
- 否则回退 `statusLabel(activeStatus)`。

## 5. 状态优先级定义（建议）

同一时刻只显示一个主状态，优先级从高到低：

1. `stream_error`（错误覆盖）
2. 终端等待（`等待后台终端 · 命令`）
3. `background_event` / MCP 启动提示
4. 审批等待（`waiting`）
5. 命令执行 / 文件编辑 / 工具调用（`running` / `editing`）
6. turn 推理中（`thinking`/`responding`，可被 reasoning header 覆盖）
7. `idle`（等待指示）

## 6. 实施步骤

### Phase A（必做，修复残留 bug）

1. 修正 normalizer 结束事件状态映射。
2. 在 runtime manager 中改为 lifecycle 派生状态。
3. 调整 ChatTimeline 的显示优先级（pending first）。

### Phase B（体验对齐增强）

4. 后端增加 `statusHeader/statusDetails` 输出（`ui/state/get`）。
5. 前端状态行优先展示 header。
6. 添加 reasoning header/terminal waiting/background/stream error 的覆盖与恢复机制。

## 7. 测试方案

### 7.1 Go 单测

- `internal/uistate/event_normalizer_test.go`
  - 断言结束事件不再返回 `UIStatusRunning`。
  - 新增 `item/commandExecution/terminalInteraction` 与 `mcp_startup_update` 归类断言。
- `internal/uistate/runtime_state_test.go`
  - `exec_command_begin -> exec_command_end`（无 `turn_complete`）不会残留 running。
  - `patch_apply_begin -> patch_apply_end` 不残留 editing/running。
  - `stream_error` 覆盖后可在后续正常事件恢复。
  - `approvalDepth` 在 `exec_command_begin/patch_apply_begin/turn_complete` 的清理行为符合预期。
  - `ReplaceThreads()` 不回写覆盖已有派生状态（尤其 running 残留场景）。

### 7.2 前端测试

- 新增/扩展 `cmd/agent-terminal/frontend/vue-app/stores/__tests__/...`
  - 构造 `activeStatus=running` 但 timeline 无 pending，断言不显示“执行中”。
  - 构造有 pending command/file/approval，断言显示对应优先标签。

## 8. 验收标准

- 不再出现“时间线已结束但状态仍执行中”。
- turn 开始/结束行为稳定：开始进入工作态，结束回空闲态。
- 审批、终端等待、MCP 启动、错误覆盖可正确显示且可恢复。
- 与 Codex 的状态感知路径一致：**生命周期驱动 + 层级覆盖**。

## 9. 风险与回滚

- 风险：历史事件流不完整时可能导致派生状态偏保守（更快回 idle）。
- 缓解：保留 `turn_complete` 兜底收敛，并通过 timeline pending 再次校验。
- 回滚：可先仅上线 Phase A；Phase B 分支开关控制（header 功能可灰度）。
