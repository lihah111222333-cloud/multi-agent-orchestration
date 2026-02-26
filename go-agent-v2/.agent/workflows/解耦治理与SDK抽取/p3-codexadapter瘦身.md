---
description: P3 codexadapter 瘦身 — turn_tracker 三合一、archive 精简、messages DRY
---

# P3: codexadapter 瘦身

> **⚡ 可并行** — 与 P4、P5 同时执行（第二波）

## 前置条件

- [ ] P1 tools 接口化已完成（避免 tooladapter 冲突）

## 任务范围

### 需要修改的文件（全在 `internal/apiserver/codexadapter/` 内）

- `turn_tracker.go` (378行) + `turn_tracker_event.go` (656行) + `turn_tracker_stall.go` (319行) → 抽公共逻辑，避免重复状态机
- `thread_archive.go` (905行) + `thread_archive_utils.go` (556行) → 合并可复用工具，但避免超大函数
- `thread_messages.go` (549行) → DRY 精简
- `turn_prepare.go` (387行) + `turn_prompt.go` (306行) + `turn_resume.go` (187行) + `turn_steer_alignment.go` (45行) → 整合公共路径

### 禁止触碰的文件 ⚠️

- `internal/apiserver/*.go` (P4 负责)
- `internal/apiserver/server.go`, `server_context.go` — 只读（import codexadapter.Adapter/Deps）
- `internal/apiserver/commonadapter/*` — 只读（被 codexadapter 4 个文件 import，接口不可变）
- `internal/codex/*` (P5 负责)
- `internal/uistate/*` (P4/P5 负责)
- `internal/lsp/*` (P2 已完成)

## 接口稳定性约束 ⚠️

`apiserver/server.go` 和 `server_context.go` 直接引用 `codexadapter.Adapter` 和 `codexadapter.Deps`。

**P3 不得改变以下公开签名**：
- `codexadapter.New(deps Deps) *Adapter`
- `codexadapter.Deps` struct 的字段名和类型
- `*Adapter` 的所有导出方法签名（`Submit`, `SendCommand`, `GetThreadID`, `ResumeThread` 等）

内部实现可自由重构，但对外接口必须保持不变。

## guardrail 兼容约束（新增）

`thread_archive_utils_guardrail_test.go` 直接解析 `thread_archive_utils.go` 文件路径。

- 默认保留 `thread_archive_utils.go`（可瘦身为 facade/转发层）。
- 若确需删除该文件，必须在同一提交中同步更新 guardrail test，且补充等价的导出面约束测试。

## 执行步骤

// turbo-all

### 步骤 1: turn_tracker 去重与职责归位

1. 先提取共享状态检查/时间窗口判断为 helper，再决定是否需要物理合并文件
2. 保留 `event` 与 `stall` 的职责边界（推荐分文件，避免 >1,000 行巨型文件）
3. 删除重复定义，确保同一状态机只存在一处实现
4. 验证: `go build ./internal/apiserver/codexadapter/...`
5. 验证: `go test ./internal/apiserver/codexadapter/...`

### 步骤 2: thread_archive 合并精简

1. 将 `thread_archive_utils.go` 中可复用工具按主题并入（I/O、解析、恢复决策）
2. 简化 `inspectThreadArchiveForRestore`（当前接收 8 个 func 参数的 95 行函数 → 结构体方法/策略对象）
3. 保留 `thread_archive_utils.go` 作为稳定入口（允许内部委托），避免破坏现有 guardrail
4. 验证: `go build ./internal/apiserver/codexadapter/...`

### 步骤 3: thread_messages DRY

1. 提取消息构建的重复 payload 组装为辅助函数
2. 合并相似的消息格式化逻辑
3. 验证

### 步骤 4: turn 文件整合

1. 将 `turn_steer_alignment.go` (45行) 合入 `turn_prepare.go`
2. 审查 `turn_prepare.go` 和 `turn_prompt.go` 重叠逻辑，合并可复用部分
3. 验证

### 最终验证

```bash
go build ./internal/apiserver/codexadapter/...
go test ./internal/apiserver/codexadapter/...
go build ./...
go test ./...
```

## 完成标准

- [ ] `turn_tracker` 不再存在重复状态机逻辑（文件数不作硬性约束）
- [ ] `thread_archive` 大函数显著收敛（单函数 <=120 行）
- [ ] `thread_messages` 精简到 ~300 行
- [ ] codexadapter 总行数从 6,060 降至 ~4,500（优先行为稳定）
- [ ] `thread_archive_utils.go` 保持可解析（或已同步升级 guardrail）
- [ ] `Adapter`/`Deps` 公开签名未改变
- [ ] 所有 codexadapter 测试文件通过
- [ ] `go build ./...` 通过
