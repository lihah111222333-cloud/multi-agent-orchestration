# Codex 职责隔离 — 交接包目录

本目录存放 `codex-isolation` 工作流的阶段产物。

## 文件规范

每阶段（P0-P4）提交：

| 文件 | 内容 |
|---|---|
| `pN.md` | 阶段报告（目标、变更摘要、风险、阻塞） |
| `pN.checks.log` | 验证命令输出 |
| `pN.files.txt` | 变更/新增文件清单 |
| `LATEST.md` | 全局进度指针 |
| `pN.blockers.md` | 阻塞说明（仅 `status: blocked` 时必需） |

说明：

1. `P0` 为规划阶段，可仅维护 `LATEST.md`。
2. `P1-P4` 必须提交 `pN.md / pN.checks.log / pN.files.txt`。

## `LATEST.md` 状态规范

`LATEST.md` front matter 必须包含以下字段：

1. `current_phase`: `P0/P1/P2/P3/P4`
2. `status`: `in_progress/done/blocked`
3. `next_phase`: `P1/P2/P3/P4/complete`
4. `updated_at`: ISO-8601（含时区）
5. `owner`: 当前责任 Agent

状态迁移规则：

1. 阶段开始执行：`current_phase=Pn`，`status=in_progress`，`next_phase=Pn`
2. 阶段完成并交接：`current_phase=Pn`，`status=done`，`next_phase=P(n+1)`；`P4` 完成时 `next_phase=complete`
3. 阶段阻塞：`current_phase=Pn`，`status=blocked`，`next_phase=Pn`，并新增 `pN.blockers.md`
4. 规划收口：`P0` 结束必须写为 `current_phase=P0`，`status=done`，`next_phase=P1`
5. `status` 仅描述 `current_phase`，禁止在 `LATEST.md` 使用 `pending/待执行/未开始` 等非枚举值

## 阶段概览（子包落地版）

| 阶段 | 范围 | 主题主链 | 阶段位置 |
|---|---|---|---|
| P0 | 基线确认 + LSP 快照 | 是 | 已完成（当前快照） |
| Pre-P1 | 测试基线建立（重构护栏） | 是 | 后续阶段（未到达） |
| P1 | apiserver 同包符号聚合（codex/generic） | 是 | 后续阶段（未到达） |
| P2 | LSP 全量 handler 迁移 + 诊断访问收敛 | 是 | 后续阶段（未到达） |
| P3 | `codexadapter` + `commonadapter` 子包落地 | 是 | 后续阶段（未到达） |
| P4 | 全量验收 + CLI 适配准备度评估 | 是 | 后续阶段（未到达） |

## 主题通过条件

`P1 + P2 + P3 + P4` 全部 `status=done` 且 `LATEST.md` 写为 `next_phase: complete`，即判定本工作流主题达标。
