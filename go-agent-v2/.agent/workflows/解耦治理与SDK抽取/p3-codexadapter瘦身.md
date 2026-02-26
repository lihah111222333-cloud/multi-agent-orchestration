---
description: P3 codexadapter 瘦身 — DRY 去重 + 纯函数分文件（为 P7 迁走即删做准备）
---

# P3: codexadapter 瘦身

> **⚡ 可并行** — 与 P4、P5 同时执行（第二波）

## 前置条件

- [x] P1 tools 接口化已完成

## 当前基线（2026-02-27）

| 文件 | 行数 | 纯函数 | Adapter | internal 依赖 | 可拆? |
|------|------|--------|---------|---------------|-------|
| `thread_archive.go` | 988 | 18 | 14 | 零 | ✅ ~450 行 |
| `turn_tracker_event.go` | 656 | 20 | 8 | 零 | ✅ ~350 行 |
| `thread_archive_utils.go` | 556 | 27 | 0 | codex | ⚠️ 整文件但需 codex |
| `turn_tracker.go` | 502 | 3 | 15 | 零 | ❌ |
| `turn_runtime.go` | 508 | 2 | 14 | 4 包 | ❌ 不动 |
| `thread_listing.go` | 454 | 17 | 10 | 4 包 | ⚠️ 逐函数 |
| `turn_prepare.go` | 453 | 11 | 12 | 3 包 | ⚠️ 逐函数 |
| `thread_lifecycle.go` | 374 | — | — | — | 不动 |
| `adapter.go` | 359 | — | — | — | 不动 |
| `turn_interrupt.go` | 333 | 8 | 7 | agentcore/runner | ⚠️ 逐函数 |
| `turn_tracker_stall.go` | 315 | 4 | 13 | 零 | ❌ |
| `turn_prompt.go` | 295 | 8 | 5 | 4 包 | ⚠️ 逐函数 |
| `thread_history.go` | 252 | 5 | 6 | 零 | ⚠️ 部分伪纯 |
| `thread_messages_rollout.go` | 242 | 5 | 4 | agentcore/codex | ⚠️ |
| `turn_resume.go` | 175 | 5 | 1 | 零 | ✅ ~140 行 |
| 其余 5 小文件 | 535 | — | — | — | 不动 |
| **总计** | **7,037** | | | | |

**目标**: ≤ 5,700 行（减 ~1,300 行）

## 核心策略

1. **内部 DRY** — 去重、合并重复逻辑，不创建新 SDK 包
2. **纯函数分文件** — 零 `internal/` 依赖的纯函数拆入 `_core.go`
3. **类型随函数走** — 纯函数引用的本包类型（如 `trackedTurnSummaryCacheEntry`）一起搬入 `_core.go`
4. **为 P7 做准备** — `_core.go` 为 P7 直接 `git mv`，**不留委托层**

## 禁止触碰的文件 ⚠️

- `internal/apiserver/*.go` (P4)、`internal/codex/*` (P5)、`internal/uistate/*` (P4/P5)
- `server.go`、`server_context.go`、`commonadapter/*` — 只读

## 接口稳定性约束

`codexadapter.New(deps Deps)`、`Deps` struct、所有 `*Adapter` 导出方法签名不可改变。

## 执行步骤

// turbo-all

### 步骤 1: turn_tracker_event 纯函数分离（预估减 ~350 行）

**范围**: `turn_tracker_event.go` (656 行, 20 纯函数, 零 internal 依赖)

> [!IMPORTANT]
> 纯函数引用了 `trackedTurnSummaryCacheEntry`、`turnTrackerState` 等类型，
> 定义在 `turn_tracker.go`。这些类型本身也是零 internal 依赖，**必须一起搬入 `_core.go`**。

