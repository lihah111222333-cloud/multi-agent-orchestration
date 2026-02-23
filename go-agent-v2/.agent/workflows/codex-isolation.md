---
description: Codex/LSP/通用职责隔离 — apiserver 按职责拆分并完成子包落地，服务未来多 CLI 适配
---

# Codex 职责隔离工作流（子包落地版）

// turbo-all

## 主题与最终产物

本工作流不是“仅铺路”，而是**在本次交付内完成包边界落地**：

1. `codex` 专属实现从 `internal/apiserver` 抽取为 `internal/apiserver/codexadapter/` 子包。
2. `lsp` 动态工具实现（含 ext）全部聚合到 `internal/lsp/`。
3. 通用（非 codex、非 lsp）逻辑聚合到通用适配层（`internal/apiserver/commonadapter/`）。
4. `apiserver` 仅保留协议入口、路由注册、依赖组装、轻量委派。
5. 全量 `go build ./... && go test ./... -count=1 && go vet ./...` 通过。

## 完成口径（禁止“只铺路”判完成）

1. 仅当 `P1/P2/P3/P4` 全部阶段 `status: done`，且 `LATEST.md` 为 `next_phase: complete`，方可对外声明“工作流完成”。
2. `P1`/`P2`/`P3` 任一单阶段完成只能声明“阶段完成”，不得替代主题完成。
3. 若仅完成同包重排，未形成 `codexadapter` + `commonadapter` + `internal/lsp` 三个可复用边界，视为未达标。

## LSP 审查校准结论（已验证）

基于 `documentSymbol / callHierarchy / references`：

1. `diagCache/diagMu` 读写不只在 `server_dynamic_tools.go`，还出现在 `methods_config.go` 和 `methods_ui_code_open.go`，必须统一访问面。
2. `threadArchiveTyped/threadUnarchiveTyped` 的调用链集中于归档/codex 辅助函数，适合作为 codex 子包核心簇。
3. `server_dynamic_tools.go` 仅覆盖 9 个基础 handler；`server_dynamic_tools_*_ext.go` 仍有多组 `lsp*` handler，必须纳入“全量迁移”。
4. `methods_thread.go` 中 `threadResumeTyped/threadNameSetTyped/threadRollbackTyped` 直接调用 `proc.Client`，按职责应归入 codex 侧。

## 验收标准（必须全部满足）

1. `internal/apiserver/codexadapter/` 存在并承载 codex 专属核心实现。
2. `internal/lsp/` 承载全部动态 `lsp*` tool handlers（基础 + ext）。
3. `internal/apiserver/commonadapter/` 承载通用逻辑，避免与 codex/lsp 交叉耦合。
4. `internal/apiserver` 中原方法文件仅保留 JSON-RPC 参数校验与薄委派。
5. 诊断读取统一走访问面，不再散落直接读写 `diagCache`。
6. 全量 build/test/vet 通过，交接包完整。
7. `LATEST.md` 使用规范化状态字段（`status/current_phase/next_phase`），无自由文本状态值。

## 分类标准（符号驱动）

| 分类 | 特征 | 目标包 |
|---|---|---|
| Codex | 触达 `proc.Client.Submit/SendCommand/GetThreadID/ResumeThread`；处理 rollout/codex thread；归档 codex artifact；处理 codex turn 跟踪 | `internal/apiserver/codexadapter/` |
| LSP | 动态工具 `lsp*` handler、LSP 参数解析、结果序列化 | `internal/lsp/` |
| 通用 | DTO、输入解析、技能匹配、文件搜索、非 codex 协议辅助 | `internal/apiserver/commonadapter/` |

## 执行前提

```bash
pwd && git rev-parse --is-inside-work-tree
go build ./...
go test ./internal/apiserver/... ./internal/lsp/... -count=1
```

## 多 Agent 串行模型

- `P0 Agent`: 基线确认 + LSP 快照 + 规划审查
- `Pre-P1 Agent`: 测试基线建立（重构护栏纯函数测试）
- `P1 Agent`: apiserver 同包内按符号聚合（低风险重排）
- `P2 Agent`: LSP 全量迁移到 `internal/lsp`
- `P3 Agent`: 子包落地（`codexadapter` + `commonadapter`）
- `P4 Agent`: 全量验收与 CLI 适配准备度评估

