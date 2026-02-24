---
description: 动态工具统一包迁移 纯工具层（P5）+ 同级适配层实现（P6）
---

# 工具包统一工作流

> 前置条件：codex-isolation 工作流 P4 验收通过。

## 架构定位

```
internal/tools/        ← 纯工具层（schema + handler，仅依赖 Provider 接口）
internal/tooladapter/  ← 适配层（注册、dispatch、调用上下文、依赖注入）
apiserver/             ← 传输 + 生命周期薄编排层（事件解析、可观测性、结果回传）
```

## 前置检查

```bash
pwd && git rev-parse --is-inside-work-tree
go build ./...
go test ./internal/apiserver/... ./internal/lsp/... -count=1
```

## Agent 执行编排

**总计：4 个 Agent（串行执行）**

- `P5a Agent`：低风险迁移（LSP ext + LSP schema 提取 + ext registry 接口化）
- `P5b Agent`：高耦合迁移（code_run/resource/orchestration + helpers/Provider 注入）
- `P6 Agent`：`internal/tooladapter/` 落地（register/dispatch/context）与 apiserver 薄入口收敛
- `Acceptance Agent`：执行 P6 全量门禁（含 `go test ./...`、`-race`、schema 兼容性）并出具验收结论

执行纪律：

1. 任一时刻仅 1 个 Agent 写代码。
2. 每阶段完成后提交交接结果，再进入下一 Agent。
3. 若任一门禁失败，停止下游阶段，回到对应阶段修复。

---

## P5: 动态工具迁移

### P5 目标

将散落在 `apiserver` 中的动态工具 **schema 定义 + handler 实现** 迁移到纯工具包 `internal/tools/`。

### 范围边界

> [!IMPORTANT]
> **迁移到 `internal/tools/`（工具包）**：schema 定义 + handler 实现  
> **P5 阶段暂保留在 `apiserver`（待 P6 迁出）**：`registerDynamicTools()`、`handleDynamicToolCall()`、ext 注册轮转注入  
> **保留在 `apiserver`（生命周期/展示层）**：`SetupLSP()`、`skillsDirectory()`、`buildToolNotifyPayload()`  
> **保留在 `apiserver`（使用层）**：`methods_turn.go` 中的 `collectDynamicToolNames`、`prependLSPAvailabilityWarning` 等
> **强制约束**：全部工具 handler 的外部依赖访问必须通过适配接口（Provider），不得直连底层实现。

### 迁移到 `internal/tools/`

| 工具类别 | 源文件 | 目标文件 | Server 依赖深度 |
|---|---|---|---|
| LSP ext 扩展 | 4 个 `*_ext.go` 中 schema + handler | `internal/tools/lsp_ext_*.go` | 低（仅 `lspTools`） |
| LSP 基础 | `internal/lsp/tool_handlers*.go` 中 schema（P2 已迁入） | `internal/tools/lsp_tools.go` | 低 |
| 编排 | `orchestration_tools.go` + `orchestration_report.go` | `internal/tools/orchestration.go` + `internal/tools/orchestration_report.go` | 高 |
| 资源 | `resource_tools.go` | `internal/tools/resource.go` | 高 |
| 代码执行 | `code_run_tools.go` | `internal/tools/code_run.go` | 高 |
| 共享工具函数 | `protocol.go` 中 `toolError`/`toolJSON` | `internal/tools/helpers.go` | 无 |

### P5 阶段保留在 `apiserver`（P6 迁出 + 生命周期保留）

| 职责 | 所在文件 | 说明 |
|---|---|---|
| `registerDynamicTools()` | `server_dynamic_tools.go` | P5 暂存，P6 迁移到 `internal/tooladapter/registry.go` |
| `handleDynamicToolCall()` | `server_dynamic_tools.go` | P5 暂存，P6 迁移到 `internal/tooladapter/dispatch.go` |
| `SetupLSP()` | `server_dynamic_tools.go` | 生命周期初始化；不属于工具适配迁移范围，可保留薄封装 |
| `skillsDirectory()` | `server_dynamic_tools.go` | Server 配置（生命周期/运行时环境） |
| `buildToolNotifyPayload()` | `server_dynamic_tools.go` | UI 可观测性（展示层） |
| ext 注册轮转注入 | `server_dynamic_tools_ext_registry.go` | P5 暂存，P6 迁移到 `internal/tooladapter/registry.go` |
| `allDynamicToolSchemas()` | `orchestration_tools.go` | 薄委派 → `tools.AllSchemas()` |
| `cancelAllCodeRuns()` | `code_run_tools.go` | Server 关闭链（生命周期管理），保留在 apiserver |
| `maybeAutoReportOrchestrationCompletion()` | `orchestration_report.go` | 被 `server_payload.go` 事件链调用，保留薄委派入口 |

