---
description: CLI 抽象层实施 — 解耦 Codex 硬绑定，支持多 CLI 适配
---

# CLI 抽象层实施工作流

// turbo-all

## 核心需求

核心目标只有一个：**解耦 CLI**。  
`runner`、`apiserver` 不应再硬依赖 `internal/codex` 的通用类型/接口。`codex` 应退化为一个具体实现（adapter）。
`bus` 在本阶段保持 Codex 专属消息通道，不纳入通用抽象迁移范围。
`internal/bus` 对 `codex.Event` / `codex.Event*` 的依赖属于本阶段白名单；Step 4 / Step 5 校验应显式排除“要求 bus 去 codex 化”。
抽象层必须保持**架构纯洁性**，且包名设计不能引发大规模的变量名冲突（Shadowing）。本阶段先完成“类型依赖解耦”，协议字段去耦（如 `JSON-RPC Code` 语义抽象）放到后续专项。

## 验收标准

满足以下 6 条即视为完成：

1. 新增 `internal/agentcore` 抽象层，承载 CLI 无关的事件/工具/客户端接口。
   - **命名原因**: 弃用 `internal/agent` 作为包名。`agent` 是项目核心领域词汇（`agentID`、`agentProcess`、`agent_message_delta` 等），在 `runner` / `apiserver` 中作为标识符子串高频出现。若包名使用 `agent`，极易与未来新增的局部变量或字段产生 **Name Shadowing**，且在代码审阅时难以区分「包引用」与「领域术语」。使用无冲突词汇 `agentcore` 可永久规避此风险。
2. `runner` 仅通过 `agentcore.Client` 工作，并在本阶段保持与现有 `CodexClient` 方法签名兼容（含 `GetPort` / `SpawnAndConnect`），确保迁移可分阶段落地。
3. `apiserver` 的通用事件与动态工具链路统一切换为 `agentcore` 类型；`bus` 保持 Codex 专属实现。
4. `codex` 仅保留 Codex 专属协议（如 TransportSSE、`HealthResponse`、CamelCase 工具结构 等）。
5. `go test ./... -count=1` 全通过，行为不回归。
6. 迁移完成后执行废弃代码清理，移除 `runner` / `apiserver` 不再需要的兼容 alias/临时桥接；`bus` 白名单依赖保留并标注，再次通过全量验证。

## 执行前提（P1-P5 共用）

为避免“流程正确但环境错误”导致误判，所有阶段开始前先确认：

1. 处于仓库根目录，且是可写工作区（建议使用独立分支/工作树）。
2. 已安装并可用：`bash`、`go`、`rg`、`gofmt`。
3. Go 版本与项目 `go.mod` 主版本兼容。
4. 若全量测试依赖外部服务（如数据库），需提前准备环境变量与服务实例。
5. 校验脚本存在：`.agent/workflows/cli-abstraction-handoffs/verify-helpers.sh`（Step 3/4/5 会 `source`）。

建议预检命令：

```bash
pwd
git rev-parse --is-inside-work-tree
bash --version | head -n 1
go version
rg --version
gofmt -h >/dev/null
test -f .agent/workflows/cli-abstraction-handoffs/verify-helpers.sh
```

预检通过标准：

1. 上述命令全部成功。
2. 若任一命令失败，先修复环境再进入 Step 0。

## 边界定义（先定边界再迁移）

### 放入 `internal/agentcore`（CLI 无关抽象层）

- `Event` (自 `events.go`)、`EventHandler` (自 `client.go`)。
  - **⚠️ 兼容约束**: 本阶段保持 `Event.RespondFunc func(code int, message string) error` 与 `Event.DenyFunc func() error` 不变，避免 Phase 1 破坏 `client_appserver_events.go`、`server_dynamic_tools.go`、`server_approval.go` 现有调用链。
  - **⚠️ EventHandler 迁移方向（必读）**:
    - `codex.EventHandler`（`client.go:L39`，签名 `func(event Event)`，单参数）→ **迁移到 `agentcore.EventHandler`**。此类型被 `CodexClient.SetEventHandler` 接口方法、`server_payload.go:AgentEventHandler` 返回值、`main.go:L73` 事件绑定使用。
    - `runner.EventHandler`（`manager.go:L106`，签名 `func(agentID string, event Event)`，双参数）→ **不迁移，保留在 `runner` 包**。P2 迁移时仅将其内部 `codex.Event` 引用替换为 `agentcore.Event`，不要与 `agentcore.EventHandler` 混淆。
- 通用事件常量（完整枚举，**全部** 进入 `agentcore`）：
  - 核心生命周期: `EventSessionConfigured`, `EventTurnStarted`, `EventTurnComplete`, `EventIdle`, `EventError`, `EventShutdownComplete`
  - Agent 输出: `EventAgentMessage`, `EventAgentMessageDelta`, `EventAgentMessageContentDelta`, `EventAgentReasoning`, `EventAgentReasoningDelta`, `EventAgentReasoningRaw`, `EventAgentReasoningRawDelta`, `EventAgentReasoningSectionBreak`, `EventAgentMessageCompleted`
  - 命令执行: `EventExecApprovalRequest`, `EventExecCommandBegin`, `EventExecCommandOutputDelta`, `EventExecCommandEnd`
  - 代码修改: `EventPatchApplyBegin`, `EventPatchApplyEnd`, `EventTurnDiff`, `EventUndoStarted`, `EventUndoCompleted`
  - MCP / Skills / Review: `EventMCPToolCallBegin`, `EventMCPToolCallEnd`, `EventMCPListToolsResponse`, `EventListSkillsResponse`, `EventEnteredReviewMode`, `EventExitedReviewMode`
  - 协作代理: `EventCollabAgentSpawnBegin`, `EventCollabAgentSpawnEnd`, `EventCollabAgentInteractionBegin`, `EventCollabAgentInteractionEnd`, `EventCollabWaitingBegin`, `EventCollabWaitingEnd`
    - **⚠️ 注意**: `EventCollabWaitingBegin` / `EventCollabWaitingEnd` 在 `client_appserver_events.go` 的 `methodToEventMap` 中无入站映射条目（Codex app-server 通知 → Go 事件），但 `notifications.go` 的 `eventMethodMap` 中有出站映射（Go 事件 → 前端推送：`"collab_waiting_begin" → "item/started"`）。入站缺失为**预存缺陷**，与本次迁移无关。迁移到 `agentcore` 不影响行为。
  - Dynamic Tools: `EventDynamicToolCall`
  - MCP 启动: `EventMCPStartupComplete`
  - 其他: `EventTokenCount`, `EventContextCompacted`, `EventThreadNameUpdated`, `EventThreadRolledBack`, `EventWarning`, `EventStreamError`, `EventBackgroundEvent`, `EventPlanDelta`, `EventPlanUpdate`
- `DynamicTool`、`DynamicToolCallData`。
  - **⚠️ 兼容约束**: 本阶段保留现有 camelCase JSON tag（如 `inputSchema`、`threadId`），确保与 Codex app-server 线协议兼容；去协议化改造后置。
- 通用事件 DTO（完整清单如下）：
  - `TextData`、`ErrorData`、`WarningData`、`TokenCountData`
  - `SessionConfiguredData`、`ExecApprovalRequestData`
  - `ExecCommandBeginData`、`ExecCommandEndData`、`PatchApplyData`
  - `CollabAgentData`、`ThreadNameUpdatedData`、`TurnDiffData`
