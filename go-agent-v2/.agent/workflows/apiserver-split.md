---
description: apiserver 职责拆分（可执行版）— Codex 专属逻辑沉入 codexadapter，Dashboard/Skills 业务独立包化
---

# apiserver 职责拆分工作流（可执行版）

> 前置条件：`tool-consolidation` 工作流 P6 已验收通过。

## 1. 目标与通过标准

目标是把 `internal/apiserver` 降级为“协议入口 + 路由注册 + 轻委派”，并把业务实现拆到独立包，且全程保持功能不变。

最终通过标准（全部满足才算完成）：

1. `apiserver` 不再承载 Thread/Turn/TurnTracker 业务实现。
2. `apiserver` 不再承载 Dashboard/Skills 业务实现。
3. `codexadapter`、`dashrpc`、`skills` 不反向 import `apiserver`。
4. JSON-RPC method 名称集合不变。
5. 动态工具 schema 不变（`TestDynamicToolSchemasStable` 通过）。
6. 全量测试通过，最终阶段 `-race` 通过。

## 2. 目标架构

```text
internal/apiserver/codexadapter/    # Codex CLI 专属逻辑（Thread/Turn/TurnTracker/Resume）
internal/apiserver/commonadapter/   # 跨 CLI 通用纯函数（Skill 匹配/名称规范化/文本拼接等）
internal/dashrpc/                   # [NEW] Dashboard JSON-RPC 业务（避免与 internal/dashboard HTTP 服务混淆）
internal/skills/                    # [NEW] 技能管理 JSON-RPC 业务
internal/apiserver/                 # 传输层 + 生命周期 + JSON-RPC 路由薄入口
```

> 以下文件属于 apiserver 传输层/协议入口职责，不参与本次迁移：
> `methods_account.go`、`methods_approval.go`、`methods_command.go`、`methods_config.go`、
> `methods_ui_code_open.go`、`methods_ui_projects.go`、`methods_ui_state.go`、
> `code_run_tools.go`、`orchestration_report.go`、`orchestration_tools.go`、`resource_tools.go`、
> `workspace_methods.go`、`notifications.go`、`server_*.go`、`protocol.go`、`methods_offline52_list.go`。

## 3. 强约束（必须遵守）

### 3.1 路径改动白名单（按阶段）

| 阶段 | 允许改动 | 禁止改动 |
|---|---|---|
| P0 | `internal/apiserver/p1_codex_isolation_test.go` `internal/apiserver/methods_helpers_p3_test.go` | 其他业务实现文件 |
| P1 | `internal/apiserver/turn_tracker*.go` `internal/apiserver/codexadapter/**` `internal/apiserver/server.go`（仅 tracker 相关） | `internal/dashrpc/**` `internal/skills/**` |
| P2 | `internal/apiserver/*dashboard*.go` `internal/apiserver/server.go`（仅注册绑定） `internal/dashrpc/**` | `internal/skills/**` `thread/turn` 业务文件 |
| P3 | `internal/apiserver/methods_skills.go` `internal/apiserver/methods.go`（仅 skills 注册项） `internal/apiserver/*skills*entry*.go` `internal/apiserver/server.go`（仅 skills 绑定） `internal/skills/**` | `internal/dashrpc/**` `thread/turn` 业务文件 |
| P4 | `internal/apiserver/methods_thread*.go` `internal/apiserver/methods_turn*.go` `internal/apiserver/methods_helpers_codex.go`（业务逻辑迁出，仅保留薄委派） `internal/apiserver/methods_helpers.go`（仅斜杠命令相关函数） `internal/apiserver/codexadapter/**` | `internal/dashrpc/**` `internal/skills/**` |

### 3.2 边界硬约束

1. `internal/apiserver/codexadapter/**`、`internal/apiserver/commonadapter/**`、`internal/dashrpc/**`、`internal/skills/**` 均不得 import `internal/apiserver`。
2. `apiserver` 中 Thread/Turn 入口函数必须是薄委派（参数解析 -> 调用 adapter -> 返回结果）。
3. 除 `codexadapter` 外，`apiserver` 业务文件不得出现 `proc.Client.*` 直接调用。
4. 迁移过程中不得更改 JSON-RPC method name。
5. 迁移过程中不得更改动态工具 schema（通过测试守护）。

## 4. 一次性基线（开始前必须执行）

