---
description: P1 tools Provider 接口抽象 — 切断对 executor/runner/service/store 的直接依赖
---

# P1: tools Provider 接口抽象

> **⚡ 可并行** — 与 P1.5、P2 同时执行

## 前置条件

- [ ] P0 准备阶段已完成
- [ ] `types_sdk.go` 和行为接口已定义

## 任务范围

### 需要修改的文件

- `internal/tools/providers.go` — 旧接口返回类型改为 P0 定义的行为接口
- `internal/tools/code_run.go` — 删除 `import "internal/executor"` 和 `import "internal/store"`，改用接口调用
- `internal/tools/resource.go` — 删除 `import "internal/service"` 和 `import "internal/store"`，改用接口调用
- `internal/tools/orchestration.go` — 删除 `import "internal/runner"`，改用 `AgentLauncher` 接口
- `internal/tools/lsp_tools.go` — 检查是否有耦合（当前仅依赖 agentcore，可能无需改）
- `internal/tools/lsp_schema_builder.go` — 同上
- `internal/tooladapter/context.go` — 实现适配器，让具体类型满足新接口
- `internal/tooladapter/registry.go` — 更新注册逻辑
- `internal/apiserver/tool_providers.go` — 调整 Provider 实现以匹配新接口签名

## 接口映射基线（必须）

行为接口必须可一一映射到现有实现，禁止引入底层不存在的方法名。

| 现有实现 | 行为接口建议 | 适配方式 |
|------|------|------|
| `(*executor.CodeRunner).Run(ctx, executor.RunRequest)` | `CodeExecRunner.Run(ctx, CodeRunRequest)` | `tooladapter` 中做请求结构转换 |
| `(*store.AuditLogStore).Append(ctx, *store.AuditEvent)` | `AuditLogger.Append(ctx, *AuditEvent)` | `tooladapter` 中做事件结构转换 |
| `(*store.TaskDAGStore)` | `DAGManager` | 方法透传 |
| `(*service.WorkspaceManager)` | `WorkspaceOps` | 方法透传 |
| `(*runner.AgentManager)` | `AgentLauncher` | 方法透传 |

### 禁止触碰的文件 ⚠️

- `internal/lsp/*` (P2 负责)
- `internal/apiserver/server_dynamic_tools_diff.go` (P1.5 负责)
- `internal/apiserver/server_dynamic_tools.go` (P1.5 负责)
- `internal/difftracker/*` (P1.5 负责)
- `internal/codex/*` (不在本次范围)
- `internal/apiserver/codexadapter/*` (P3 负责)

## 执行步骤

// turbo-all

1. 修改 `providers.go` — 将 6 个 Provider 的返回类型从具体类型改为行为接口
   ```diff
    type CodeRunProvider interface {
   -    CodeRunner() *executor.CodeRunner
   -    AuditLogStore() *store.AuditLogStore
   +    CodeRunner() CodeExecRunner
   +    AuditLogger() AuditLogger
    }
   ```
   对 ResourceProvider、OrchestrationProvider 做同样改造

2. 修改 `code_run.go`
   - 将 `resolveCodeRunner` 返回类型从 `*executor.CodeRunner` 改为 `CodeExecRunner`
   - 将 `writeCodeRunAudit` 改用 `provider.AuditLogger().Append(...)`（或你在 P0 定义的等价接口）
   - 删除 `import "internal/executor"` 和 `import "internal/store"`

3. 修改 `resource.go`
   - 所有 `provider.DAGStore().*` → `provider.DAGManager().*`
   - 所有 `provider.WorkspaceManager().*` → `provider.WorkspaceOps().*`
   - 删除 `import "internal/service"` 和 `import "internal/store"`

4. 修改 `orchestration.go`
   - `provider.Manager()` → `provider.AgentLauncher()`
   - 删除 `import "internal/runner"`

5. 修改 `tooladapter/context.go` 和 `registry.go`
   - 创建 wrapper 适配器让 `executor.CodeRunner` 满足 `tools.CodeExecRunner`
   - 让 `*store.TaskDAGStore`、`*service.WorkspaceManager`、`*runner.AgentManager` 透传满足行为接口
   - import 具体类型仅在 tooladapter

6. 编译验证
   ```bash
   go build ./internal/tools/...
   go test ./internal/tools/...
   go build ./internal/tooladapter/...
   go test ./internal/tooladapter/...
   go build ./...
   ```

7. 确认 `tools/` 的 import 不再包含 `executor`/`runner`/`service`/`store`
   ```bash
   rg -n '"github.com/multi-agent/go-agent-v2/internal/(executor|runner|service|store)"' internal/tools/*.go
   # 应返回空
   ```

## 完成标准

- [ ] `internal/tools/` 不再 import `internal/executor`, `internal/runner`, `internal/service`, `internal/store`
- [ ] `internal/tooladapter/` 包含适配器实现
- [ ] `go build ./...` 通过
- [ ] `go test ./internal/tools/... ./internal/tooladapter/...` 通过
- [ ] 不修改其他 Agent 负责的文件

## 验证命令

```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2
go build ./internal/tools/...
go test ./internal/tools/...
go build ./internal/tooladapter/...
go test ./internal/tooladapter/...
go build ./...
go test ./...
```