- `Client` 接口、`ClientFactory`。
  - **⚠️ 兼容约束**: 本阶段保持 `Client` 与现有 `CodexClient` 签名一致（含 `GetPort`、`SpawnAndConnect`、`SendDynamicToolResult(..., requestID *int64)`）；仅迁移类型归属，不做接口收缩。
  - **⚠️ 兼容约束**: `ClientFactory` 保持现有兼容签名（`func(port int, agentID string) Client`）；`ClientOptions` 方案后置。
- 会话管理 DTO：`ThreadInfo`、`ResumeThreadRequest`、`ForkThreadRequest`、`ForkThreadResponse`。

> **⚠️ runner.AgentEvent 迁移**: `runner.AgentEvent` 结构体内嵌了 `codex.Event` 字段（`manager.go:92-95`），是公开导出类型。P2 迁移时必须同步将其 `Event` 字段类型改为 `agentcore.Event`，否则编译失败。

> **⚠️ 注意**: `codex.Client`（具体结构体, `client.go`）不改名，继续作为 `agentcore.Client` 接口的实现存在。`bus/router.go` 直接使用 `*codex.Client` 并访问其 `Transport` 字段，因此该结构体不可移动。

### 保留在 `internal/codex`（Codex 专属具体实现）

- `TransportMode`、`TransportSSE`（已废弃）。
- `Skill`、`SubmitMessage`、`CommandMessage`、`DynamicToolResultMessage`。
- `CreateThreadRequest`、`CreateThreadResponse`、`HealthResponse`（与 API 强挂钩）。
- `DynamicToolCallResponse`、`DynamicToolContentItem`（含外围服务定制化结构）。
- `CommandDef`、`AllCommands`（Codex 特定斜杠命令）。
- `methodToEventMap`（JSON-RPC 到底层事件的映射路由）。
- `rollout_reader.go` 整体（`FindRolloutPath`、`ReadRolloutMessagesWithTrim`）— 与 Codex 本地存储路径强耦合。
- `MCPToolCallData`、`MCPTool`、`MCPListToolsResponseData`、`ListSkillsResponseData` — 与 Codex MCP/Skills 协议挂钩。

> 说明：`CreateThreadRequest` 当前包含 `Skills []Skill`，属于 Codex 协议细节，不进入 `agentcore`，避免抽象层反向依赖 `codex`。

## Agent 执行步骤（Runbook）

按顺序执行，不并行改动 Phase。

## 多 Agent 执行模型（P1-P5）

本工作流明确采用 **5 个独立执行 Agent 串行交付**：

- `P1 Agent`：只负责 Phase 1（抽象层落地 + codex alias 桥接）
- `P2 Agent`：只负责 Phase 2（runner 迁移）
- `P3 Agent`：只负责 Phase 3（apiserver 迁移 + 入口绑定迁移）
- `P4 Agent`：只负责 Phase 4（全量验收与反查）
- `P5 Agent`：只负责 Phase 5（迁移后清理与测试重建）

执行纪律（必须遵守）：

1. 任一时刻只允许 1 个 Agent 写代码，其他 Agent 只读。
2. 下游 Agent 必须基于上游 Agent 的“交接包”继续执行，禁止跳步。
3. 每个 Agent 仅修改本阶段允许路径；跨阶段改动视为流程违规。
4. 发现基线失败、接口漂移、或未约定新增文件时，立即中止并回传阻塞点。

交接包（每个 Phase 结束必须提交）：

1. 变更文件清单（按路径分组：`agentcore` / `runner` / `apiserver` / `cmd` / `tests`）。
2. 核心 diff 摘要（仅列行为变化，不贴大段代码）。
3. 验证命令与结果（通过/失败 + 失败摘要）。
4. 风险与遗留项（明确是否阻塞下一阶段）。
5. 下一阶段建议起点（应先读哪些文件、先跑哪些命令）。

## 交付物落盘规范（强制）

所有阶段产物必须写入固定目录：

- 根目录：`.agent/workflows/cli-abstraction-handoffs/`

每个阶段必须至少落盘 3 份文件（`N` 为阶段号）：

1. 阶段报告：`.agent/workflows/cli-abstraction-handoffs/pN.md`
2. 验证日志：`.agent/workflows/cli-abstraction-handoffs/pN.checks.log`
3. 变更清单：`.agent/workflows/cli-abstraction-handoffs/pN.files.txt`

可选文件（有阻塞时必须写）：

1. 阻塞说明：`.agent/workflows/cli-abstraction-handoffs/pN.blockers.md`

全局指针文件（每阶段完成后必须更新）：

1. `.agent/workflows/cli-abstraction-handoffs/LATEST.md`
2. 内容至少包含：`current_phase`、`status`、`next_phase`、`updated_at`、`owner`

落盘时机要求：

1. 阶段开始时先创建 `pN.md` 并写入“目标与边界”。
2. 每轮验证后追加写入 `pN.checks.log`（不要覆盖历史）。
3. 阶段结束前更新 `pN.files.txt` 与 `LATEST.md`。

### Step 0: 基线确认（由 P1 Agent 执行，必须先过）

```bash
go build ./...
go test ./internal/codex/... -count=1
go test ./internal/runner/... -count=1
go test ./internal/apiserver/... -count=1
go test ./internal/bus/... -count=1
```

通过标准：5 条命令全部成功；否则先修复基线问题，不进入迁移。

Step 0 附加要求：

1. 必须把失败命令的原始输出摘录到 `.agent/workflows/cli-abstraction-handoffs/p1.md`（至少保留首个报错堆栈）。
2. 若基线失败，P1 只做“恢复基线”最小修复，不得提前实现 Phase 1 新功能。
3. 基线恢复后需要重跑 Step 0 全部命令，不允许只跑失败项。

### Step 1 / P1: 落地 Phase 1（建立抽象层）

P1 Agent 目标与边界：

1. 只处理 `internal/agentcore` 建层与 `internal/codex` alias 桥接，不处理 `runner` / `apiserver` 业务逻辑迁移。
2. 允许改动路径：`internal/agentcore/**`、`internal/codex/events.go`、`internal/codex/client.go`、`internal/codex/interface.go`。
3. 禁止改动路径：`internal/runner/**`、`internal/apiserver/**`、`cmd/**`（除非编译修复不可避免且在交接包中说明）。
4. 必须输出 `P1` 迁移映射表：`codex` 类型 -> `agentcore` 类型 -> 是否 alias 保留。

执行动作：

1. 新建 `internal/agentcore/types.go`，从 `internal/codex/events.go` 取出通用事件与 DTO（保持现有 wire-compatible 字段与签名，不做协议语义改造）。
2. 新建 `internal/agentcore/client.go`，从 `internal/codex/interface.go` 提取 `CodexClient` 接口并重命名为 `Client`，并从 `internal/codex/client.go` 提取 `EventHandler`，保持 `ClientFactory` 与现有参数形态兼容。
3. 修改 `internal/codex/events.go` 与 `internal/codex/client.go`，将已迁移类型改为 alias（含会话 DTO 与 `EventHandler`）。
4. 修改 `internal/codex/interface.go`，将接口改为 alias（不动 `CreateThread*` 等 Codex 专属协议类型）。
5. 运行格式化与验证。

```bash
gofmt -w internal/agentcore/types.go internal/agentcore/client.go internal/codex/events.go internal/codex/interface.go internal/codex/client.go
go build ./internal/agentcore/...
go test ./internal/codex/... -count=1
go test ./internal/runner/... -count=1
go test ./internal/apiserver/... -count=1
go test ./internal/bus/... -count=1
```

通过标准：`internal/agentcore` 可编译；核心测试通过。