```bash
set -euo pipefail

pwd && git rev-parse --is-inside-work-tree
mkdir -p .tmp/apiserver-split-baseline

# 1) JSON-RPC method 名称基线（功能契约）
if [ ! -f scripts/extract_jsonrpc_methods.go ]; then
  echo "FAIL: missing scripts/extract_jsonrpc_methods.go"; exit 1
fi
go run "$PWD/scripts/extract_jsonrpc_methods.go" > .tmp/apiserver-split-baseline/jsonrpc.methods.before.txt
test -s .tmp/apiserver-split-baseline/jsonrpc.methods.before.txt || {
  echo "FAIL: empty method baseline"; exit 1;
}

# 2) 基础编译与关键守护测试（限定业务包，避免 scripts/ 多 main 互斥）
go build ./cmd/... ./internal/... ./pkg/...
go test ./internal/apiserver -run "TestP1CodexSymbolsAreIsolated|TestP3CodexEntryMethodsDelegateToCodexAdapter|TestP4BoundaryMethodsAvoidDirectClientCalls" -count=1
go test ./internal/tools -run TestDynamicToolSchemasStable -count=1
```

## 5. 每阶段通用门禁（阶段结束必须执行）

```bash
set -euo pipefail

if [ ! -f scripts/extract_jsonrpc_methods.go ]; then
  echo "FAIL: missing scripts/extract_jsonrpc_methods.go"; exit 1
fi
if [ ! -s .tmp/apiserver-split-baseline/jsonrpc.methods.before.txt ]; then
  echo "FAIL: missing baseline snapshot, run section 4 first"; exit 1
fi

go build ./cmd/... ./internal/... ./pkg/...
go test ./internal/apiserver/... -count=1
go vet ./internal/apiserver/...

# codexadapter 已被 ./internal/apiserver/... 递归覆盖，无需单独执行
if [ -d internal/dashrpc ]; then
  go test ./internal/dashrpc/... -count=1
  go vet ./internal/dashrpc/...
fi
if [ -d internal/skills ]; then
  go test ./internal/skills/... -count=1
  go vet ./internal/skills/...
fi

# Schema 稳定性（功能不变核心守护）
go test ./internal/tools -run TestDynamicToolSchemasStable -count=1

# 反向依赖
bad=0
if rg -n '"github.com/multi-agent/go-agent-v2/internal/apiserver"' internal/apiserver/codexadapter --glob '*.go' --glob '!**/*_test.go'; then
  echo "FAIL: codexadapter imports apiserver"; bad=1
fi
if rg -n '"github.com/multi-agent/go-agent-v2/internal/apiserver"' internal/apiserver/commonadapter --glob '*.go' --glob '!**/*_test.go'; then
  echo "FAIL: commonadapter imports apiserver"; bad=1
fi
if [ -d internal/dashrpc ] && rg -n '"github.com/multi-agent/go-agent-v2/internal/apiserver"' internal/dashrpc --glob '*.go' --glob '!**/*_test.go'; then
  echo "FAIL: dashrpc imports apiserver"; bad=1
fi
if [ -d internal/skills ] && rg -n '"github.com/multi-agent/go-agent-v2/internal/apiserver"' internal/skills --glob '*.go' --glob '!**/*_test.go'; then
  echo "FAIL: skills imports apiserver"; bad=1
fi
test "$bad" -eq 0

# 强约束前置：所有阶段都禁止 apiserver 非 adapter 目录直连 proc.Client
if rg -n 'proc\.Client\.[A-Za-z_][A-Za-z0-9_]*\b' internal/apiserver --glob '*.go' --glob '!internal/apiserver/codexadapter/**' --glob '!**/*_test.go'; then
  echo "FAIL: direct proc.Client call outside codexadapter"; exit 1
fi

# JSON-RPC method 名称集合不变
go run "$PWD/scripts/extract_jsonrpc_methods.go" > .tmp/apiserver-split-baseline/jsonrpc.methods.after.txt
test -s .tmp/apiserver-split-baseline/jsonrpc.methods.after.txt || {
  echo "FAIL: empty method snapshot after changes"; exit 1;
}
diff -u \
  .tmp/apiserver-split-baseline/jsonrpc.methods.before.txt \
  .tmp/apiserver-split-baseline/jsonrpc.methods.after.txt
```

> 方法提取由 `scripts/extract_jsonrpc_methods.go` 负责（AST 提取：`s.methods[...]` + `register(...)`）。禁止运行时拼接 method 名；method 注册必须使用字符串字面量或 `const` 字符串。

## 6. 分阶段执行细则

---

## P0：先修护栏测试口径（解锁迁移）

### P0 要改什么