### 分批策略

> [!WARNING]
> 工具 handler 对 `Server` 的耦合远超 P3（code_run 单文件 15+ Server 字段）。必须分两批。

**P5a（低风险）**:
1. 4 个 `*_ext.go` 中 schema + handler → `internal/tools/lsp_ext_*.go`
2. LSP 基础 schema（P2 已迁入 `internal/lsp/`）→ `internal/tools/lsp_tools.go`（从 `internal/lsp` 二次提取 schema 定义，`internal/lsp/ToolHandlers` 保留 handler 实现并实现 `LSPProvider` 接口）
3. apiserver 侧保留 ext registry + `registerDynamicTools()`，改为调用 `tools.LSPTools()` / `tools.LSPExtTools()`
4. ext registry 不得使用 `*Server` receiver，改为普通函数并显式注入 `LSPHandlerProvider` 与 `dynTools`，消除对 `*Server` 的直接依赖（否则 P6 迁移到 `tooladapter` 时会产生反向 import）

**P5b 前置（在迁移前完成）**:
1. 将 `toolError()`/`toolJSON()` 从 `protocol.go` 提取到 `internal/tools/helpers.go`（所有工具 handler 共用）
2. 定义全部 Provider 接口（含 `AgentRuntimeProvider`），见设计约束第 3 条

**P5b（高耦合）**:
1. `code_run_tools.go` → `internal/tools/code_run.go`
   - `cancelCodeRuns`/`setAgentWorkDir`/`clearAgentWorkDir`/`getAgentWorkDir` 被 orchestration 跨文件调用，必须通过 `AgentRuntimeProvider` 接口注入
   - `awaitCodeRunApproval`/`waitForFrontendDecision` 通过 `ApprovalProvider` 注入（审批逻辑属传输层）
2. `resource_tools.go` → `internal/tools/resource.go`
   - 3 处直接 `s.Notify()` 调用改为通过 `ResourceProvider.NotifyEvent()` 接口
3. `orchestration_tools.go` + `orchestration_report.go` → `internal/tools/orchestration.go` + `internal/tools/orchestration_report.go`
   - `allDynamicToolSchemas` 递归引用：`orchestrationLaunchAgent` 需全部 schema，改为 `SchemaProvider` 接口
   - `submitAgentPrompt` 依赖 `s.submitAgentMessage` hook（codex adapter 注入），改为 `OrchestrationProvider.SubmitPrompt()` 接口
   - `rememberOrchestrationReportRequest` 改为 `OrchestrationProvider.RememberReportRequest()` 接口

### 设计约束

1. **`internal/tools/` 是纯工具包**：仅含 schema + handler，不含 dispatch/注册/事件路由。不可 import `apiserver`。
2. **统一 Tool 结构体**：
   ```go
   type Tool struct {
       Schema  agentcore.DynamicTool
       Handler func(ctx ToolCallContext, args json.RawMessage) string
   }
   type ToolCallContext struct {
       AgentID   string
       CallID    string
       RequestID *int64           // code_run 需要 resolve callID（当 CallID 为空时 fallback 到 RequestID）
       Ctx       context.Context  // code_run 用于 cancel
   }
   ```
   > [!NOTE]
   > 当前 `dynTools` map 是 `func(json.RawMessage) string`，但 `code_run` 需 `(ctx, agentID, callID, args)`，`orchestration_send_message` 需 `(senderID, args)`。统一签名消除硬编码分支。`RequestID` 源自 `event.RequestID`，由当前 `resolveCodeRunCallID(call.CallID, event.RequestID)` 使用。P6 由 `tooladapter` 负责把运行时上下文装配并映射到该结构。

