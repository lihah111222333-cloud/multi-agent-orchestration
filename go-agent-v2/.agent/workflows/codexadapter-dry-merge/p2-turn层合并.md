---
description: Step 2 并行任务，Turn 层按主题功能合并 + DRY（文件数非硬约束）。
---

# P2: Turn 层合并（⚡ 可并行）

> **给 Agent:** 此任务与 P1/P3 并行，操作 `turn_*`（非 tracker）+ `slash_command*` + `with_thread.go`。

## 前置条件

- [ ] P0 已完成

## 合并原则（必须遵守）

1. 按功能主题合并，不按来源文件表机械迁移。
2. 文件数不是硬指标，优先让每个 turn 用例入口清晰。
3. 同层命名统一：`turn_*.go`，斜杠命令统一到 `slash_command.go`。
4. `with_thread` 仅作为 runtime 内部能力，不保留独立孤岛文件。
5. 功能等价：turn 对外行为、slash 兼容入口、事件/payload 语义不得变化（除非显式修复单列说明）。

## 覆盖归属（全覆盖）

- 生产文件：`turn_*.go`（排除 `turn_tracker*`）、`slash_command*.go`、`with_thread.go`
- 测试文件：`turn_prompt_test.go`、`turn_resume_test.go`、`with_thread_test.go`
- 不在本阶段：`adapter*.go`、`runtime_context.go`、`thread_*`、`turn_tracker*`

## 命名规范（强约束）

1. 主文件命名：`turn_<domain>.go`
2. 辅助文件命名：`turn_<domain>_<aspect>.go`
3. 例外文件：`slash_command.go`
4. 禁止命名：`turn_misc.go`、`turn_helpers2.go`、`turn_tmp.go`、`turn_v2.go`

## 主题任务包（按功能执行）

### T1 运行时编排域

- 职责：`EnsureThreadReady + start/steer submit + runtime notify + with_thread`
- 推荐文件：`turn_runtime.go`（必要时 `turn_runtime_resume.go`）
- 约束：turn 入口路径统一，避免 start/steer 两套并行模板代码

### T2 恢复与搜索域

- 职责：`turn resume + fuzzy search`
- 推荐文件：`turn_resume.go`
- 约束：搜索仅服务恢复流程，不再作为孤立 wrapper

### T3 中断域

- 职责：`interrupt/forceComplete/state/wait/notify`
- 推荐文件：`turn_interrupt.go`
- 约束：命令发送、状态判定、等待策略在同域内闭环

### T4 输入准备域

- 职责：`parse input + attachment + timeline + skill selection`
- 推荐文件：`turn_prepare.go`
- 约束：输入解析一次遍历完成，避免重复 switch

### T5 提示词与技能域

- 职责：`prompt build + lsp hint + skill match + skill render`
- 推荐文件：`turn_prompt.go`
- 约束：匹配与渲染同域组织，不散落多文件

### T6 斜杠命令域

- 职责：`slash params parse + send + thread fallback`
- 推荐文件：`slash_command.go`
- 约束：保留向后兼容入口，避免 apiserver 调用回退

## DRY 优化（主题内同步执行）

1. **D3/D14**: 删除 `*Adapter.FuzzyFileSearch()` 直传包装
2. **D8**: 输入解析统一到 `parseTurnInputs()` 一次遍历
3. **D9**: 附件拼装收敛为 image/file 两个 helper
4. **D10**: 删除 `BuildAttachmentPreviewURL` passthrough
5. **D15**: 仅包内导出降级
6. **D16**: 清理临时 DIAG 日志
7. **E2**: 删除仅包内使用的 Skill 类型别名
8. 使用 P0 accessor / `requireThreadID` / `threadLogFields`

## 步骤

// turbo-all

1. 先按主题落目标文件命名
2. 逐主题迁移并内聚逻辑
3. 每主题结束执行包级验证
4. 清理废弃源文件并做一次全量验证

```bash
go build ./...
go test ./internal/apiserver/codexadapter/...
go vet ./internal/apiserver/codexadapter/...
```

## 完成标准

- [ ] turn 非 tracker 合并按主题完成（runtime/resume/interrupt/prepare/prompt/slash）
- [ ] turn 层文件名符合 `turn_*.go` + `slash_command.go`
- [ ] 输入解析与附件处理不重复实现
- [ ] 本阶段归属文件覆盖率 100%（无漏项）
- [ ] 功能等价（turn/slash 行为、事件、payload 无回归）
- [ ] `go build` + `go test` + `go vet` 通过
