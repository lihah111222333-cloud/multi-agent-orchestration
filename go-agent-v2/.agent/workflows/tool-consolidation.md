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
| LSP 基础 | `server_dynamic_tools.go` 中 `buildLSPDynamicTools()` | `internal/tools/lsp_tools.go` | 低 |
| 编排 | `orchestration_tools.go` 中 `buildOrchestrationTools()` + handler | `internal/tools/orchestration.go` | 高 |
| 资源 | `resource_tools.go` | `internal/tools/resource.go` | 高 |
| 代码执行 | `code_run_tools.go` | `internal/tools/code_run.go` | 高 |

### P5 阶段保留在 `apiserver`（P6 迁出 + 生命周期保留）

| 职责 | 所在文件 | 说明 |
|---|---|---|
| `registerDynamicTools()` | `server_dynamic_tools.go` | P5 暂存，P6 迁移到 `internal/tooladapter/registry.go` |
| `handleDynamicToolCall()` | `server_dynamic_tools.go` | P5 暂存，P6 迁移到 `internal/tooladapter/dispatch.go` |
| `SetupLSP()` | `server_dynamic_tools.go` | 生命周期初始化；不属于工具适配迁移范围，可保留薄封装 |
| `skillsDirectory()` | `server_dynamic_tools.go` | Server 配置（生命周期/运行时环境） |
| `buildToolNotifyPayload()` | `server_dynamic_tools.go` | UI 可观测性（展示层） |
| ext 注册轮转注入 | `server_dynamic_tools_ext_registry.go` | P5 暂存，P6 迁移到 `internal/tooladapter/registry.go` |
| `buildAllDynamicTools()` | `orchestration_tools.go` | 薄委派 → `tools.AllSchemas()` |

### 分批策略

> [!WARNING]
> 工具 handler 对 `Server` 的耦合远超 P3（code_run 单文件 15+ Server 字段）。必须分两批。

**P5a（低风险）**:
1. 4 个 `*_ext.go` 中 schema + handler → `internal/tools/lsp_ext_*.go`
2. `buildLSPDynamicTools()` schema → `internal/tools/lsp_tools.go`
3. apiserver 侧保留 ext registry + `registerDynamicTools()`，改为调用 `tools.LSPTools()` / `tools.LSPExtTools()`

**P5b（高耦合）**:
1. `code_run_tools.go` → `internal/tools/code_run.go`
2. `resource_tools.go` → `internal/tools/resource.go`
3. `orchestration_tools.go` → `internal/tools/orchestration.go`
   - `buildAllDynamicTools` 递归引用：`orchestrationLaunchAgent` 需全部 schema，改为 `SchemaProvider` 接口

### 设计约束

1. **`internal/tools/` 是纯工具包**：仅含 schema + handler，不含 dispatch/注册/事件路由。不可 import `apiserver`。
2. **统一 Tool 结构体**：
   ```go
   type Tool struct {
       Schema  agentcore.DynamicTool
       Handler func(ctx ToolCallContext, args json.RawMessage) string
   }
   type ToolCallContext struct {
       AgentID string
       CallID  string
       Ctx     context.Context // code_run 用于 cancel
   }
   ```
   > [!NOTE]
   > 当前 `dynTools` map 是 `func(json.RawMessage) string`，但 `code_run` 需 `(ctx, agentID, callID, args)`，`orchestration_send_message` 需 `(senderID, args)`。统一签名消除硬编码分支。P6 由 `tooladapter` 负责把运行时上下文装配并映射到该结构。

3. **多接口注入**：
   - `LSPProvider`: 暴露最小化 LSP 能力接口（禁止暴露 `*lsp.ToolHandlers` 等具体实现类型）
   - `CodeRunProvider`: `CodeRunner()`, `AllocPendingRequest()`, `AuditLogStore()` 等
   - `ResourceProvider`: `FileStore()`, `DAGStore()`, `WorkspaceMgr()` 等
   - `OrchestrationProvider`: `Manager()`, `CancelCodeRuns()` 等