3. **多接口注入**：
   - `LSPProvider`: 暴露最小化 LSP 能力接口（禁止暴露 `*lsp.ToolHandlers` 等具体实现类型）
   - `CodeRunProvider`: `CodeRunner()`, `AuditLogStore()` 等
   - `ApprovalProvider`: `AwaitApproval(agentID, callID, mode, command string, isDangerous bool) bool` — 审批逻辑（WebSocket/Wails/fail-close）本质是传输层，通过此接口注入
   - `ResourceProvider`: `SharedFileStore()`, `DAGStore()`, `WorkspaceManager()`, `NotifyEvent(method string, params any)` — 工具 handler 不得直接调用 `s.Notify()`
   - `OrchestrationProvider`: `Manager()`, `SubmitPrompt(agentID, prompt string, images, files []string) error`, `RememberReportRequest(senderID, workerID string)`
   - `AgentRuntimeProvider`: `CancelCodeRuns(agentID string) int`, `SetAgentWorkDir(agentID, cwd string)`, `ClearAgentWorkDir(agentID string)`, `GetAgentWorkDir(agentID string) string` — 跨工具共享状态（code_run 定义、orchestration 使用），必须统一注入
   - `SchemaProvider`: `AllSchemas() []agentcore.DynamicTool` — 解决 `orchestrationLaunchAgent` 对 `allDynamicToolSchemas` 的递归引用
4. **P5 后 apiserver 仍可暂持适配入口，但不得新增工具逻辑**：
   - `registerDynamicTools()` 仅做到 `tools.AllTools(providers)` 的装配
   - `handleDynamicToolCall()` 仅保留最小路由逻辑，详细适配逻辑放到 P6
   - `allDynamicToolSchemas()` 薄委派调用 `tools.AllSchemas()`
5. JSON schema 不变。
6. **全部工具走适配层**：`internal/tools` 的 handler 只能通过 `*Provider` 接口访问能力，不得直接访问 `proc.Client`、`lsp.Manager`、store/workspace/runner 具体实现。
7. **Schema 兼容性必须可执行校验**：
   - 新增 `internal/tools/schema_compat_test.go`，固定测试名 `TestDynamicToolSchemasStable`
   - 新增 golden 文件 `internal/tools/testdata/tool_schemas.golden.json`
   - 仅允许在显式设置 `UPDATE_TOOL_SCHEMAS_GOLDEN=1` 时更新 golden

### P5 验证

```bash
go build ./...
go test ./internal/tools/... ./internal/apiserver/... ./internal/lsp/... -count=1
go vet ./...
# Schema 兼容性门禁（JSON schema 不变）
go test ./internal/tools -run TestDynamicToolSchemasStable -count=1
# 确认 apiserver 不再承载工具 schema/handler（适配层迁移到 P6）
if rg -n "^func \(s \*Server\) build[A-Za-z0-9_]*Tools\b" internal/apiserver --glob '!**/*_test.go'; then
  echo "FAIL: apiserver still contains tool build functions"; exit 1
fi
# 结构性检查：apiserver 不应继续定义动态工具 schema
if rg -n "InputSchema\s*:" internal/apiserver --glob '!**/*_test.go'; then
  echo "FAIL: apiserver still defines DynamicTool schemas"; exit 1
fi
# 确认 tools handler 未直连 Server 具体实现
if rg -n "s\\.(mgr|lsp|fileStore|cmdStore|dagStore|promptStore|workspaceMgr|codeRunner|auditLogStore|approvalInFlight|uiRuntime)" internal/tools/; then
  echo "FAIL: tools package has direct Server field access"; exit 1
fi
# 确认 tools handler 未直接调用 s.Notify()
if rg -n 's\.Notify\(' internal/tools/; then
  echo "FAIL: tools package directly calls s.Notify()"; exit 1
fi
# 确认无反向依赖
if rg '"github.com/multi-agent/go-agent-v2/internal/apiserver"' internal/tools/; then
  echo "FAIL: tools package imports apiserver"; exit 1
fi
# 确认 ext registry 签名不再依赖 *Server
if rg -n 'func\s*\(\s*(\w+\s+)?\*Server\s*\)' internal/apiserver/server_dynamic_tools_ext_registry.go; then
  echo "FAIL: ext registry still uses func(*Server) signature"; exit 1
fi
# 确认 toolError/toolJSON 已迁入 tools 包
if rg -n '^func toolError\b|^func toolJSON\b' internal/apiserver/protocol.go; then
  echo "FAIL: toolError/toolJSON still in apiserver/protocol.go"; exit 1
fi
```

---

## P6: 适配层实现（同级包）

### P6 目标

将 P5 暂留在 apiserver 的动态工具适配逻辑迁移到与 `internal/tools/` 同级的新包 `internal/tooladapter/`，实现可定位、可复用的统一适配层。

### P6 迁移清单

