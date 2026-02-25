---
description: Step 2 并行任务，Thread 层按主题功能合并 + DRY（文件数非硬约束）。
---

# P1: Thread 层合并（⚡ 可并行）

> **给 Agent:** 此任务与 P2/P3 并行，只操作 `thread_*` 文件。

## 前置条件

- [ ] P0 已完成，`accessor / requireThreadID / threadLogFields` 可用

## 合并原则（必须遵守）

1. 按功能主题合并，不按来源文件名机械拼接。
2. 文件数不是硬指标，优先职责边界清晰。
3. 每个主题域最多保留 1 个主文件 + 少量同前缀辅助文件。
4. 同层统一命名：仅允许 `thread_*.go`。
5. 功能等价：thread 对外行为、返回结构、错误语义不得变化（除非显式修复单列说明）。

## 覆盖归属（全覆盖）

- 生产文件：`thread_*.go`（37 个）
- 测试文件（4 个）：
  - `thread_history_test.go`
  - `thread_list_helpers_test.go`
  - `thread_messages_test.go`
  - `thread_archive_utils_guardrail_test.go`
- 不在本阶段：`adapter*.go`、`runtime_context.go`、`turn_*`、`turn_tracker*`

## 命名规范（强约束）

1. 主文件命名：`thread_<domain>.go`
2. 辅助文件命名：`thread_<domain>_<aspect>.go`
3. 禁止命名：`thread_misc.go`、`thread_helpers2.go`、`thread_tmp.go`、`thread_v2.go`

## 主题任务包（按功能执行）

### T1 生命周期域

- 职责：`start/resume/fork/command/resolve` 全链路
- 推荐文件：`thread_lifecycle.go`（必要时 `thread_lifecycle_resume.go`）
- 约束：对外入口收敛，内部 helper 私有化

### T2 列表聚合域

- 职责：`list + history source + archive source + helper`
- 推荐文件：`thread_listing.go`（必要时 `thread_listing_history.go`）
- 约束：统一排序/去重/分页策略，不重复拼装逻辑

### T3 管理域

- 职责：`name/alias/id/manage/usecase`
- 推荐文件：`thread_manage.go`（必要时 `thread_manage_alias.go`）
- 约束：统一 threadId 校验与日志字段

### T4 归档域

- 职责：`archive/restore/prune/inspect/manifest/fileops/root/path`
- 推荐文件：`thread_archive.go` + `thread_archive_ops.go` + `thread_archive_utils.go`
- 约束：先处理 guardrail 再删壳文件，避免测试回归

### T5 消息域

- 职责：`messages load/source/rollout/stream/history-exists/candidates`
- 推荐文件：`thread_messages.go` + `thread_messages_stream.go`
- 约束：hydration/stream 逻辑单向依赖，避免双向调用

## DRY 优化（主题内同步执行）

1. **D4**: 删除 `appendThreadItems[T]`/`toThreadSnapshots[T]` 泛型 type-switch
2. **D11**: `thread_history_sources` 中 4 个 lookup 收敛到 2 个入口
3. **D12**: `appendHistoryFromXxx` 收敛为统一回调
4. **D15**: 仅包内符号降为小写
5. 全面替换三段式 nil-guard 为 P0 accessor
6. 全面替换 threadId 校验为 `requireThreadID`
7. 全面替换日志字段重复为 `threadLogFields`

## 步骤

// turbo-all

1. 先按主题落目标文件名（命名先行）
2. 逐主题合并代码并移除重复 helper
3. 每个主题完成后执行一次编译/测试
4. 全部主题完成后清理废弃源文件
5. 统一跑验证

```bash
go build ./...
go test ./internal/apiserver/codexadapter/...
go vet ./internal/apiserver/codexadapter/...
```

## 完成标准

- [ ] thread 合并按主题完成（lifecycle/listing/manage/archive/messages）
- [ ] thread 层文件名全部符合 `thread_*.go`
- [ ] 无 `thread_*` 跨主题重复逻辑
- [ ] 本阶段归属文件覆盖率 100%（无漏项）
- [ ] 功能等价（thread 行为/返回结构/错误语义无回归）
- [ ] `go build` + `go test` + `go vet` 通过