P1 交付清单（交给 P2）：

1. `internal/agentcore` 的公开 API 清单（`Event` / `EventHandler` / `Client` / `ClientFactory` / DTO）。
2. alias 保留清单（哪些类型暂时仍从 `codex` 导出，原因是什么）。
3. 与 `bus` 相关的兼容声明（确认 `codex.Event` 路径仍可用）。

### Step 2 / P2: 落地 Phase 2（runner 切到 agentcore）

P2 Agent 目标与边界：

1. 只处理 `runner` 迁移，确保 `AgentManager` 的事件和客户端依赖切到 `agentcore`。
2. 允许改动路径：`internal/runner/**`；如需最小联动，最多触达 `internal/codex/interface.go` 的类型别名声明（仅允许签名/别名对齐，不得改动具体实现逻辑、导出协议语义或引入行为变化）。
3. 禁止改动路径：`internal/apiserver/**`、`cmd/**`、`internal/bus/**`。
4. fallback 行为必须保持：app-server 失败后仍可降级到 REST，且降级路径日志语义不退化。

执行动作：

1. 修改 `internal/runner/manager.go` 全部 `codex.*` 通用类型引用到 `agentcore`（完整引用清单见下方）。
2. 调整 `clientFactory` / `appServerFactory` / `restFactory` 到 `agentcore.ClientFactory`，保持现有 `(port, agentID)` 参数兼容。
3. 新增统一的工厂注入方法：`SetClientFactories(appFactory, restFactory agentcore.ClientFactory)`。
4. 将 `AgentEvent.Event` 字段从 `codex.Event` 改为 `agentcore.Event`。
5. 保持 app-server → REST fallback 与 `CleanOrphanedProcesses` 现有行为不变（本阶段不做职责迁移）。
6. 测试重建后置到 Step 5（迁移完成后统一重建/迁移测试），本阶段先确保生产代码迁移可编译、可运行。

**执行前行号预验证**（因代码可能在 P1 后有行号漂移，P2 Agent 必须先跑此命令比对下方表格）：

```bash
rg -n 'codex\.' internal/runner/manager.go --glob '!**/*_test.go'
```

若行号与下方表格偏移超过 ±5 行，P2 Agent 应更新表格后再继续迁移，避免误操作。

**`runner/manager.go` 的 `codex.*` 完整引用清单**（P2 Agent 必须逐条迁移）：

| 行号 | 引用 | 迁移目标 |
|------|------|----------|
| L53 | `Client codex.CodexClient` (AgentProcess 字段) | `agentcore.Client` |
| L94 | `Event codex.Event` (AgentEvent 字段) | `agentcore.Event` |
| L106 | `type EventHandler func(agentID string, event codex.Event)` | 替换为 `agentcore.Event` |
| L108 | `type clientFactory func(...) codex.CodexClient` | `agentcore.Client` |
| L134 | `codex.NewAppServerClient(port, agentID)` | 保留（默认工厂构造，Step 5 去耦） |
| L135 | `codex.NewClient(port, agentID)` | 保留（默认工厂构造，Step 5 去耦） |
| L150 | `func(agentID string, event codex.Event)` | `agentcore.Event` |
| L194 | `dynamicTools []codex.DynamicTool` (Launch 参数) | `agentcore.DynamicTool` |
| L231 | `func(event codex.Event)` (SetEventHandler 闭包) | `agentcore.Event` |
| L249 | `func(event codex.Event)` (fallback 闭包) | `agentcore.Event` |
| L263 | `codex.Event{Type: codex.EventBackgroundEvent, ...}` | `agentcore.Event` + `agentcore.EventBackgroundEvent` |
| L309 | `func(... event codex.Event)` (handleEvent 参数) | `agentcore.Event` |
| L336-341 | `codex.EventCollab*` (6 个常量) | `agentcore.EventCollab*` |
| L347 | `codex.EventShutdownComplete` | `agentcore.EventShutdownComplete` |
| L151 | `codex.EventAgentMessageDelta`, `codex.EventExecCommandOutputDelta` | `agentcore.*` |

```bash
if [ -f "internal/runner/manager_test.go" ]; then
  echo "INFO: internal/runner/manager_test.go exists; coverage gate deferred to Step 5"
  gofmt -w internal/runner/manager.go internal/runner/manager_test.go
else
  echo "WARN: internal/runner/manager_test.go not found; test rebuild is deferred to Step 5"
  gofmt -w internal/runner/manager.go
fi

rg -n 'func \(m \*AgentManager\) SetClientFactories\(' internal/runner/manager.go
rg -n 'agentcore\.ClientFactory' internal/runner/manager.go
go test ./internal/runner/... -v -count=1
```

通过标准：`SetClientFactories` 存在、参数类型包含 `agentcore.ClientFactory`，且 runner 包验证通过（测试重建与覆盖门禁在 Step 5 执行）。

P2 交付清单（交给 P3）：

1. `runner` 新旧类型对照（`codex.*` -> `agentcore.*`）。
2. `SetClientFactories` 的签名与调用点清单。
3. fallback 相关函数与行为说明（便于 P5 编写/迁移测试）。

### Step 3 / P3: 落地 Phase 3 (apiserver 链路迁移)

P3 Agent 目标与边界：

1. 处理 `apiserver` 全量 `codex` 通用类型迁移，并同步处理入口绑定 `cmd/app-server/main.go`。
2. 允许改动路径：`internal/apiserver/**`、`cmd/app-server/main.go`。
3. 禁止改动路径：`internal/bus/**`（白名单保留）、`internal/runner/**`（除编译修复的极小联动）。
4. 对 `GetActiveTurnID()` 断言必须保留兼容注释，避免误删“具体实现契约”。

将 `apiserver` 内 `codex.DynamicTool`、`codex.Event` 体系彻底迁往 `agentcore`。由于涉及面较广，分批迁移。
`bus` 保持 Codex 专属，不做类型迁移。

> **⚠️ 高危点 1:** `server_payload.go` 中的 `AgentEventHandler` 签名必须修改为返回 `agentcore.EventHandler`。
> **⚠️ 高危点 2:** `methods_turn.go` 中对 `GetActiveTurnID()` 的类型断言属于隐式扩展点，需要保留注释说明这是具体实现的契约。
> **⚠️ 高危点 3:** `cmd/app-server/main.go` 的 `mgr.SetOnEvent(...)` 入口绑定需同步改为 `agentcore.Event`，避免在入口层残留 `codex.Event`。
> **⚠️ 高危点 4:** `methods_turn.go:L57` 的 `resolveClientActiveTurnID(client codex.CodexClient)` 参数类型必须迁移为 `agentcore.Client`；同文件 L224/L239/L261 的 `codex.DynamicTool` 引用也需迁移为 `agentcore.DynamicTool`。

**迁移前先动态发现完整文件清单**（`apiserver` 文件数量会随分支变化，静态清单易遗漏）：

```bash
rg -l 'codex\.' internal/apiserver/ --glob '!**/*_test.go' | sort
```

**建议分批节奏:**