| 文件 | 变更 |
|---|---|
| `internal/apiserver/p1_codex_isolation_test.go` | 移除“固定文件名”断言，改为“边界行为断言” |
| `internal/apiserver/methods_helpers_p3_test.go` | 保留“不得直连 proc.Client / 必须经 codexAdapter”约束，去掉路径耦合 |

### P0 迁移结果核对

```bash
set -euo pipefail
go test ./internal/apiserver -run "TestP1CodexSymbolsAreIsolated|TestP3CodexEntryMethodsDelegateToCodexAdapter|TestP4BoundaryMethodsAvoidDirectClientCalls" -count=1
go test ./internal/apiserver/... -count=1
go vet ./internal/apiserver/...
```

---

## P1：TurnTracker 迁入 `codexadapter`

### P1 要改什么（符号搬迁矩阵）

| 源 | 目标 | 要求 |
|---|---|---|
| `internal/apiserver/turn_tracker.go` | `internal/apiserver/codexadapter/turn_tracker.go` | 迁移状态机核心逻辑 |
| `internal/apiserver/turn_tracker_codex.go` | `internal/apiserver/codexadapter/turn_tracker_helpers.go`（或 `turn_runtime.go`） | 迁移 stall/heartbeat/终结判定辅助逻辑 |
| `internal/apiserver/turn_tracker_test.go` | `internal/apiserver/codexadapter/turn_tracker_test.go` | 测试随逻辑迁移 |

### P1 怎么改

1. 先迁移纯逻辑与 helper；再让 `apiserver` 入口变为委派调用。
2. P1 不强制一次性收敛 `Server` 全部 tracker 字段；允许保留状态字段，先完成“逻辑下沉 + 入口委派”。
3. 不允许 `codexadapter` import `apiserver`。

### P1 核对迁移结果

```bash
set -euo pipefail

go test ./internal/apiserver/... -count=1

# TurnTracker 核心实现不得残留在 apiserver 顶层
if rg -n '^type trackedTurnSummaryCacheEntry\b|^func.*ensureTurnTrackerLocked\b' internal/apiserver --glob '*.go' --glob '!**/codexadapter/**' --glob '!**/*_test.go'; then
  echo "FAIL: TurnTracker core definition still in apiserver"; exit 1
fi

# TurnTracker 核心函数若残留在 apiserver，必须是“薄委派”。
# 该检查使用 AST（避免 sed/wc 误判）。
# 注意：该脚本是 P1 验收门禁，P1 前运行失败是预期现象。
if [ ! -f scripts/check_p1_turn_tracker_thin_wrappers.go ]; then
  echo "FAIL: missing scripts/check_p1_turn_tracker_thin_wrappers.go"; exit 1
fi
go run "$PWD/scripts/check_p1_turn_tracker_thin_wrappers.go"
```

---

## P2：Dashboard JSON-RPC 迁入 `internal/dashrpc`

> `internal/dashboard` 目前是 HTTP 服务包，不作为本次 JSON-RPC 拆分目标。

### P2 要改什么（符号搬迁矩阵）

| 源 | 目标 | 要求 |
|---|---|---|
| `internal/apiserver/dashboard_methods.go` | `internal/dashrpc/methods.go` | 迁移 dashboard 业务查询逻辑 |
| `internal/apiserver/methods_ui_dashboard.go` | `internal/dashrpc/ui.go` | 迁移 UI dashboard 聚合逻辑 |
| `internal/apiserver/server.go`（或 methods 注册点） | 保留薄入口 | 仅保留路由注册绑定 |

### P2 怎么改（必须用可解耦注册方式）

禁止直接把 `s.methods`（`apiserver.Handler`）传入 `dashrpc`。  
必须使用“注册回调注入”，避免 `dashrpc` 反向依赖 `apiserver` 类型。

推荐接口：

```go
// internal/dashrpc/register.go
type MethodHandler func(ctx context.Context, params json.RawMessage) (any, error)
type RegisterFn func(name string, h MethodHandler)

// MethodCaller 必须能回调 thread/list 等非 dashboard 方法，
// 用于 ui/dashboard/get 的 agents 页面兜底逻辑（callDash → thread/list）。
func Register(register RegisterFn, provider DashboardProvider, caller MethodCaller)
```

`apiserver` 侧薄入口示意：

```go
dashrpc.Register(func(name string, h dashrpc.MethodHandler) {
    s.methods[name] = func(ctx context.Context, params json.RawMessage) (any, error) {
        return h(ctx, params)
    }
}, providers, s)
```

### P2 核对迁移结果