1. 从 `turn_tracker.go` 提取被纯函数引用的类型定义到 `turn_tracker_core.go`
2. 将 `turn_tracker_event.go` 的 20 个纯函数搬到 `turn_tracker_core.go`
3. Adapter 方法留在 `turn_tracker_event.go`，直接调 core 纯函数（同包，无额外 import）
4. 验证: `go build && go test ./internal/apiserver/codexadapter/...`

### 步骤 2: thread_archive 纯函数分离（预估减 ~450 行）

**范围**: `thread_archive.go` (988 行, 18 纯函数, 零 internal 依赖)

> [!IMPORTANT]
> 纯函数引用 6 个本包类型：`threadArchiveRestoreDeps`、`threadArchiveManifestScope`、
> `threadArchiveFile`、`threadArchiveManifest`、`threadArchiveRestoreNotice`、`threadArchives`。
> 这些类型本身零 internal 依赖，一起搬入 `thread_archive_core.go`。

1. 提取类型定义 + 18 纯函数到 `thread_archive_core.go`
2. 简化 `inspectThreadArchiveForRestore`（8 func 参数 → 结构体）
3. `thread_archive_utils.go` 保留（guardrail 依赖），内部 DRY 去重
4. 验证

### 步骤 3: turn_resume 纯函数分离（预估减 ~140 行）

**范围**: `turn_resume.go` (175, 5 纯函数, 零 internal 依赖)

> `TryResumeCandidates` 依赖 `logger`/`apperrors`，均在 `pkg/`，可拆。 
> 5 个函数均可搬入 `turn_resume_core.go`。

1. 拆 `turn_resume_core.go`
2. Adapter 方法 `ResumeThread` 留原文件
3. 验证

### 步骤 4: thread_history 部分分离（预估减 ~50 行）

**范围**: `thread_history.go` (252, 5 纯函数)

> [!WARNING]
> `resolveCodexThreadCandidates` 虽然签名是 `func`（非 Adapter 方法），但通过参数
> 接收 `stores` 和回调，**不是真纯函数**，不可拆。
> 仅 `ensureContext`、`normalizeHistoryTimeout`、`appendUniqueThreadIDFallback` 可拆（~50 行）。

1. 拆 3 个真纯函数到 `thread_history_core.go`
2. 验证

### 步骤 5: 有依赖文件逐函数检查（预估减 ~150 行）

**范围**: `turn_prepare.go`、`turn_prompt.go`、`turn_interrupt.go`、`thread_listing.go`

> 这些文件的纯函数多（44 个），但文件级有 internal 依赖。
> 需要逐函数检查函数体是否引用 internal，不引用的拆入 `_core.go`。

1. 逐函数标注依赖
2. 可拆的拆出 `_core.go`（预估约 10-15 个函数可拆）
3. 不可拆的留原文件
4. 验证

### 步骤 6: 内部 DRY 收尾（预估减 ~200 行）

1. `turn_tracker.go` + `turn_tracker_stall.go` 删除重复逻辑
2. `thread_messages_rollout.go` + `thread_messages_hydration.go` 合并相似逻辑
3. 全量验证

### 最终验证

```bash
go build ./...
go test ./internal/apiserver/codexadapter/...
go test ./...

# _core.go 零 internal 依赖检查
for f in internal/apiserver/codexadapter/*_core.go; do
  if grep -q '"github.com/multi-agent/go-agent-v2/internal/' "$f"; then
    echo "FAIL: $f has internal imports"
  else
    echo "PASS: $f clean"
  fi
done

# 行数统计
find internal/apiserver/codexadapter -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l
```

## 完成标准

- [ ] codexadapter 总行数 ≤ 5,700
- [ ] `*_core.go` 文件零 `internal/` 依赖
- [ ] 被拆纯函数引用的本包类型已随函数一起搬入 `_core.go`
- [ ] 同一逻辑只存在一处（无双实现）
- [ ] `thread_archive` 单函数 ≤ 120 行
- [ ] `thread_archive_utils.go` 保持可解析
- [ ] `Adapter`/`Deps` 公开签名未改变
- [ ] `go build ./... && go test ./...` 通过