- **Batch 1 (工具与编排)**: `server_dynamic_tools.go`, `server_dynamic_tools_ext_registry.go`, `server_dynamic_tools_actions_ext.go`, `server_dynamic_tools_hierarchy_ext.go`, `server_dynamic_tools_semantic_ext.go`, `server_dynamic_tools_xref_ext.go`, `orchestration_tools.go`, `resource_tools.go`, `code_run_tools.go`, `server_approval.go`
  - **`server_dynamic_tools.go` 迁移明细**: `buildLSPDynamicTools() []codex.DynamicTool`（L149, L166）、`handleDynamicToolCall(agentID string, event codex.Event)`（L285）、`var call codex.DynamicToolCallData`（L334）、`call codex.DynamicToolCallData` 参数（L461）→ 全部替换为 `agentcore.*`。
  - **`server_dynamic_tools_ext_registry.go` 迁移明细**: `build func(*Server) []codex.DynamicTool`（L14, L25）、`buildExtendedLSPDynamicTools() []codex.DynamicTool`（L61, L67）、`dedupeDynamicToolsByName(tools []codex.DynamicTool)`（L78, L83）→ 全部替换为 `agentcore.DynamicTool`。
  - **`server_approval.go` 迁移明细**: `handleApprovalRequest(agentID, method string, payload map[string]any, event codex.Event)`（L56）→ 替换为 `agentcore.Event`。
- **Batch 2 (生命周期与载荷 + 入口绑定)**: `server_payload.go`, `methods_turn.go`, `methods_thread.go`, `methods_helpers.go`, `cmd/app-server/main.go`
  - **`methods_turn.go` 迁移注意**: 除事件常量外，还需迁移 `resolveClientActiveTurnID` 参数 `codex.CodexClient` → `agentcore.Client`（L57），以及 `collectDynamicToolNames`（L224）、`prependLSPAvailabilityWarning`（L239）、`resolveStartInstructionsForLaunch`（L261）的 `codex.DynamicTool` → `agentcore.DynamicTool`。
  - **`methods_turn.go` 高危点**: `resolveClientActiveTurnID`（L53-65）使用本地接口 `activeTurnIDReader` 对 `agentcore.Client` 做类型断言。迁移后必须在 `activeTurnIDReader` 接口定义上方**添加注释**，说明此为具体实现（`*codex.AppServerClient`）的契约，并非所有 `agentcore.Client` 实现都应提供 `GetActiveTurnID()` 方法。
  - **`methods_thread.go` 保留项**: `codex.FindRolloutPath`（L886）、`codex.ReadRolloutMessagesWithTrim`（L900/L1232）属于 Codex 专属 `rollout_reader` 依赖，**不迁移**。
  - **`methods_thread.go` 迁移项**: `codex.ResumeThreadRequest`（L111）、`codex.ForkThreadRequest`（L145）属于会话管理 DTO（边界定义中已明确迁移到 agentcore），**需迁移**；`codex.EventAgentMessage`（L917）等通用常量同样迁移为 `agentcore.*`。
  - **`methods_helpers.go` 迁移项**: `codex.ResumeThreadRequest`（L402）→ `agentcore.ResumeThreadRequest`，与 `methods_thread.go` 同类型迁移。
  - **`notifications.go` 同步义务（不在迁移范围但需注意）**: 该文件的 `eventMethodMap` 使用原始字符串（如 `"session_configured"`）而非 `codex.Event*` 常量作为键，因此无 `codex` import，不在 Batch 1/2 迁移范围内。但这些字符串值与 `agentcore.Event*` 常量值相同——**如果迁移中修改了任何常量的字符串值，此文件会静默失配**。P5 阶段应将 `eventMethodMap` 键改为引用 `agentcore.Event*` 常量以消除漂移风险。

> 当前经 `rg -l` 验证，Batch 1 + Batch 2 已完整覆盖 apiserver 中所有引用 `codex.*` 的非测试文件。`notifications.go` 无 `codex` import 故不在迁移范围，但有同步义务（见上）。动态发现命令用于执行时二次确认，防止新增文件遗漏。

执行小检查确保未漏（对比动态发现清单与 Batch 覆盖）：
```bash
go test ./internal/apiserver/... -count=1
# 加载共享辅助函数（extract_codex_aliases / strip_go_noise）
source .agent/workflows/cli-abstraction-handoffs/verify-helpers.sh

# 二次比对: 确认 Batch 1+2 覆盖了所有引用 codex 符号的非测试文件（alias-safe，去除注释/字符串噪声）
# 注意: methods_thread.go 保留 codex.FindRolloutPath / codex.ReadRolloutMessagesWithTrim（rollout_reader 白名单）
remaining=""
apiserver_codex_files="$(rg -l '"github.com/multi-agent/go-agent-v2/internal/codex"' internal/apiserver/ --glob '!**/*_test.go' --glob '!**/methods_thread.go' || true)"
for file in $apiserver_codex_files; do
  aliases_raw="$(extract_codex_aliases "$file" | sort -u)"
  invalid_aliases="$(printf '%s\n' "$aliases_raw" | rg '^(\\.|_)$' || true)"
  test -z "$invalid_aliases" || { echo "invalid codex import alias in $file:"; printf '%s\n' "$invalid_aliases"; exit 1; }
  aliases="$(printf '%s\n' "$aliases_raw" | rg -v '^(\\.|_)$' || true)"
  for alias in $aliases; do
    refs="$(strip_go_noise < "$file" | rg -o "\\b${alias}\\.[A-Za-z_][A-Za-z0-9_]*\\b" || true)"
    [ -z "$refs" ] && continue
    remaining="${remaining}${file}"$'\n'
    break
  done
done
test -z "$remaining" || { echo "uncovered files:"; printf '%s\n' "$remaining"; exit 1; }

# 单独验证 methods_thread.go 仅保留白名单引用（rollout_reader 函数，alias 安全）
methods_thread_codex_aliases_raw="$(extract_codex_aliases internal/apiserver/methods_thread.go | sort -u)"
methods_thread_invalid_aliases="$(printf '%s\n' "$methods_thread_codex_aliases_raw" | rg '^(\\.|_)$' || true)"
test -z "$methods_thread_invalid_aliases" || { echo "invalid codex import alias in methods_thread.go:"; printf '%s\n' "$methods_thread_invalid_aliases"; exit 1; }
methods_thread_codex_aliases="$(printf '%s\n' "$methods_thread_codex_aliases_raw" | rg -v '^(\\.|_)$' || true)"
methods_thread_codex_non_whitelist_refs=""
for alias in $methods_thread_codex_aliases; do
  refs="$(strip_go_noise < internal/apiserver/methods_thread.go | rg -o "\\b${alias}\\.[A-Za-z_][A-Za-z0-9_]*\\b" || true)"
  [ -z "$refs" ] && continue
  disallowed_refs="$(printf '%s\n' "$refs" | rg -v "^${alias}\\.(FindRolloutPath|ReadRolloutMessagesWithTrim)$" || true)"
  if [ -n "$disallowed_refs" ]; then
    methods_thread_codex_non_whitelist_refs="${methods_thread_codex_non_whitelist_refs}${disallowed_refs}"$'\n'
  fi
done
test -z "$(printf '%s' "$methods_thread_codex_non_whitelist_refs" | tr -d '[:space:]')" || { echo "methods_thread.go non-whitelist codex refs:"; printf '%s\n' "$methods_thread_codex_non_whitelist_refs"; exit 1; }
```

P3 交付清单（交给 P4）：

1. `internal/apiserver` 与 `cmd/app-server/main.go` 的迁移文件清单（按事件链路 / 工具链路 / 生命周期链路分组）。
2. 高危点验证摘要：`AgentEventHandler`、`handleApprovalRequest`、`dynamic_tool_call`、`GetActiveTurnID()` 断言注释是否保留。
3. `codex.*` 残留引用清单（若有，必须逐条给出“为何保留”理由；无理由残留视为阻塞）。
4. 可复现验证命令列表（包含至少 1 条 `go test` 与 1 条 `rg` 反查命令）。