```bash
set -euo pipefail

go test ./internal/dashrpc/... ./internal/apiserver/... -count=1

# 必须新增并维护 dashboard 契约测试（返回字段/空值形态/错误语义）
rg -n '^func TestDashboardContractsStable\(' internal/apiserver --glob '*_test.go' >/dev/null || {
  echo "FAIL: missing TestDashboardContractsStable"; exit 1;
}
go test ./internal/apiserver -run "TestDashboardContractsStable" -count=1

# 源文件必须迁走（不能用 ls fileA fileB，避免只迁走一个时漏检）
if [ -f internal/apiserver/dashboard_methods.go ] || [ -f internal/apiserver/methods_ui_dashboard.go ]; then
  echo "FAIL: dashboard source files still exist in apiserver"; exit 1
fi
```

---

## P3：Skills 迁入 `internal/skills`

### P3 要改什么（符号搬迁矩阵）

| 源 | 目标 | 要求 |
|---|---|---|
| `internal/apiserver/methods_skills.go` | `internal/skills/methods.go` | 迁移 skills CRUD 与远程读写逻辑 |
| `methods_skills.go` 中纯函数 | `internal/skills/helpers.go` | 纯函数拆分 |
| `Server.GetAgentSkills` | `apiserver` 保留薄入口 | 委派到 `skills.Manager` |

### P3 怎么改

1. `collectAutoMatchedSkillMatches` / `renderAutoMatchedSkillPrompt` 继续留在 `methods_turn.go`（与 turn 启动流程紧耦合），不跟 Skills 一起迁移。P4 阶段随 turn 逻辑一起下沉到 `codexadapter`。
2. `skills` 包仅通过 provider 获取 `SkillService`，不得直接依赖 `apiserver.Server`。
3. `skills/*` 注册项允许保留在 `methods.go`（仅改 skills 相关行）或迁到 `*skills*entry*.go` 薄入口文件。
4. `commonadapter/skills.go` 中的纯函数（`ClassifyAutoSkillMatch`、`NormalizeSkillNames`、`CollectSkillNameSet` 等）保持在 `commonadapter/` 不迁移，作为 `apiserver` 和 `internal/skills` 的共享依赖层。`internal/skills` 可直接 import `commonadapter`。

### P3 核对迁移结果

```bash
set -euo pipefail

go test ./internal/skills/... ./internal/apiserver/... -count=1

# 必须新增并维护 skills 契约测试（字段 shape / 兼容默认值）
rg -n '^func TestSkillsContractsStable\(' internal/apiserver --glob '*_test.go' >/dev/null || {
  echo "FAIL: missing TestSkillsContractsStable"; exit 1;
}
go test ./internal/apiserver -run "TestSkillsContractsStable" -count=1

if [ -f internal/apiserver/methods_skills.go ]; then
  echo "FAIL: methods_skills.go still in apiserver"; exit 1
fi
```

---

## P4：Thread/Turn 业务迁入 `codexadapter`

### P4 要改什么（符号搬迁矩阵）

> `codexadapter/` 已有以下文件（P1 或更早阶段迁入），P4 应追加到已有文件而非创建同名新文件：
> `thread_archive.go`、`thread_history.go`、`thread_manage.go`、`thread_messages.go`、
> `thread_usecases.go`、`turn_runtime.go`、`slash_command.go`。

| 源 | 目标 | 要求 |
|---|---|---|
| `internal/apiserver/methods_thread.go` | `codexadapter/` 已有 `thread_*.go` 按业务归入 | thread 业务逻辑下沉 |
| `internal/apiserver/methods_thread_codex.go` | `codexadapter/` 已有 `thread_*.go` 按业务归入 | thread codex 逻辑下沉 |
| `internal/apiserver/methods_turn.go` | `codexadapter/turn_runtime.go` 或新建 `turn_methods.go` | turn 业务逻辑下沉 |
| `internal/apiserver/methods_turn_codex.go` | `codexadapter/turn_runtime.go` 或新建 `turn_methods.go` | turn codex 逻辑下沉 |
| `internal/apiserver/methods_helpers_codex.go` | `codexadapter/` 按业务归入 | Codex 线程恢复/斜杠命令底层下沉 |
| `internal/apiserver/methods_helpers.go` 中斜杠命令函数 | `codexadapter/slash_command.go` | `threadUndo` 等 Codex 专属方法下沉 |
| `internal/apiserver/methods_thread_test.go` | `codexadapter/thread_*_test.go` | 测试随迁 |
| `internal/apiserver/methods_turn_test.go` | `codexadapter/turn_*_test.go` | 测试随迁 |

### P4 怎么改（分批搬迁顺序）