执行纪律：

1. 任一时刻仅 1 个 Agent 写代码。
2. 下游 Agent 必须基于上游交接包继续，禁止跳步。
3. 每阶段仅改允许路径。
4. 编译失败或测试回归立即中止并回传阻塞点。

## 交接包规范

每阶段提交到 `.agent/workflows/codex-isolation-handoffs/`：

1. `pN.files.txt`
2. `pN.checks.log`
3. `pN.md`
4. `LATEST.md`
5. `pN.blockers.md`（可选；仅 `status: blocked` 时必需）

`LATEST.md` 必须使用固定 front matter 字段：

| 字段 | 说明 | 允许值 |
|---|---|---|
| `current_phase` | 当前执行或最近完成阶段 | `P0/P1/P2/P3/P4` |
| `status` | 阶段状态（机器可读） | `in_progress/done/blocked` |
| `next_phase` | 下一步阶段 | `P1/P2/P3/P4/complete` |
| `updated_at` | 时间戳（ISO-8601，含时区） | 例如 `2026-02-24T05:12:00+08:00` |
| `owner` | 当前责任 Agent | 文本 |

状态迁移规则：

1. 阶段开始执行：`current_phase=Pn`、`status=in_progress`、`next_phase=Pn`。
2. 阶段完成并交接：`current_phase=Pn`、`status=done`、`next_phase=P(n+1)`；若 `Pn=P4`，则 `next_phase=complete`。
3. 阶段阻塞：`current_phase=Pn`、`status=blocked`、`next_phase=Pn`，并新增 `pN.blockers.md`。
4. `P0` 规划完成：`current_phase=P0`、`status=done`、`next_phase=P1`。
5. `status` 仅描述 `current_phase`，禁止引入 `pending/待执行/未开始` 等额外状态值写入 `LATEST.md`。

阶段文件要求：

1. `P0` 为规划阶段，可仅更新 `LATEST.md`。
2. `P1-P4` 必须完整提交 `pN.files.txt / pN.checks.log / pN.md`。

---

## Step 0 / P0: 基线与 LSP 快照

```bash
go build ./...
go test ./... -count=1
go vet ./...
```

通过标准：全部成功。

---

## Pre-P1: 测试基线（重构护栏）

P1 执行前必须建立测试基线，覆盖即将搬移的纯函数，作为行为不变的最低锚点。

### 最低要求

1. `methods_turn_test.go`: codex 侧（`normalizeInterruptState`、`interruptSettleMode`、`fuzzyMatch`、`normalizeSkillName`）+ 通用侧（`mergePromptText`、`collectInputSkillNames`、`composeUserTimelineTextForTurn`）。
2. `methods_helpers_test.go`: codex 侧（`isLikelyCodexThreadID`、`normalizeCodexThreadID`、`buildResumeCandidates`、`tryResumeCandidates`）+ 通用侧（`extractInputs`、`buildAttachmentName`）。
3. `turn_tracker_test.go`: payload 解析（`threadStatusTerminalFromPayload`、`normalizeTrackedTurnStatus`、`extractTrackedTurnID`）。
4. `methods_thread_test.go`: 通用纯函数（`calculateHydrationLoadLimit`、`sanitizeArchiveName`、`inferThreadArtifactKind`、`pathWithinRoot`）。

### 验证

```bash
go test ./internal/apiserver/... -count=1 -v
```

通过标准：所有新增测试通过，且不引入编译错误。

---

## Step 1 / P1: apiserver 内部先聚合（不跨包）

### P1 目标

1. 先把 codex 与通用函数按符号聚合到独立文件簇。
2. 不改行为、不改方法签名、不改 JSON-RPC method name。
3. 为 P3 跨包抽取降低冲突面。

### P1 允许改动路径

```text
internal/apiserver/methods_thread.go
internal/apiserver/methods_helpers.go
internal/apiserver/methods_turn.go
internal/apiserver/turn_tracker.go
internal/apiserver/*_codex*.go (新增)
internal/apiserver/*_skills*.go (新增)
```