### Step 4 / P4: 全量验收与反查验证

P4 Agent 目标与边界：

1. 只做“全量验证 + 证据归档 + 失败分流”，默认不改业务代码。
2. 允许改动路径：验证脚本、工作流文档、交接报告；禁止直接改 `runner/apiserver/codex` 业务逻辑（除非收到显式返工指令）。
3. 任一检查失败时，P4 负责生成失败证据并把问题回退给对应阶段，不做跨阶段代修。

P4 失败分流规则：

1. `internal/runner` 相关失败 -> 回退 `P2`。
2. `internal/apiserver` 或 `cmd/app-server/main.go` 相关失败 -> 回退 `P3`。
3. `internal/agentcore` 抽象层边界或 alias 契约失败 -> 回退 `P1`。
4. 清理后遗症（测试缺失、临时代码残留）-> 回退 `P5`。

验证整个系统内彻底洗清了 `runner` / `apiserver` 对 `codex` 通用对象的依赖。`cmd/app-server/main.go` 作为组合根允许保留 `codex` 适配器构造依赖（仅限工厂注入）；`bus` 依赖属于白名单，不在本步骤迁移。

```bash
# 1. 全量测试必须通过
go test ./... -count=1

# 2. runner 不再使用 codex 通用类型
runner_violations="$(rg -n 'codex\.(Event(?:[A-Z][A-Za-z0-9_]*)?|DynamicTool|DynamicToolCallData|Client|CodexClient|EventHandler|ThreadInfo|ResumeThreadRequest|ForkThreadRequest|ForkThreadResponse|TextData|ErrorData|WarningData|TokenCountData|SessionConfiguredData|ExecApprovalRequestData|ExecCommandBeginData|ExecCommandEndData|PatchApplyData|CollabAgentData|ThreadNameUpdatedData|TurnDiffData)\b' internal/runner --glob '!**/*_test.go' | rg -v '^[^:]+:[0-9]+:\s*(//|/\*|\*/|\*[[:space:]])' || true)"
test -z "$runner_violations" || { printf '%s\n' "$runner_violations"; exit 1; }

# 3. runner 除 manager.go 外不应再导入 codex
runner_import_violations="$(rg -n '"github.com/multi-agent/go-agent-v2/internal/codex"' internal/runner --glob '!internal/runner/manager.go' --glob '!**/*_test.go' || true)"
test -z "$runner_import_violations" || { printf '%s\n' "$runner_import_violations"; exit 1; }

# 4. runner 允许 manager.go 保留最多一个 codex import（默认工厂构造）
runner_import_count="$(rg -c '"github.com/multi-agent/go-agent-v2/internal/codex"' internal/runner/manager.go || echo 0)"
test "$runner_import_count" -le 1 || { echo "runner codex import count: $runner_import_count"; exit 1; }

# 加载共享辅助函数 (extract_codex_aliases / strip_go_noise)
source .agent/workflows/cli-abstraction-handoffs/verify-helpers.sh

# 4.1 manager.go 的 codex 引用必须仅用于默认工厂构造（alias-safe，去除注释/字符串噪声）
runner_manager_codex_aliases_raw="$(extract_codex_aliases internal/runner/manager.go | sort -u)"
runner_manager_invalid_aliases="$(printf '%s\n' "$runner_manager_codex_aliases_raw" | rg '^(\\.|_)$' || true)"
test -z "$runner_manager_invalid_aliases" || { echo "invalid codex import alias in manager.go:"; printf '%s\n' "$runner_manager_invalid_aliases"; exit 1; }
runner_manager_codex_aliases="$(printf '%s\n' "$runner_manager_codex_aliases_raw" | rg -v '^(\\.|_)$' || true)"
runner_manager_codex_non_factory_refs=""
for alias in $runner_manager_codex_aliases; do
  refs="$(strip_go_noise < internal/runner/manager.go | rg -o "\\b${alias}\\.[A-Za-z_][A-Za-z0-9_]*\\b" || true)"
  [ -z "$refs" ] && continue
  disallowed_refs="$(printf '%s\n' "$refs" | rg -v "^${alias}\\.(NewAppServerClient|NewClient)$" || true)"
  if [ -n "$disallowed_refs" ]; then
    runner_manager_codex_non_factory_refs="${runner_manager_codex_non_factory_refs}${disallowed_refs}"$'\n'
  fi
done
test -z "$(printf '%s' "$runner_manager_codex_non_factory_refs" | tr -d '[:space:]')" || { printf '%s\n' "$runner_manager_codex_non_factory_refs"; exit 1; }

# 5. apiserver 不再使用 codex 通用类型
# 注意: methods_thread.go 保留 codex.FindRolloutPath / codex.ReadRolloutMessagesWithTrim（rollout_reader 白名单），
# 这些函数名不在下方正则匹配范围内，不会被误报。
apiserver_violations="$(rg -n 'codex\.(Event(?:[A-Z][A-Za-z0-9_]*)?|DynamicTool|DynamicToolCallData|Client|CodexClient|ClientFactory|EventHandler|ThreadInfo|ResumeThreadRequest|ForkThreadRequest|ForkThreadResponse|TextData|ErrorData|WarningData|TokenCountData|SessionConfiguredData|ExecApprovalRequestData|ExecCommandBeginData|ExecCommandEndData|PatchApplyData|CollabAgentData|ThreadNameUpdatedData|TurnDiffData)\b' internal/apiserver --glob '!**/*_test.go' | rg -v '^[^:]+:[0-9]+:\s*(//|/\*|\*/|\*[[:space:]])' || true)"
test -z "$apiserver_violations" || { printf '%s\n' "$apiserver_violations"; exit 1; }

# 5.1 apiserver 除 methods_thread.go 白名单文件外不应导入 codex（防 alias 绕过）
apiserver_import_violations="$(rg -n '"github.com/multi-agent/go-agent-v2/internal/codex"' internal/apiserver --glob '!internal/apiserver/methods_thread.go' --glob '!**/*_test.go' || true)"
test -z "$apiserver_import_violations" || { printf '%s\n' "$apiserver_import_violations"; exit 1; }

# 5.2 methods_thread.go 中 codex 引用仅允许 rollout_reader 白名单符号（alias 安全）
methods_thread_codex_aliases_raw="$(extract_codex_aliases internal/apiserver/methods_thread.go | sort -u)"
methods_thread_invalid_aliases="$(printf '%s\n' "$methods_thread_codex_aliases_raw" | rg '^(\\.|_)$' || true)"
test -z "$methods_thread_invalid_aliases" || { echo "invalid codex import alias in methods_thread.go:"; printf '%s\n' "$methods_thread_invalid_aliases"; exit 1; }
methods_thread_codex_aliases="$(printf '%s\n' "$methods_thread_codex_aliases_raw" | rg -v '^(\\.|_)$' || true)"
methods_thread_codex_non_whitelist_refs=""
for alias in $methods_thread_codex_aliases; do
  refs="$(strip_go_noise < internal/apiserver/methods_thread.go | rg -o "\\b${alias}\\.[A-Za-z_][A-Za-z0-9_]*\\b" || true)"
  [ -z "$refs" ] && continue
  disallowed_refs="$(printf '%s\n' "$refs" | rg -v "^${alias}\\.(FindRolloutPath|ReadRolloutMessagesWithTrim)$" || true)"
  if [ -n "$disallowed_refs" ]; then
    methods_thread_codex_non_whitelist_refs="${methods_thread_codex_non_whitelist_refs}${disallowed_refs}"$'\n'
  fi
done
test -z "$(printf '%s' "$methods_thread_codex_non_whitelist_refs" | tr -d '[:space:]')" || { printf '%s\n' "$methods_thread_codex_non_whitelist_refs"; exit 1; }

# 6. agentcore 层绝不能反向依赖 codex（防腐层底线）
go list ./internal/agentcore/... >/dev/null || { echo "internal/agentcore package not found"; exit 1; }
agentcore_codex_imports="$(go list -f '{{join .Imports "\n"}}' ./internal/agentcore/... | rg "internal/codex" || true)"
test -z "$agentcore_codex_imports" || { printf '%s\n' "$agentcore_codex_imports"; exit 1; }

# 7. app-server 入口不再依赖 codex 通用事件类型（应走 agentcore.Event）
main_codex_aliases_raw="$(extract_codex_aliases cmd/app-server/main.go | sort -u)"
main_invalid_aliases="$(printf '%s\n' "$main_codex_aliases_raw" | rg '^(\\.|_)$' || true)"
test -z "$main_invalid_aliases" || { echo "invalid codex import alias in main.go:"; printf '%s\n' "$main_invalid_aliases"; exit 1; }
main_codex_aliases="$(printf '%s\n' "$main_codex_aliases_raw" | rg -v '^(\\.|_)$' || true)"
app_server_event_type_violations=""
for alias in $main_codex_aliases; do
  found="$(strip_go_noise < cmd/app-server/main.go | rg -o "\\b${alias}\\.(Event|EventHandler)\\b" || true)"
  if [ -n "$found" ]; then
    app_server_event_type_violations="${app_server_event_type_violations}${found}"$'\n'
  fi
done
test -z "$(printf '%s' "$app_server_event_type_violations" | tr -d '[:space:]')" || { printf '%s\n' "$app_server_event_type_violations"; exit 1; }

# 8. app-server 入口允许 codex 依赖，但仅限工厂注入（NewAppServerClient/NewClient）
app_server_codex_non_factory_refs=""
for alias in $main_codex_aliases; do
  refs="$(strip_go_noise < cmd/app-server/main.go | rg -o "\\b${alias}\\.[A-Za-z_][A-Za-z0-9_]*\\b" || true)"
  [ -z "$refs" ] && continue
  disallowed_refs="$(printf '%s\n' "$refs" | rg -v "^${alias}\\.(NewAppServerClient|NewClient)$" || true)"
  if [ -n "$disallowed_refs" ]; then
    app_server_codex_non_factory_refs="${app_server_codex_non_factory_refs}${disallowed_refs}"$'\n'
  fi
done
test -z "$(printf '%s' "$app_server_codex_non_factory_refs" | tr -d '[:space:]')" || { printf '%s\n' "$app_server_codex_non_factory_refs"; exit 1; }

# 9. bus 白名单验证：本阶段必须仍依赖 codex 事件类型（alias-safe，去除注释/字符串噪声）
bus_codex_event_refs=""
bus_codex_import_files="$(rg -l '"github.com/multi-agent/go-agent-v2/internal/codex"' internal/bus --glob '!**/*_test.go' || true)"
for file in $bus_codex_import_files; do
  aliases_raw="$(extract_codex_aliases "$file" | sort -u)"
  invalid_aliases="$(printf '%s\n' "$aliases_raw" | rg '^(\\.|_)$' || true)"
  test -z "$invalid_aliases" || { echo "invalid codex import alias in $file:"; printf '%s\n' "$invalid_aliases"; exit 1; }
  aliases="$(printf '%s\n' "$aliases_raw" | rg -v '^(\\.|_)$' || true)"
  for alias in $aliases; do
    refs="$(strip_go_noise < "$file" | rg -o "\\b${alias}\\.Event([A-Z][A-Za-z0-9_]*)?\\b" || true)"
    [ -z "$refs" ] && continue
    bus_codex_event_refs="${bus_codex_event_refs}${file}:"$'\n'"${refs}"$'\n'
  done
done
test -n "$(printf '%s' "$bus_codex_event_refs" | tr -d '[:space:]')" || { echo "internal/bus lost expected codex event dependency"; exit 1; }

# 10. bus 反向约束：本阶段不得引入 agentcore 依赖
# TODO: 此检查仅适用于 Phase 1 白名单阶段，后续若启动 "bus 通用化改造" 工作流则应移除
bus_agentcore_refs="$(rg -n 'agentcore\.|internal/agentcore' internal/bus || true)"
test -z "$bus_agentcore_refs" || { printf '%s\n' "$bus_agentcore_refs"; exit 1; }
```

