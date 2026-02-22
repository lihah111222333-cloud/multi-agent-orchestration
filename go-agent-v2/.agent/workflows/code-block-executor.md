---
description: 实现代码块执行动态工具 (code_run + code_run_test)
---

# Code Block Executor 实现工作流

// turbo-all

## 前置条件

- 项目根: `go-agent-v2/`
- `go build ./...` 通过
- `go test ./internal/executor/ -v` 通过
- `go test ./internal/apiserver/ -run 'DynamicTool|Approval' -count=1` 通过

## Step 1: CodeRunner 引擎

文件: `internal/executor/code_runner.go` (~320 行)

### 1.1 类型

- `CodeRunner` (workDir, hasNode, hasTsx, sem chan struct{} cap=3, tempRoot)
- `RunRequest` (Language, Code, Command, Mode, AutoWrap, TestFunc, TestPkg, WorkDir, Timeout)
- `RunResult` (Success, Output, ExitCode, Duration, Language, Mode, Truncated)

Timeout 默认值: `Run()` 入口若 `Timeout <= 0` 则设为 30s，避免零值导致 `context.WithTimeout` 立即超时。

### 1.2 方法

- `NewCodeRunner(workDir)` — 探测 node/tsx + 初始化实例级 tempRoot + 信号量
- `Run(ctx, req)` — 信号量限流 → 按 Mode 分发 (run/test/project_cmd)
- `runGo` — MkdirTemp → main.go → `go run` (cmd.Dir=workDir)
- `runGoTest` — `go test -v -run ^TestFunc$ TestPkg`
- `runJS` / `runTS` — MkdirTemp → node / npx tsx
- `runProjectCmd` — `sh -c Command` (cmd.Dir = WorkDir 或 workDir；唯一强制审批 mode)
- `wrapGoMain(code)` — 自动补 main + 仅导入实际引用包 (避免 unused import)
- `execCommand(ctx, name, args, dir)` — 进程组管理 + 聚合输出总量限制 512KB (`stdout+stderr` 总和，不是各自 512KB)
- `cleanup(path)` — os.RemoveAll

约束:
- 禁止全局删除 `/tmp/code_exec_*`；仅清理当前实例创建目录，必要时只清理过期目录 (如 >24h)。
- 审批范围明确: 仅 `project_cmd` 强制审批；`run/test` 默认免审批（仍受隔离临时目录、超时、输出上限约束）。
- `WorkDir` 路径校验: 若 `req.WorkDir != ""` 必须通过 `pathWithinRoot` 风格校验 (`filepath.Abs` + `filepath.Rel` 判断不以 `..` 开头)，禁止仅用 `HasPrefix`。
- `project_cmd` 可选检测 `dangerousPatterns` (复用 `command_card.go`)，在审批 payload 中标注 `is_dangerous` 辅助前端展示风险等级。

### 1.3 进程组管理

```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
// 超时: syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
```

### 1.4 验证

```bash
go build ./internal/executor/
```

## Step 2: 测试

### 2.1 executor 单测

文件: `internal/executor/code_runner_test.go` (~220 行)

- `TestCodeRunner_GoRun` — auto_wrap
- `TestCodeRunner_GoRunWithImport` — 项目包 import
- `TestCodeRunner_GoRunTimeout` — 超时 + 进程组清理
- `TestCodeRunner_GoTest` — go test -run
- `TestCodeRunner_JSRun` — console.log
- `TestCodeRunner_ProjectCmd` — echo hello
- `TestCodeRunner_ProjectCmd_CustomWorkDir` — 自定义目录
- `TestCodeRunner_OutputTruncation` — 512KB 截断
- `TestCodeRunner_ConcurrencyLimit` — 信号量
- `TestCodeRunner_AutoWrap_NoUnusedImports` — 自动包裹不引入未使用 import
- `TestCodeRunner_TempCleanup_InstanceScoped` — 仅清理实例目录，不误删其他目录
- `TestCodeRunner_WorkDir_PathTraversalBlocked` — 阻断 `../../` 与前缀绕过 (`/root/work2` 不能落入 `/root/work`)
- `TestCodeRunner_OutputLimit_AggregatedStdoutStderr` — `stdout+stderr` 合计按 512KB 截断

```bash
go test ./internal/executor/ -run TestCodeRunner -v
```

### 2.2 apiserver 接入测试

文件: `internal/apiserver/code_run_tools_test.go` (~180 行)