1. 先搬只读方法：`thread/list`、`thread/loaded/list`、`thread/read`、`thread/resolve`、`thread/messages`。
2. 再搬变更方法：`thread/start`、`thread/resume`、`thread/name/set`、`thread/rollback`、`thread/fork`、`thread/archive`、`thread/unarchive`、`thread/compact/start`。
3. 搬 turn 主链：`turn/start`、`turn/steer`、`turn/interrupt`、`turn/forceComplete`、`review/start`。
4. 最后搬 Codex 斜杠命令：`thread/undo`、`thread/model/set`、`thread/personality/set`、`thread/approvals/set`、`thread/mcp/list`、`thread/skills/list`、`thread/debugMemory`、`thread/backgroundTerminals/clean`。源码在 `methods_helpers.go` 中，迁入 `codexadapter/slash_command.go`。
5. 每批都把 apiserver 入口收敛为薄委派，再进入下一批。

### P4 核对迁移结果

```bash
set -euo pipefail

# 必须新增并维护 P4 路由覆盖测试：
# 覆盖 methods.go 中所有 thread/*、turn/*、review/start 注册入口，
# 验证对应处理函数均为薄委派并经 s.codexAdapter。
for t in \
  TestP4ThreadTurnRegisteredRoutesDelegateToCodexAdapter \
  TestP4BoundaryMethodsAvoidDirectClientCalls \
  TestP3CodexEntryMethodsDelegateToCodexAdapter \
  TestP3ResidualMethodsMustDelegateViaCodexAdapter \
  TestP4NoDirectCodexPackageImportOutsideAdapter
do
  rg -n "^func ${t}\\(" internal/apiserver --glob '*_test.go' >/dev/null || {
    echo "FAIL: missing ${t}"; exit 1;
  }
done
go test ./internal/apiserver -run "TestP4ThreadTurnRegisteredRoutesDelegateToCodexAdapter" -count=1

go test ./internal/apiserver -run "TestP4BoundaryMethodsAvoidDirectClientCalls|TestP3CodexEntryMethodsDelegateToCodexAdapter|TestP3ResidualMethodsMustDelegateViaCodexAdapter|TestP4NoDirectCodexPackageImportOutsideAdapter" -count=1
go test ./internal/apiserver/... -count=1
go test -race ./internal/apiserver/... -count=1

# 允许薄入口文件继续存在；不允许 apiserver 非 adapter 目录直连 proc.Client
if rg -n 'proc\.Client\.[A-Za-z_][A-Za-z0-9_]*\b' internal/apiserver --glob '*.go' --glob '!internal/apiserver/codexadapter/**' --glob '!**/*_test.go'; then
  echo "FAIL: direct proc.Client call outside codexadapter"; exit 1
fi
```

## 7. 功能不变保障（必须执行）

每阶段结束至少必须完成前 3 类保障；第 4/5 类按阶段触发条件执行：

1. **接口契约不变**：`jsonrpc.methods.before.txt` vs `after.txt` 必须 `diff` 无差异。
2. **工具契约不变**：`go test ./internal/tools -run TestDynamicToolSchemasStable -count=1` 必须通过。
3. **边界行为不变**：P0 护栏测试集必须持续通过。
4. **业务响应契约不变（按阶段）**：P2 起 `TestDashboardContractsStable` 必须通过；P3 起再追加 `TestSkillsContractsStable`。
5. **并发行为不变（最终门禁）**：仅 P4 必须执行 `go test -race`。

## 8. Agent 执行编排

总计 5 个 Agent，严格串行：

1. `P0 Agent`：护栏测试口径升级
2. `P1 Agent`：TurnTracker 下沉
3. `P2 Agent`：Dashboard JSON-RPC 下沉至 `dashrpc`
4. `P3 Agent`：Skills 下沉至 `skills`
5. `P4 Agent`：Thread/Turn 下沉至 `codexadapter`

> **P5 RegisterFn 评估结论**：经审查不可行。Thread/Turn wrapper 文件包含 Go receiver 方法绑定、params/response 类型定义和 Server 字段注入，不是纯路由注册，无法用 RegisterFn 消除。Dashboard 的 RegisterFn 模式之所以可行，是因为 dashrpc 定义了完整的 DashboardProvider 接口。P0-P4 已完成全部职责拆分目标。

执行纪律：

1. 任一时刻只允许 1 个 Agent 写代码。
2. 每阶段必须提交：变更清单 + 门禁输出 + 风险说明。
3. 任一门禁失败立即停止，先修本阶段，不得继续下游阶段。