通过标准：上述所有检查无错误输出，系统行为零回归。

P4 交付清单（交给 P5）：

1. 全量校验日志（每条检查命令结果 + 首个失败点）。
2. 残余技术债列表（需保留 alias、待删桥接、待补测试）。
3. 对白名单结论再确认：`internal/bus` 仍依赖 `codex.Event` 且为预期结果。

### Step 5 / P5: 迁移后废弃代码清理（收尾阶段）

P5 Agent 目标与边界：

1. 清理仅限“迁移期临时代码”，不重构业务流程，不引入接口收缩。
2. 必须完成测试重建/迁移（特别是 runner fallback 相关测试）。
3. 允许保留 `bus` 需要的 `codex` 导出；不得把“保留白名单”误当作“清理遗漏”。
4. 交付后文档应可直接作为回归 SOP，其他 Agent 无需再补上下文。
5. 依赖门槛：`codex` 仅允许存在于“适配器实现 + 组合根（`cmd/app-server/main.go`）+ `bus` 白名单 + `apiserver/methods_thread.go` 的 rollout_reader 白名单调用”；`runner` 非测试代码禁止 import `codex`，`apiserver` 非测试代码仅允许上述白名单。

在 Step 4 全量验收通过后执行，目标是移除阶段性兼容代码，避免长期技术债。

执行动作：

1. 清理 `runner` / `apiserver` 中为迁移保留的兼容 alias 与桥接逻辑（如 “Phase 1 alias”、“兼容期”），并去除不再需要的 import。
2. `internal/codex` 中为 `internal/bus` 白名单保留的兼容导出（`Event` / `Event*` 路径）本阶段不清零，并在源码保留明确注释。
3. **默认工厂去耦（单一路线）**：移除 `runner/manager.go` 内硬编码 `codex.NewAppServerClient` / `codex.NewClient`，统一改为构造注入。具体做法：
   - `NewAgentManager` 固化为带参构造并返回错误：`NewAgentManager(appFactory, restFactory agentcore.ClientFactory) (*AgentManager, error)`
   - `NewAgentManager` 对空 factory 必须返回显式 `error`（禁止 `panic` 与隐式默认值回退）
   - `main.go` 作为组合根显式注入并处理错误：`mgr, err := runner.NewAgentManager(codex.NewAppServerClient, codex.NewClient)`
   - 生产路径不再依赖 `SetClientFactories` 二次注入（该方法如保留，仅作为测试注入点）
   - 完成后 `internal/runner/**` 非测试代码对 `internal/codex` 的 import 清零，`internal/apiserver/**` 仅保留 rollout_reader 白名单 import