| 迁移对象 | 源文件 | 目标文件 | 说明 |
|---|---|---|---|
| 工具注册入口 | `server_dynamic_tools.go:registerDynamicTools` | `internal/tooladapter/registry.go` | 统一从 `tools.AllTools()` 装配 runtime handlers |
| dispatch 路由 | `handleDynamicToolCall` L357-386 中 handler 路由 + 可观测计数 | `internal/tooladapter/dispatch.go` | 仅 handler 路由 + `ToolCallContext` 装配 |
| ext 注册机制 | `server_dynamic_tools_ext_registry.go` | `internal/tooladapter/registry.go` | ext provider 统一在 tooladapter 汇总（签名已在 P5a 改为接口化） |
| call 上下文装配逻辑 | apiserver 零散字段 | `internal/tooladapter/context.go` | 统一装配运行时上下文，并映射到 `tools.ToolCallContext` |
| apiserver 入口薄委派 | `server_dynamic_tools.go` | 保留为传输层入口 | 仅做：信封解析、心跳保活、调用 `tooladapter.Dispatch`、UI 通知广播、结果回传 |
| `lspTools` 字段接口化 | `server.go:74` `lspTools *lsp.ToolHandlers` | 改为 `lspTools tools.LSPProvider` | P5a 审查遗留：消除对 `*lsp.ToolHandlers` 具体类型的暴露，需同步将 `AvailabilitySummary()`/`DiagnosticsQuery()` 纳入 `LSPProvider` 接口或通过独立接口注入 |
| code_run 薄委派 | `code_run_tools.go` 中 `codeRunToolset()`/`codeRunToolSchemas()`/`codeRunWithAgent()`/`codeRunTestWithAgent()` | 删除薄委派，由 `tooladapter.Dispatch` 统一路由 | P5b 遗留：`handleDynamicToolCall` 中 `code_run`/`code_run_test` 硬编码分支改为走 `ToolCallContext` 统一签名 |
| code_run 运行时状态 | `code_run_tools.go` 中 `registerCodeRunCancel`/`unregisterCodeRunCancel`/`cancelCodeRuns`/`cancelAllCodeRuns` + workDir 管理 | 保留在 apiserver（生命周期管理）| Server 实现 `AgentRuntimeProvider` 接口，方法本身不迁移 |
| code_run 审批 | `code_run_tools.go` 中 `awaitCodeRunApproval`/`waitForFrontendDecision` | 保留在 apiserver（传输层） | Server 实现 `ApprovalProvider` 接口，审批逻辑依赖 WebSocket/Wails |
| resource 薄委派 | `resource_tools.go` 中 `resourceToolset()`/`resourceToolSchemas()`/`runResourceTool()` + 14 个 handler 转发 | 删除薄委派，由 `tooladapter.Dispatch` 统一路由 | P5b 遗留：handler 转发函数全部消除 |
| resource Provider 实现 | `resource_tools.go` 中 `DAGStore()`/`CommandCardStore()`/`PromptTemplateStore()`/`SharedFileStore()`/`WorkspaceManager()`/`NotifyEvent()` | 保留在 apiserver | Server 实现 `ResourceProvider` 接口，方法本身不迁移 |
| orchestration 薄委派 | `orchestration_tools.go` 中 `orchestrationToolset()`/`orchestrationToolSchemas()`/`runOrchestrationTool()` + 5 个 handler 转发 | 删除薄委派，由 `tooladapter.Dispatch` 统一路由 | P5b 遗留：handler 转发函数全部消除 |
| `allDynamicToolSchemas` 聚合 | `orchestration_tools.go` 中 `allDynamicToolSchemas()`/`AllSchemas()` | `internal/tooladapter/registry.go` 统一提供 `AllSchemas()` | 当前实现聚合各 `*ToolSchemas()`；P6 改为 `tooladapter` 统一汇总 `tools.*Tools()` 的 schema |
| orchestration Provider 实现 | `orchestration_tools.go` 中 `NextThreadSeq()` + `server_payload.go` 中 `SubmitPrompt()`/`RememberReportRequest()` | 保留在 apiserver | Server 实现 `OrchestrationProvider` + `SchemaProvider` 接口 |

> [!IMPORTANT]
> `handleDynamicToolCall` **必须拆分**，不可整体迁移：
> - **保留在 apiserver（传输层）**：心跳保活（L280-302）、proc 查找 + 事件信封解析（L304-339）、UI 通知广播（L411-417）、结果回传（L419-422）
> - **迁移到 tooladapter（适配层）**：可观测性计数（L341-353）、handler dispatch 路由（L357-386）、`ToolCallContext` 装配
> - **禁止**：将心跳 / 事件解析 / 结果回传带入 `tooladapter`