- `TestBuildAllDynamicTools_IncludesCodeRunTools` — 验证注入链路包含 `code_run` / `code_run_test`
- `TestHandleDynamicToolCall_CodeRun_PassesAgentID` — 验证 dispatch 传递 agentID
- `TestHandleDynamicToolCall_CodeRun_PassesCallID` — 验证 dispatch 传递 callID
- `TestHandleDynamicToolCall_CodeRun_CallIDFallback` — callID 为空时回退 requestID/nonce，去重键仍唯一
- `TestCodeRunApproval_FailClose` — 无前端/超时/拒绝时不执行命令
- `TestCodeRunApproval_DedupKeyByCallID` — 同一 agent 并发请求不互相去重吞掉
- `TestCodeRunAudit_WritesAuditEvent` — 审计字段与裁剪策略正确

```bash
go test ./internal/apiserver/ -run 'CodeRun|DynamicTool' -count=1 -v
```

## Step 3: 工具 + 审批 + 审计

文件: `internal/apiserver/code_run_tools.go` (~250 行)

### 3.1 buildCodeRunTools()

返回 `[]codex.DynamicTool`。不可用语言不注册。

### 3.2 handler (需要 agentID)

```go
func (s *Server) codeRunWithAgent(agentID, callID string, args json.RawMessage) string
func (s *Server) codeRunTestWithAgent(agentID, callID string, args json.RawMessage) string
```

流程: 审批 → 执行 → 审计

### 3.3 审批

```go
func (s *Server) awaitUserApproval(agentID, method, approvalKey string, payload map[string]any) bool
```

从 `handleApprovalRequest` 复用等待语义，但不回传 codex `Submit("yes/no")`，仅返回 bool。
必须保留:
- 协议 method 固定: `item/commandExecution/requestApproval`（不拼接 callID，避免破坏前端/桥接协议兼容）
- 去重键独立: `approvalKey = agentID + ":" + method + ":" + approvalID`，避免同一 agent 并发 code_run 被错误去重
- 双通道: `SendRequestToAll` (WebSocket) → 降级 `AllocPendingRequest` (Wails)
- fail-close: 无前端 / 超时 / 错误统一返回 `false`
- 超时语义与现有审批保持一致 (默认 5 分钟，后续可配置)
- **不需要自带心跳** — 外层 `handleDynamicToolCall` 已有心跳，避免冗余 `touchTrackedTurnLastEvent`

设计备注:
- `approvalID` 生成规则: `callID` 非空优先；为空则用 `event.RequestID`；仍为空则生成进程内 nonce（如时间戳+原子计数）。
- 如未来在非 `handleDynamicToolCall` 场景复用 `awaitUserApproval`，由调用方显式承担保活策略。

### 3.4 审计

写入 `audit_events` (不是 `ai_log`，`ai_log` 为 `system_logs` 派生查询)。

建议字段:
- `event_type=code_run`
- `action=run|test|project_cmd`
- `result=success|failed|denied|timeout`
- `actor=agentID`
- `target=language/mode`
- `detail` + `extra` 包含 `exit_code` / `duration_ms` / `work_dir` / `output_truncated`

安全约束:
- `code` / `command` / `output` 必须裁剪 (如 4KB) 后写入 `extra`，避免超大与敏感内容直存。

```bash
go build ./internal/apiserver/
```

## Step 4: 注册

### 4.1 `server_dynamic_tools.go`

`handleDynamicToolCall` dispatch 加特殊分支 (传 agentID，同 orchestration_send_message):

```go
} else if call.Tool == "code_run" {
    result = s.codeRunWithAgent(agentID, call.CallID, call.Arguments)
} else if call.Tool == "code_run_test" {
    result = s.codeRunTestWithAgent(agentID, call.CallID, call.Arguments)
```

`registerDynamicTools()` 中加注释说明跳过原因:
```go
// code_run / code_run_test: 需要 agentID + callID, 在 handleDynamicToolCall 中硬编码分支。
```

### 4.2 `orchestration_tools.go`

`buildAllDynamicTools()` 定义在 `orchestration_tools.go`，在此处追加:

```go
tools = append(tools, s.buildCodeRunTools()...)
```

### 4.3 `server.go`

Server 加 `codeRunner *executor.CodeRunner`，`New()` 初始化。

## Step 5: 全量验证

```bash
go build ./...
go test ./internal/executor/ -v
go test ./internal/apiserver/ -v -count=1
```