4. **`notifications.go` 常量化（分层处理）**：`eventMethodMap` 的 70 个字符串键中，部分 **有对应 `agentcore.Event*` 常量**（如 `"session_configured"` → `agentcore.EventSessionConfigured`），部分 **无对应常量**（如 `"exec_output_delta"`、`"collab_agent_launched"`、`"login_completed"` 等属于 apiserver 层私有映射）。P5 Agent 必须先执行前置分析，再分类处理：
   - **前置分析命令**（列出所有无常量对应的键）：
     ```bash
     # 提取 eventMethodMap 中所有字符串键，检查哪些在 agentcore 中无对应 Event* 常量
      rg -oN '^\t"([a-z_]+)":\s' -r '$1' internal/apiserver/notifications.go | while read key; do
       rg -q "Event.*= \"$key\"" internal/agentcore/types.go || echo "NO_CONSTANT: $key"
     done
     ```
   - **有对应常量** → 替换为 `agentcore.Event*` 引用
   - **无对应常量但属于通用事件** → 先在 `agentcore/types.go` 新增常量，再替换
   - **属于 apiserver 层私有映射（不具备跨 CLI 通用性）** → 保留字符串值，加注释标注 `// apiserver-only, no agentcore constant`
5. 重建/迁移测试（含 `internal/runner/manager_test.go`），补齐 app-server → REST fallback 覆盖。
6. 清理后执行全量回归验证，确认行为无回退。

```bash
gofmt -w internal/agentcore/ internal/codex/ internal/runner/ internal/apiserver/ cmd/app-server/main.go

# 加载共享辅助函数 (extract_codex_aliases / strip_go_noise)
source .agent/workflows/cli-abstraction-handoffs/verify-helpers.sh

# bus 白名单：本阶段要求其继续走 codex.Event / codex.Event*，不做迁移（alias-safe，去除注释/字符串噪声）
bus_codex_event_refs=""
bus_codex_import_files="$(rg -l '"github.com/multi-agent/go-agent-v2/internal/codex"' internal/bus --glob '!**/*_test.go' || true)"
for file in $bus_codex_import_files; do
  aliases_raw="$(extract_codex_aliases "$file" | sort -u)"
  invalid_aliases="$(printf '%s\n' "$aliases_raw" | rg '^(\\.|_)$' || true)"
  test -z "$invalid_aliases" || { echo "invalid codex import alias in $file:"; printf '%s\n' "$invalid_aliases"; exit 1; }
  aliases="$(printf '%s\n' "$aliases_raw" | rg -v '^(\\.|_)$' || true)"
  for alias in $aliases; do
    refs="$(strip_go_noise < "$file" | rg -o "\\b${alias}\\.Event([A-Z][A-Za-z0-9_]*)?\\b" || true)"
    [ -z "$refs" ] && continue
    bus_codex_event_refs="${bus_codex_event_refs}${file}:"$'\n'"${refs}"$'\n'
  done
done
test -n "$(printf '%s' "$bus_codex_event_refs" | tr -d '[:space:]')" || { echo "expected internal/bus to keep codex event dependency in this phase"; exit 1; }

test -f internal/runner/manager_test.go || { echo "missing internal/runner/manager_test.go; rebuild tests first"; exit 1; }
runner_tests_count="$(go test ./internal/runner/... -list '^Test' | rg '^Test' | wc -l | tr -d ' ')"
test "${runner_tests_count:-0}" -gt 0 || { echo "runner test list is empty"; exit 1; }
# runner fallback 行为门禁：必须实际执行并通过至少 1 个 fallback 相关测试
runner_fallback_test_log="$(mktemp)"
set +e
go test ./internal/runner/... -run 'Fallback|AppServer|REST' -count=1 -v >"$runner_fallback_test_log" 2>&1
runner_fallback_test_rc=$?
set -e
cat "$runner_fallback_test_log"
test "$runner_fallback_test_rc" -eq 0 || { echo "runner fallback-focused tests failed"; exit 1; }
runner_fallback_pass_count="$(rg -n '^--- PASS: Test.*(Fallback|AppServer|REST)' "$runner_fallback_test_log" | wc -l | tr -d ' ')"
test "${runner_fallback_pass_count:-0}" -gt 0 || { echo "runner fallback gate requires at least one executed fallback-oriented test"; cat "$runner_fallback_test_log"; exit 1; }

# runner 构造器必须是带 factory 的显式 error 签名
rg -n 'func[[:space:]]+NewAgentManager\(' internal/runner/manager.go >/dev/null || { echo "runner.NewAgentManager function not found"; exit 1; }
rg -n 'appFactory,[[:space:]]*restFactory[[:space:]]+agentcore\.ClientFactory' internal/runner/manager.go >/dev/null || { echo "runner.NewAgentManager must accept (appFactory, restFactory agentcore.ClientFactory)"; exit 1; }
rg -n '\(\*AgentManager,[[:space:]]*error\)' internal/runner/manager.go >/dev/null || { echo "runner.NewAgentManager must return (*AgentManager, error)"; exit 1; }
new_manager_nil_guard="$(rg -n 'appFactory[[:space:]]*==[[:space:]]*nil|restFactory[[:space:]]*==[[:space:]]*nil' internal/runner/manager.go || true)"
test -n "$new_manager_nil_guard" || { echo "runner.NewAgentManager must validate nil factories and return error"; exit 1; }

# main.go 必须用 codex 工厂直接调用 NewAgentManager（参数绑定级校验）
main_codex_aliases_raw="$(extract_codex_aliases cmd/app-server/main.go | sort -u)"
test -n "$main_codex_aliases_raw" || { echo "main.go must import internal/codex for factory injection"; exit 1; }
main_invalid_aliases="$(printf '%s\n' "$main_codex_aliases_raw" | rg '^(\\.|_)$' || true)"
test -z "$main_invalid_aliases" || { echo "invalid codex import alias in main.go:"; printf '%s\n' "$main_invalid_aliases"; exit 1; }
main_codex_aliases="$(printf '%s\n' "$main_codex_aliases_raw" | rg -v '^(\\.|_)$' || true)"
app_server_constructor_binding_ok=""
app_server_codex_non_factory_refs=""
for alias in $main_codex_aliases; do
  constructor_match="$(strip_go_noise < cmd/app-server/main.go | rg "runner\\.NewAgentManager\\([[:space:]]*${alias}\\.NewAppServerClient[[:space:]]*,[[:space:]]*${alias}\\.NewClient[[:space:]]*\\)" || true)"
  if [ -n "$constructor_match" ]; then
    app_server_constructor_binding_ok="yes"
  fi
  refs="$(strip_go_noise < cmd/app-server/main.go | rg -o "\\b${alias}\\.[A-Za-z_][A-Za-z0-9_]*\\b" || true)"
  [ -z "$refs" ] && continue
  disallowed_refs="$(printf '%s\n' "$refs" | rg -v "^${alias}\\.(NewAppServerClient|NewClient)$" || true)"
  if [ -n "$disallowed_refs" ]; then
    app_server_codex_non_factory_refs="${app_server_codex_non_factory_refs}${disallowed_refs}"$'\n'
  fi
done
test -n "$app_server_constructor_binding_ok" || { echo "main.go must call runner.NewAgentManager(codex.NewAppServerClient, codex.NewClient)"; exit 1; }
test -z "$(printf '%s' "$app_server_codex_non_factory_refs" | tr -d '[:space:]')" || { printf '%s\n' "$app_server_codex_non_factory_refs"; exit 1; }
main_constructor_assign="$(strip_go_noise < cmd/app-server/main.go | rg -n '[A-Za-z_][A-Za-z0-9_]*[[:space:]]*,[[:space:]]*err[[:space:]]*:=[[:space:]]*runner\.NewAgentManager\(' || true)"
test -n "$main_constructor_assign" || { echo "main.go must capture error from runner.NewAgentManager (e.g. mgr, err := ...)"; exit 1; }
main_err_guard="$(strip_go_noise < cmd/app-server/main.go | rg -n 'if[[:space:]]+err[[:space:]]*!=[[:space:]]*nil' || true)"
test -n "$main_err_guard" || { echo "main.go must handle NewAgentManager error"; exit 1; }
main_set_client_factories_calls="$(strip_go_noise < cmd/app-server/main.go | rg -n 'SetClientFactories[[:space:]]*\(' || true)"
test -z "$main_set_client_factories_calls" || { echo "main.go should not rely on SetClientFactories in production path"; printf '%s\n' "$main_set_client_factories_calls"; exit 1; }

runner_codex_imports_after_cleanup="$(rg -n '\"github.com/multi-agent/go-agent-v2/internal/codex\"' internal/runner --glob '!**/*_test.go' || true)"
test -z "$runner_codex_imports_after_cleanup" || { printf '%s\n' "$runner_codex_imports_after_cleanup"; exit 1; }
apiserver_codex_imports_after_cleanup="$(rg -n '\"github.com/multi-agent/go-agent-v2/internal/codex\"' internal/apiserver --glob '!internal/apiserver/methods_thread.go' --glob '!**/*_test.go' || true)"
test -z "$apiserver_codex_imports_after_cleanup" || { printf '%s\n' "$apiserver_codex_imports_after_cleanup"; exit 1; }
methods_thread_codex_aliases_raw="$(extract_codex_aliases internal/apiserver/methods_thread.go | sort -u)"
methods_thread_invalid_aliases="$(printf '%s\n' "$methods_thread_codex_aliases_raw" | rg '^(\\.|_)$' || true)"
test -z "$methods_thread_invalid_aliases" || { echo "invalid codex import alias in methods_thread.go:"; printf '%s\n' "$methods_thread_invalid_aliases"; exit 1; }
methods_thread_codex_aliases="$(printf '%s\n' "$methods_thread_codex_aliases_raw" | rg -v '^(\\.|_)$' || true)"
methods_thread_codex_non_whitelist_refs=""
for alias in $methods_thread_codex_aliases; do
  refs="$(strip_go_noise < internal/apiserver/methods_thread.go | rg -o "\\b${alias}\\.[A-Za-z_][A-Za-z0-9_]*\\b" || true)"
  [ -z "$refs" ] && continue
  disallowed_refs="$(printf '%s\n' "$refs" | rg -v "^${alias}\\.(FindRolloutPath|ReadRolloutMessagesWithTrim)$" || true)"
  if [ -n "$disallowed_refs" ]; then
    methods_thread_codex_non_whitelist_refs="${methods_thread_codex_non_whitelist_refs}${disallowed_refs}"$'\n'
  fi
done
test -z "$(printf '%s' "$methods_thread_codex_non_whitelist_refs" | tr -d '[:space:]')" || { printf '%s\n' "$methods_thread_codex_non_whitelist_refs"; exit 1; }

go test ./... -count=1
```