### P6 设计约束

1. `internal/tooladapter/` 与 `internal/tools/` 同级，二者均不可 import `apiserver`。
2. `internal/tools` 保持纯工具层：schema + handler；适配逻辑全部收敛到 `internal/tooladapter`。
3. `apiserver` 仅保留传输 + 生命周期薄编排职责：事件解析、心跳/可观测性、回传；不再实现工具注册与 dispatch 细节。
4. `tooladapter` 对外暴露统一入口：
   - `Register(registry RuntimeRegistry, deps Providers)`
   - `Dispatch(call DynamicToolCall, deps Providers) (string, error)`
5. 所有工具调用依赖访问统一经 Provider 接口，禁止出现底层实现直连。
6. 工具 JSON schema 与名称保持不变。

### P6 验证

```bash
go build ./...
go test ./internal/tooladapter/... ./internal/tools/... ./internal/apiserver/... -count=1
go vet ./...
# 生产门禁：全量回归 + 竞态检查
go test ./... -count=1
go test -race ./... -count=1
# Schema 兼容性门禁（JSON schema 不变）
go test ./internal/tools -run TestDynamicToolSchemasStable -count=1
# apiserver 不再持有工具 build 函数
if rg -n "^func \(s \*Server\) build[A-Za-z0-9_]*Tools\b" internal/apiserver --glob '!**/*_test.go'; then
  echo "FAIL: apiserver still contains tool build functions"; exit 1
fi
# 结构性检查：apiserver 不应继续定义动态工具 schema
if rg -n "InputSchema\s*:" internal/apiserver --glob '!**/*_test.go'; then
  echo "FAIL: apiserver still defines DynamicTool schemas"; exit 1
fi
# 结构性检查：apiserver 不应保留任何 dynTools 写入
if rg -n "dynTools\\[[^]]+\\]\\s*=" internal/apiserver --glob '!**/*_test.go'; then
  echo "FAIL: apiserver still writes to dynTools map"; exit 1
fi
# 若保留薄入口函数，必须定义在 server_dynamic_tools.go 且委派到 tooladapter（自动门禁）
bad=0
if rg -n "^func \\(s \\*Server\\) (registerDynamicTools|handleDynamicToolCall)\\b" internal/apiserver --glob '!**/*_test.go' >/dev/null; then
  rg -n "^func \\(s \\*Server\\) (registerDynamicTools|handleDynamicToolCall)\\b" internal/apiserver/server_dynamic_tools.go >/dev/null || { echo "FAIL: thin entrypoints defined outside server_dynamic_tools.go"; bad=1; }
  rg -n "tooladapter\\.(Register|Dispatch)\\(" internal/apiserver/server_dynamic_tools.go >/dev/null || { echo "FAIL: thin entrypoints do not delegate to tooladapter"; bad=1; }
fi
# 无论是否保留薄入口函数，apiserver 必须存在对 tooladapter 的实际调用
rg -n "tooladapter\\.(Register|Dispatch)\\(" internal/apiserver --glob '!**/*_test.go' >/dev/null || { echo "FAIL: no tooladapter integration found in apiserver"; bad=1; }
test "$bad" -eq 0 || exit 1
# tooladapter 不反向依赖 apiserver
if rg '"github.com/multi-agent/go-agent-v2/internal/apiserver"' internal/tooladapter/; then
  echo "FAIL: tooladapter imports apiserver"; exit 1
fi
```

---

## 通过标准

- `internal/tools/` 包含全部工具（schema + handler + helpers）
- `internal/tooladapter/` 承载全部工具适配层实现（注册 + dispatch + provider 注入）
- `apiserver` 仅保留传输 + 生命周期薄编排与可观测性，不承载工具 schema/handler/适配实现
- `internal/tools` 与 `internal/tooladapter` 均不 import `apiserver`
- 全部工具 handler 仅经 Provider 接口访问外部依赖（无底层实现直连、无直接 `s.Notify()`）
- `go test ./... -count=1` 与 `go test -race ./... -count=1` 全绿
- `go test ./internal/tools -run TestDynamicToolSchemasStable -count=1` 全绿
- JSON schema 不变（存量工具，golden 校验通过）