### P1 关键拆分要求（符号级）

1. `methods_thread.go`:
- codex 侧迁出：`threadResumeTyped`、`threadMessagesTyped`、rollout 相关函数、`threadArchiveTyped/threadUnarchiveTyped`、`threadNameSetTyped`、`threadRollbackTyped`。
- 通用侧保留：线程 CRUD/list/read/resolve DTO 与非 codex 生命周期逻辑。

2. `methods_helpers.go`:
- codex 侧迁出：thread candidate / ensure ready / register binding / slash command sending。
- 通用侧保留：输入提取、附件构造、debug 辅助。

3. `methods_turn.go`:
- codex 侧迁出：`activeTurnIDReader`（接口）、`resolveClientActiveTurnID`、`turnStartTyped`、`turnSteerTyped`、`turnInterrupt`、`turnForceComplete`、`reviewStartTyped`、`normalizeInterruptState`、`readThreadRuntimeState`、`waitInterruptSettled`、`waitInterruptOutcome`。
- 通用侧保留：DTO（`UserInput`/`turnStartParams`/`turnInfo`）、skill prompt 组装、fuzzy 搜索、LSP hint、名称规范化。

4. `turn_tracker.go`:
- codex turn 跟踪与 stall 处理拆成 `turn_tracker_codex*.go` 文件簇。

### P1 验证

```bash
go test ./internal/apiserver/... -count=1
go vet ./internal/apiserver/...
rg -n "Client\\.Submit|Client\\.SendCommand|Client\\.GetThreadID|Client\\.ResumeThread" internal/apiserver | sort
```

---

## Step 2 / P2: LSP 全量迁移到 internal/lsp

### P2 目标

1. 把 apiserver 内全部 `lsp*` 动态工具 handler 迁入 `internal/lsp`。
2. apiserver 仅保留动态工具注册与依赖注入。
3. 统一诊断访问面，消除散落 `diagCache` 直接读取。

### P2 允许改动路径

```text
internal/lsp/tool_handlers.go
internal/lsp/tool_handlers_*.go
internal/apiserver/server_dynamic_tools.go
internal/apiserver/server_dynamic_tools_*_ext.go
internal/apiserver/server_dynamic_tools_ext_registry.go
internal/apiserver/server.go
internal/apiserver/methods_config.go
internal/apiserver/methods_ui_code_open.go
```

### P2 迁移清单（必须全量）

1. 基础 9 个：`Hover/OpenFile/Diagnostics/Definition/References/DocumentSymbol/Rename/Completion/DidChange`。
2. Ext 10 个：`CodeAction/SignatureHelp/Format/CallHierarchy/TypeHierarchy/SemanticTokens/FoldingRange/WorkspaceSymbol/Implementation/TypeDefinition`。
3. 辅助方法 2 个：`lspAvailabilitySummary`（配置摘要）、`lspDiagnosticsQueryTyped`（诊断查询 JSON-RPC handler）。迁移至 `internal/lsp`，apiserver 保留薄委派。

### P2 设计约束

1. 在 `internal/lsp` 定义 `ToolHandlers`，由 `Manager` + 诊断访问接口驱动。
2. `Server` 暴露线程安全诊断访问方法（如 `GetDiagnostics/GetAllDiagnostics/SetDiagnostics`）。
3. `methods_config.go` 与 `methods_ui_code_open.go` 必须改为调用统一访问面。
4. tool 名称与输入 schema 不变，避免协议破坏。
5. 若保留 ext provider registry，注册回调不得继续依赖 `*Server` 上的 `lsp*` handler 实现。
6. 禁止把动态工具 `lsp*` handler 迁移到 `internal/apiserver` 的其他文件“躲过校验”；P2 结束时 apiserver 全目录不得存在动态 `lsp*` handler 实现。
7. `internal/apiserver` 内所有 `s.dynTools["lsp_*"]` 注册必须统一绑定到 `s.lspTools.*`，禁止绑定到 `s.lsp*` 本地实现或匿名函数。

### P2 验证