通过标准：`internal/runner/**` 非测试代码清零 `codex` import；`internal/apiserver/**` 非测试代码仅允许 `methods_thread.go` 的 rollout_reader 白名单依赖；`cmd/app-server/main.go` 仅保留 `codex` 工厂注入引用且不走 `SetClientFactories` 生产注入；`bus` 白名单依赖保留且有注释；全量测试通过。

P5 最终交付清单（交给负责人/合并 Agent）：

1. 最终变更清单（按 P1-P5 归档，标注每个文件属于哪个阶段）。
2. 最终验证证据（`go test ./... -count=1` 与关键 `rg` 反查结果）。
3. 白名单声明（`internal/bus` 继续依赖 `codex.Event` / `codex.Event*` 是本期有意设计）。
4. 未完成事项与下期建议（仅列真实残留，不写泛化 TODO）。

## P1-P5 标准交接模板（建议直接复制）

每个 Agent 完成后，建议提交如下结构，减少上下游理解偏差：

````md
# P{N} Handoff

## 1) 变更范围
- 修改文件:
- 新增文件:
- 删除文件:
- 未触达但相关文件:

## 2) 行为变化摘要
- 变化点 A:
- 变化点 B:
- 兼容保留点:

## 3) 验证命令
- [ ] go build ./...
- [ ] go test ...
- [ ] rg 反查 ...

失败命令摘录:
```text
<首个失败堆栈或关键报错>
```

## 4) 风险与阻塞
- 风险:
- 是否阻塞下游: yes/no
- 若阻塞，建议处理顺序:

## 5) 给下一阶段的建议
- 下一阶段先读:
- 下一阶段先跑:
- 明确不要做:
````

阶段阻塞判定（统一标准）：

1. 编译失败且无法在本阶段允许路径内修复，判定阻塞。
2. 关键接口签名与上游交接不一致，判定阻塞。
3. 白名单策略冲突（例如误清理 `bus` 依赖），判定阻塞。
4. 测试全红但无最小复现线索，判定阻塞。

阶段恢复策略（出现阻塞时）：

1. 先提交最小诊断报告，不要继续扩大改动面。
2. 在报告中明确“最后一个可工作的 commit/状态”。
3. 提供最多 3 条可执行修复建议，避免开放式描述。

## P1-P5 路径授权矩阵

为避免越权改动，每个 Agent 的主写路径如下：

1. `P1`：`internal/agentcore/**`、`internal/codex/events.go`、`internal/codex/client.go`、`internal/codex/interface.go`
2. `P2`：`internal/runner/**`（可最小触达 `internal/codex/interface.go`，仅限类型别名/签名对齐，禁止行为变更）
3. `P3`：`internal/apiserver/**`、`cmd/app-server/main.go`
4. `P4`：原则上只读；如需修复，仅限“验证脚本误报”类小修
5. `P5`：`internal/runner/**`、`internal/apiserver/**`、`internal/codex/**`（但保留 `bus` 白名单所需导出）

额外说明：

1. `internal/bus/**` 在本工作流里默认只读（除非用户单独下达“迁移 bus”指令）。
2. 若某阶段必须越权改动，需在交接包中记录“越权原因 + 影响评估 + 回滚方式”。

---

## 明确非目标（本工作流不做）

以下内容不影响 CLI 解耦，移出本计划：

- LSP handler 泛型消重
- Resource handler 泛型消重
- Tool schema 外置（`go:embed`）
- `bus` 的通用化改造（本阶段维持 Codex 专属消息通道，允许继续依赖 `codex.Event` / `codex.Event*` 白名单）
- `agentcore.Client` 接口收缩（移除 `GetPort`、重塑 `SpawnAndConnect` / `SendDynamicToolResult` / `ClientFactory` 签名）
- 审批与工具回传链路的协议语义抽象（例如 `RespondApproval`、去除 `requestID` 泄漏）
- `CleanOrphanedProcesses` 的职责迁移（runner ↔ codex）
- 以强类型 DTO 替换 `extractLastAgentMessage` 的历史 payload 解析链路
- `Event.RespondFunc` 的协议语义抽象（移除 JSON-RPC code 参数）与相关调用链重构
- `DynamicTool` / `DynamicToolCallData` 的 JSON tag 去协议化（camelCase → 中立结构）
- 会话模型中特定网络实现细节泄露（如 `ForkThreadResponse` 强带 `Port` 字段）
- `uistate` 事件路由解耦（当前通过 `string` 匹配事件名，不直接依赖 `codex.*` 类型）

如需推进，单独开“代码消重/配置外置”工作流。