4. **P5 后 apiserver 仍可暂持适配入口，但不得新增工具逻辑**：
   - `registerDynamicTools()` 仅做到 `tools.AllTools(providers)` 的装配
   - `handleDynamicToolCall()` 仅保留最小路由逻辑，详细适配逻辑放到 P6
   - `buildAllDynamicTools()` 薄委派调用 `tools.AllSchemas()`
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
rg -n "^func \\(s \\*Server\\) (buildLSPDynamicTools|buildOrchestrationTools|buildResourceTools|buildCodeRunTools)" internal/apiserver --glob '!**/*_test.go' && echo "FAIL" || echo "OK"
# 结构性检查：apiserver 不应继续定义动态工具 schema
rg -n "agentcore\\.DynamicTool\\{" internal/apiserver --glob '!**/*_test.go' && echo "FAIL" || echo "OK"
# 确认 tools handler 未直连 Server 具体实现
rg -n "s\\.(mgr|lsp|fileStore|cmdStore|dagStore|promptStore|workspaceMgr|codeRunner|auditLogStore|approvalInFlight|uiRuntime)" internal/tools/ && echo "FAIL" || echo "OK"
# 确认无反向依赖
rg '"github.com/multi-agent/go-agent-v2/internal/apiserver"' internal/tools/ && echo "FAIL" || echo "OK"
```

---

## P6: 适配层实现（同级包）

### P6 目标

将 P5 暂留在 apiserver 的动态工具适配逻辑迁移到与 `internal/tools/` 同级的新包 `internal/tooladapter/`，实现可定位、可复用的统一适配层。

### P6 迁移清单

| 迁移对象 | 源文件 | 目标文件 | 说明 |
|---|---|---|---|
| 工具注册入口 | `server_dynamic_tools.go:registerDynamicTools` | `internal/tooladapter/registry.go` | 统一从 `tools.AllTools()` 装配 runtime handlers |
| 调用分发入口 | `server_dynamic_tools.go:handleDynamicToolCall` | `internal/tooladapter/dispatch.go` | 统一 dispatch + 上下文注入 |
| ext 注册机制 | `server_dynamic_tools_ext_registry.go` | `internal/tooladapter/registry.go` | ext provider 统一在 tooladapter 汇总 |
| call 上下文装配逻辑 | apiserver 零散字段 | `internal/tooladapter/context.go` | 统一装配运行时上下文，并映射到 `tools.ToolCallContext` |
| apiserver 入口薄委派 | `server_dynamic_tools.go` | 可选保留（薄入口） | 仅解析事件并调用 `tooladapter.Dispatch` |

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
# apiserver 不再持有工具注册/dispatch 具体实现（允许薄委派入口存在）
rg -n "^func \\(s \\*Server\\) (buildLSPDynamicTools|buildOrchestrationTools|buildResourceTools|buildCodeRunTools)\\b" internal/apiserver --glob '!**/*_test.go' && echo "FAIL" || echo "OK"
# 结构性检查：apiserver 不应继续定义动态工具 schema
rg -n "agentcore\\.DynamicTool\\{" internal/apiserver --glob '!**/*_test.go' && echo "FAIL" || echo "OK"
# 结构性检查：apiserver 不应保留任何 dynTools 写入（包括匿名函数和命名函数绑定）
rg -n "dynTools\\[[^]]+\\]\\s*=" internal/apiserver --glob '!**/*_test.go' && echo "FAIL" || echo "OK"
# 若保留薄入口函数，必须定义在 server_dynamic_tools.go 且委派到 tooladapter（自动门禁）
bad=0
if rg -n "^func \\(s \\*Server\\) (registerDynamicTools|handleDynamicToolCall)\\b" internal/apiserver --glob '!**/*_test.go' >/dev/null; then
  rg -n "^func \\(s \\*Server\\) (registerDynamicTools|handleDynamicToolCall)\\b" internal/apiserver/server_dynamic_tools.go >/dev/null || { echo "FAIL: thin entrypoints defined outside server_dynamic_tools.go"; bad=1; }
  rg -n "tooladapter\\.(Register|Dispatch)\\(" internal/apiserver/server_dynamic_tools.go >/dev/null || { echo "FAIL: thin entrypoints do not delegate to tooladapter"; bad=1; }
fi
# 无论是否保留薄入口函数，apiserver 必须存在对 tooladapter 的实际调用
rg -n "tooladapter\\.(Register|Dispatch)\\(" internal/apiserver --glob '!**/*_test.go' >/dev/null || { echo "FAIL: no tooladapter integration found in apiserver"; bad=1; }
test "$bad" -eq 0 && echo "OK"
# tooladapter 不反向依赖 apiserver
rg '"github.com/multi-agent/go-agent-v2/internal/apiserver"' internal/tooladapter/ && echo "FAIL" || echo "OK"
```

---

## 通过标准

- `internal/tools/` 包含全部工具（schema + handler）
- `internal/tooladapter/` 承载全部工具适配层实现（注册 + dispatch + provider 注入）
- `apiserver` 仅保留传输 + 生命周期薄编排与可观测性，不承载工具 schema/handler/适配实现
- `internal/tools` 与 `internal/tooladapter` 均不 import `apiserver`
- 全部工具 handler 仅经 Provider 接口访问外部依赖（无底层实现直连）
- `go test ./... -count=1` 与 `go test -race ./... -count=1` 全绿
- `go test ./internal/tools -run TestDynamicToolSchemasStable -count=1` 全绿
- JSON schema 不变（存量工具，golden 校验通过）
