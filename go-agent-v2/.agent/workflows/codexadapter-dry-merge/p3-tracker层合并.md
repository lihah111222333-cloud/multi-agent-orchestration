---
description: Step 2 并行任务，Tracker 层按主题功能合并 + DRY（文件数非硬约束）。
---

# P3: Tracker 层合并（⚡ 可并行）

> **给 Agent:** 此任务与 P1/P2 并行，只操作 `turn_tracker*` 文件。

## 前置条件

- [ ] P0 已完成

## 合并原则（必须遵守）

1. 按 tracker 功能主题合并，不按原文件名拼接。
2. 保持 tracker 状态、stall、event 三域边界清晰。
3. 文件数不是硬指标，但同层命名必须统一为 `turn_tracker*.go`。
4. 纯函数与 adapter 方法按职责放置，避免同逻辑双实现。
5. 功能等价：turn_tracker 终结判定、summary 注入、stall 行为不得回归。

## 覆盖归属（全覆盖）

- 生产文件：`turn_tracker*.go`
- 测试文件：`turn_tracker_test.go`、`tracked_turn_shape_guardrail_test.go`
- 不在本阶段：`adapter*.go`、`runtime_context.go`、`thread_*`、`turn_*`（非 tracker）

## 命名规范（强约束）

1. 主文件命名：`turn_tracker.go`、`turn_tracker_stall.go`、`turn_tracker_event.go`
2. 辅助文件命名：`turn_tracker_<domain>_<aspect>.go`
3. 禁止命名：`tracker_misc.go`、`turn_tracker_tmp.go`、`turn_tracker_v2.go`

## 主题任务包（按功能执行）

### T1 Tracker Core 域

- 职责：`state init + lifecycle + terminal wait + status + heartbeat`
- 推荐文件：`turn_tracker.go`（必要时 `turn_tracker_lifecycle.go`）
- 约束：active turn 读写路径统一

### T2 Stall 控制域

- 职责：`stall detect + grace + heartbeat + auto interrupt`
- 推荐文件：`turn_tracker_stall.go`
- 约束：stall 计时与中断执行闭环，不跨域跳转

### T3 Event 与完成域

- 职责：`event parse + diag + capture + finalize + summary payload/cache`
- 推荐文件：`turn_tracker_event.go`
- 约束：事件提取、终结判定、summary 注入使用单一路径

## DRY 优化（主题内同步执行）

1. **D5**: 提取 `withActiveTurn`，消除方法开头模板
2. **D6**: `CaptureAndInjectTurnSummary` 参数收敛（11 → 7）
3. **D7**: `MaybeFinalizeTrackedTurn` 诊断字段统一 builder
4. **D15**: 仅包内导出降级
5. **D16**: 删除临时 DIAG 日志
6. **D17**: stall/meta heartbeat 纯函数按域收敛，避免重复包装
7. **E3**: 提取 `supersedeActiveTurn()` 子函数
8. 统一使用 `threadLogFields`

## 步骤

// turbo-all

1. 先定义三大主题文件落位（core/stall/event）
2. 按主题迁移，先保行为再做 DRY
3. 每主题结束执行一次验证
4. 全部完成后清理冗余文件

```bash
go build ./...
go test ./internal/apiserver/codexadapter/...
go vet ./internal/apiserver/codexadapter/...
```

## 完成标准

- [ ] tracker 合并按主题完成（core/stall/event）
- [ ] tracker 文件名符合 `turn_tracker*.go`
- [ ] event→finalize→summary 路径单一且可测试
- [ ] 本阶段归属文件覆盖率 100%（无漏项）
- [ ] 功能等价（tracker 状态机与终结语义无回归）
- [ ] `go build` + `go test` + `go vet` 通过