```bash
go test ./internal/lsp/... ./internal/apiserver/... -count=1
go vet ./internal/lsp/... ./internal/apiserver/...
if rg -n --glob '*.go' "func \\(s \\*Server\\) lsp(Hover|OpenFile|Diagnostics|Definition|References|DocumentSymbol|Rename|Completion|DidChange|CodeAction|SignatureHelp|Format|CallHierarchy|TypeHierarchy|SemanticTokens|FoldingRange|WorkspaceSymbol|Implementation|TypeDefinition|AvailabilitySummary|DiagnosticsQueryTyped)\\b" internal/apiserver; then
  echo "error: apiserver still contains dynamic lsp handlers"
  exit 1
fi
if rg -n --glob '*.go' 's\\.dynTools\\["lsp_[^"]+"\\]\\s*=' internal/apiserver | rg -v 's\\.lspTools\\.'; then
  echo "error: lsp dynTools bindings in apiserver must route through s.lspTools"
  exit 1
fi
```

通过标准：`internal/apiserver` 全目录不再保留动态工具 `lsp*` handler 实现（仅可有注册/装配逻辑），且所有 `lsp_*` 动态工具注册均通过 `s.lspTools.*` 完成。

---

## Step 3 / P3: 子包落地（本工作流核心交付）

### P3 目标

1. 将 P1 codex 文件簇正式迁移到 `internal/apiserver/codexadapter/`。
2. 将通用逻辑聚合到 `internal/apiserver/commonadapter/`。
3. `apiserver` 方法层退化为薄委派。

### P3 允许改动路径

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

### P3 实施要求

1. 定义 `codexadapter.ServerContext`（或等价接口）封装 `mgr/store/binding/uiRuntime/notify` 所需能力。
2. `codexadapter` 仅依赖接口，不反向依赖 `apiserver` 具体结构。
3. `commonadapter` 承载非 codex 通用能力（输入、prompt、skills、fuzzy）。
4. apiserver 原入口函数可保留，但实现必须是薄委派。
5. 白名单入口 `server_payload.go`、`server_approval.go` 仅允许参数转换、日志与错误包装，不得新增 codex 业务分支与状态管理。

### P3 验证

```bash
go test ./internal/apiserver/... ./internal/apiserver/codexadapter/... ./internal/apiserver/commonadapter/... -count=1
# 稳定校验：不依赖管道退出语义，显式枚举违规文件
bad=0
for f in $(rg -l --glob '*.go' "Client\\.Submit|Client\\.SendCommand|Client\\.GetThreadID|Client\\.ResumeThread" internal/apiserver); do
  case "$f" in
    internal/apiserver/codexadapter/*|internal/apiserver/server_approval.go|internal/apiserver/server_payload.go) ;;
    *) echo "unexpected proc.Client usage: $f"; bad=1 ;;
  esac
done
test "$bad" -eq 0
```

通过标准：线程/回合/codex 归档链路中的 `proc.Client.*` 直接调用仅出现在 `codexadapter`（或明确允许的极少委派包装）；系统事件链路 `server_approval.go`、`server_payload.go` 为临时豁免并在 `p3.md` 记录原因。

---

## Step 4 / P4: 全量验收与 CLI 适配准备度

### P4 验证命令

```bash
go build ./...
go test ./... -count=1
go vet ./...
```

### P4 验收输出

1. `codexadapter` / `commonadapter` / `internal/lsp` 文件清单与职责边界说明。
2. `apiserver` 薄委派点位列表。
3. “适配其他 CLI”最小接入面：
- Codex: `ServerContext` 接口能力清单
- LSP: `ToolHandlers` 构造与调用入口
- Common: 通用输入与技能匹配入口
4. 更新 `LATEST.md` 为 `current_phase: P4`、`status: done`、`next_phase: complete`。

## 后续（独立工作流）

后续只做“多 CLI 适配层”即可，不再做大规模职责搬移。

### 其他 codex 散点（本工作流不处理，记录备忘）

| 文件 | codex 逻辑 | 说明 |
|---|---|---|
| `server_payload.go` L523-526 | `codexThreadId` 字段注入 | 事件分发中的 codex 线程 ID 修正 |
| `notifications.go` 整体 | `eventMethodMap` codex event→JSON-RPC 映射 | P3 `codexadapter` 时可一并迁移 |
