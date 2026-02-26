---
description: Go 代码解耦治理 + SDK 抽取 — 收尾执行手册（当前仅 P7）
---

# 解耦治理与 SDK 抽取

## 当前状态（2026-02-27）

| 属性 | 值 |
|------|-----|
| 已完成阶段 | P0, P1, P1.5, P1.6, P2, P3, P4, P5, P6 |
| 未完成阶段 | **P7** |
| 当前执行链 | **P6-lite（回归）→ P7** |
| 注意事项 | 仓库处于“部分目录已迁移到 `pkg/`”状态，P7 必须按分步收尾流程执行 |

## 收尾依赖图（执行版）

```mermaid
graph LR
    P6L["P6-lite: 回归验证"] --> P7["P7: SDK 提取（分步收尾）"]
```

## 阶段清单（执行版）

- [x] P0: 准备阶段
- [x] P1: tools Provider 接口抽象
- [x] P1.5: diff 模块独立
- [x] P1.6: bus 解耦
- [x] P2: LSP 碎片合并
- [x] P3: codexadapter 瘦身
- [x] P4: apiserver 顶层整理
- [x] P5: 事件表驱动
- [x] P6: 全量集成验证（历史基线）
- [ ] P7: SDK 提取（剩余）

## 文件分配（仅剩阶段）

| 文件/目录 | P7 | 约束 |
|----------|:--:|------|
| `internal/apiserver/codexadapter/*.go` | ✓ 写 | 仅允许适配层、存储/UI 状态相关改动 |
| `internal/apiserver/codexadapter/*_test.go` | ✓ 写 | 适配层与契约回归 |
| `internal/agentcore/**` | ✓ 写/迁移 | 迁移到 `pkg/codexsdk/agentcore` |
| `internal/codex/**` | ✓ 写/迁移 | 迁移到 `pkg/codexsdk/codex` |
| `pkg/codexsdk/**` | ✓ 写 | 目标目录（含 service/consumer 分层） |
| 其余目录 | R | 非本轮范围 |

## 阶段门禁（必须）

- P7 前门禁（P6-lite）:
  - `go build ./...`
  - `go vet ./...`
  - `go test ./...`
  - 依赖边界检查:
    - `pkg/toolsdk/tools` 不 import `internal/executor|runner|service|store`
    - `pkg/diffsdk/difftracker` 与 `pkg/toolsdk/lsp` 不 import 任何 `internal/*`
    - `internal/bus` 不 import `internal/codex`
- P7 完成门禁:
  - `internal/agentcore` 与 `internal/codex` 目录已迁移移除
  - 无残留 import `github.com/multi-agent/go-agent-v2/internal/(agentcore|codex)`
  - `codexsdk` 分层门禁通过（`service/consumer` 目录存在、非空实现、依赖方向正确）
  - `go build ./... && go test ./...` 通过

## 执行与回滚规范

- 当前仅剩 P7，默认串行执行。
- 推荐在独立 worktree 执行，避免与其他进行中的改动互相污染：
  ```bash
  git worktree add ../go-agent-v2-p7 -b codex/p7-finish
  ```
- 每个子步骤结束创建 checkpoint 提交（按文档里的分步 pathspec 执行）。
- 失败恢复：
  ```bash
  git switch -c recover-p7 <checkpoint-commit>
  ```

## 验证命令（每步骤必跑）

```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2
go build ./...
go vet ./...
go test ./...
```
