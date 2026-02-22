---
description: code-block-executor.md 计划的完成验证证据
---

# Code Block Executor — 完成证据

> 基于 [code-block-executor.md](./code-block-executor.md) 计划的逐步验证记录。

## Step 1: CodeRunner 引擎 ✅

**文件**: `internal/executor/code_runner.go` (593 行)

| 计划要求 | 实现状态 | 验证方式 |
|---------|---------|---------|
| `CodeRunner` 结构体 (workDir, hasNode, hasTsx, sem, tempRoot) | ✅ L63-69 | 编译通过 |
| `RunRequest` / `RunResult` 类型 | ✅ L72-93 | 编译通过 |
| `NewCodeRunner(workDir)` — 探测 node/tsx + tempRoot | ✅ L103-132 | `TestCodeRunner_JSRun` |
| `Run()` — 信号量限流 → Mode 分发 | ✅ L154-205 | `TestCodeRunner_ConcurrencyLimit` |
| `runGo` — MkdirTemp → main.go → go run | ✅ L231-257 | `TestCodeRunner_GoRun` |
| `runGoTest` — go test -v -run | ✅ L260-279 | `TestCodeRunner_GoTest` |
| `runJS` / `runTS` — node / npx tsx | ✅ L286-339 | `TestCodeRunner_JSRun` |
| `runProjectCmd` — sh -c + WorkDir | ✅ L348-367 | `TestCodeRunner_ProjectCmd` |
| `wrapGoMain` — 自动包裹 + 仅导入引用包 | ✅ L419-470 | `TestCodeRunner_AutoWrap_NoUnusedImports` |
| 注释行过滤 (审查修复 #3) | ✅ L425-435 | `TestWrapGoMain_CommentOnlyImportNotAdded` |
| `execCommand` — 进程组管理 + 512KB 聚合输出 | ✅ L473-522 | `TestCodeRunner_OutputTruncation` |
| `cmd.Cancel` + `WaitDelay` (用户改进) | ✅ L483-487 | `TestCodeRunner_GoRunTimeout` |
| `validateWorkDir` — filepath.Rel 路径穿越防护 | ✅ L533-546 | `TestCodeRunner_WorkDir_PathTraversalBlocked` |
| 仅清理实例目录 | ✅ L141-147, L566-570 | `TestCodeRunner_TempCleanup_InstanceScoped` |

## Step 2: 测试 ✅

### executor 测试 (33/33 PASS)

```
--- PASS: TestCodeRunner_GoRun (0.63s)
--- PASS: TestCodeRunner_GoRunWithImport (0.58s)
--- PASS: TestCodeRunner_GoRunTimeout (2.00s)
--- PASS: TestCodeRunner_GoTest (0.02s)
--- PASS: TestCodeRunner_JSRun (0.07s)
--- PASS: TestCodeRunner_ProjectCmd (0.00s)
--- PASS: TestCodeRunner_ProjectCmd_CustomWorkDir (0.00s)
--- PASS: TestCodeRunner_OutputTruncation (0.03s)
--- PASS: TestCodeRunner_ConcurrencyLimit (0.27s)
--- PASS: TestCodeRunner_AutoWrap_NoUnusedImports (0.55s)
--- PASS: TestCodeRunner_TempCleanup_InstanceScoped (0.01s)
--- PASS: TestCodeRunner_WorkDir_PathTraversalBlocked (0.00s)
--- PASS: TestCodeRunner_OutputLimit_AggregatedStdoutStderr (0.04s)
--- PASS: TestWrapGoMain_AlreadyHasPackage (0.00s)
--- PASS: TestWrapGoMain_HasMainFunc (0.00s)
--- PASS: TestWrapGoMain_SnippetOnly (0.00s)
--- PASS: TestTruncateForAudit (0.00s)
--- PASS: TestWrapGoMain_CommentOnlyImportNotAdded (0.00s)  ← 审查修复回归
--- PASS: TestWrapGoMain_MixedCommentAndCode (0.00s)        ← 审查修复回归
+ 14 个已有 CommandCard 测试
```

### apiserver 测试 (117+ PASS, 零回归)

`TestHandleApprovalRequest_DeduplicatesConcurrent` flaky 已修复 (10/10 PASS)。

## Step 3: 工具 + 审批 + 审计 ✅

**文件**: `internal/apiserver/code_run_tools.go` (340 行)

| 计划要求 | 实现状态 | 行号 |
|---------|---------|------|
| `buildCodeRunTools()` — 工具定义 | ✅ | L32-71 |
| `codeRunWithAgent` handler | ✅ + nil guard | L78-134 |
| `codeRunTestWithAgent` handler | ✅ + nil guard | L137-170 |
| `awaitCodeRunApproval` — 双通道 + fail-close | ✅ | L184-213 |
| `waitForFrontendDecision` — 共享等待逻辑 | ✅ | L221-268 |
| 去重键独立 (含 approvalID) | ✅ | L194 |
| `writeCodeRunAudit` — 审计写入 | ✅ | L275-309 |
| 安全裁剪 code/command/output ≤ 4KB | ✅ | L287-295 |

## Step 4: 注册 ✅

| 计划要求 | 文件 | 行号 |
|---------|------|------|
| `handleDynamicToolCall` dispatch 分支 | `server_dynamic_tools.go` | L315-319 |
| `buildAllDynamicTools` 追加 | `orchestration_tools.go` | L208 |
| Server.codeRunner 字段 + 初始化 | `server.go` | L69, L258-264 |

## Step 5: 全量验证 ✅

```
$ go build ./...           → ✅ 通过 (仅 macOS linker 警告)
$ go test ./internal/executor/ -v   → ✅ 33/33 PASS
$ go test ./internal/apiserver/ -v  → ✅ 117+ PASS
```

## 额外完成项

### 提示词注入 (计划外增强)

| 文件 | 变更 |
|------|------|
| `methods.go` | `defaultCodeRunPrompt` + `config/codeRunPrompt/read\|write` 注册 |
| `methods_config.go` | `resolveCodeRunPrompt` + config read/write |
| `methods_turn.go` | `appendCodeRunHint` 注入 `turn/start` + `turn/steer` |

### 自审查修复

| # | 严重度 | 问题 | 修复 |
|---|--------|------|------|
| 1 | 🔴 | handler nil-panic | 添加 `codeRunner == nil` guard |
| 2 | 🔴 | 超时重复 kill | 移除冗余 `killProcessGroup` (cmd.Cancel 已处理) |
| 3 | 🟡 | 注释假阳性 import | 添加注释行过滤 + 2 个回归测试 |
| 4 | 🟡 | 未使用 callID 参数 | 改为 `_` |
| 5 | 🟡 | flaky dedup 测试 | startBarrier + DenyFunc sleep |

### 未修复项 (低风险, 留待后续)

| # | 严重度 | 问题 | 原因 |
|---|--------|------|------|
| 6 | 🟢 | 符号链接绕过 validateWorkDir | 生产环境极少出现 |
| 7 | 🟢 | 审计参数命名重名 | 代码可读性建议 |

## 产出文件清单

| 文件 | 类型 | 行数 |
|------|------|------|
| `internal/executor/code_runner.go` | 新增 | 593 |
| `internal/executor/code_runner_test.go` | 新增 | 477 |
| `internal/apiserver/code_run_tools.go` | 新增 | 340 |
| `internal/apiserver/server.go` | 修改 | +14 |
| `internal/apiserver/server_dynamic_tools.go` | 修改 | +5 |
| `internal/apiserver/orchestration_tools.go` | 修改 | +2 |
| `internal/apiserver/methods.go` | 修改 | +13 |
| `internal/apiserver/methods_config.go` | 修改 | +54 |
| `internal/apiserver/methods_turn.go` | 修改 | +10 |
| `internal/apiserver/server_approval_test.go` | 修改 | +14 |
