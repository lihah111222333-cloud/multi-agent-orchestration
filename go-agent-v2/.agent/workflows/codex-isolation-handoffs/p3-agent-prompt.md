# P3 子 Agent 执行提示词：子包落地（codexadapter + commonadapter）

## 角色

你是 `P3 Agent`，负责 `codex-isolation` 工作流 Step 3 / P3 阶段。
当前交接状态见 `.agent/workflows/codex-isolation-handoffs/LATEST.md`（应为 `P2 done -> P3`）。

## 阶段目标

1. 将 P1 已聚合的 codex 文件簇正式迁移到 `internal/apiserver/codexadapter/`。
2. 将通用逻辑聚合到 `internal/apiserver/commonadapter/`。
3. 让 `internal/apiserver` 方法层退化为薄委派（参数转换 + 调用适配层）。

## 强制约束

1. 执行代码任务时：**读取和修改代码必须使用 LSP 工具**。
2. 测试/构建验证：**必须使用 run 工具**。
3. 遵守 TDD：先 RED（失败测试）→ GREEN（最小实现）→ REFACTOR。
4. 不改 JSON-RPC method name，不改对外协议输入输出语义。
5. 仅允许修改 P3 白名单路径。

## P3 允许改动路径（严格）

```text
internal/apiserver/codexadapter/**
internal/apiserver/commonadapter/**
internal/apiserver/methods_thread*.go
internal/apiserver/methods_helpers*.go
internal/apiserver/methods_turn*.go
internal/apiserver/turn_tracker*.go
internal/apiserver/server_payload.go
internal/apiserver/server_approval.go
internal/apiserver/server.go
internal/apiserver/methods.go
```

## P3 实施要求（必须满足）

1. 定义 `codexadapter.ServerContext`（或等价接口），封装 codex 侧所需能力（`mgr/store/binding/uiRuntime/notify/...`）。
2. `codexadapter` 仅依赖接口，不反向依赖 `apiserver` 具体结构体。
3. `commonadapter` 承载非 codex 通用能力（输入、prompt、skills、fuzzy 等）。
4. apiserver 入口方法可保留，但实现必须是薄委派。
5. `server_payload.go`、`server_approval.go` 仅允许参数转换、日志、错误包装；不得新增 codex 业务状态管理。

## 推荐执行步骤（TDD）

### 1) RED：先写结构约束测试

建议新增结构测试（可放 `internal/apiserver` 包内测试）验证：
1. `proc.Client.Submit/SendCommand/GetThreadID/ResumeThread` 只出现在：
   - `internal/apiserver/codexadapter/*`
   - `internal/apiserver/server_payload.go`
   - `internal/apiserver/server_approval.go`
2. apiserver 方法文件中的 codex 逻辑函数已退化为委派。

先跑测试，确保失败后再迁移实现。

### 2) GREEN：落地子包

1. 新建 `internal/apiserver/codexadapter/`：
   - 接口定义（`ServerContext`）
   - 从 `methods_*_codex.go`、`turn_tracker_codex.go` 抽离 codex 实现
2. 新建 `internal/apiserver/commonadapter/`：
   - 承载非 codex 通用函数/组件
3. 在 `internal/apiserver/server.go` 完成依赖组装与注入。
4. 将 apiserver 对应 methods 改为薄委派。

### 3) REFACTOR：清理边界

1. 精简 apiserver imports、移除重复桥接代码。
2. 校正命名与文件组织，保持 gofmt/govet 干净。
3. 保持测试全绿。

## 强制验证命令

```bash
go test ./internal/apiserver/... ./internal/apiserver/codexadapter/... ./internal/apiserver/commonadapter/... -count=1

bad=0
for f in $(rg -l --glob '*.go' "Client\.Submit|Client\.SendCommand|Client\.GetThreadID|Client\.ResumeThread" internal/apiserver); do
  case "$f" in
    internal/apiserver/codexadapter/*|internal/apiserver/server_approval.go|internal/apiserver/server_payload.go) ;;
    *) echo "unexpected proc.Client usage: $f"; bad=1 ;;
  esac
done
test "$bad" -eq 0

go vet ./internal/apiserver/... ./internal/apiserver/codexadapter/... ./internal/apiserver/commonadapter/...
```

## 完成标准

1. `codexadapter` / `commonadapter` 子包存在且职责明确。
2. apiserver 仅保留协议入口、装配、薄委派。
3. `proc.Client.*` 直接调用满足白名单约束。
4. 测试与 vet 全通过。

## 交接输出

生成并更新：

1. `.agent/workflows/codex-isolation-handoffs/p3.files.txt`
2. `.agent/workflows/codex-isolation-handoffs/p3.checks.log`
3. `.agent/workflows/codex-isolation-handoffs/p3.md`
4. `.agent/workflows/codex-isolation-handoffs/LATEST.md`：
   - `current_phase: P3`
   - `status: done`
   - `next_phase: P4`

若阻塞：
1. 写 `p3.blockers.md`
2. `LATEST.md` 写为 `status: blocked`, `next_phase: P3`
