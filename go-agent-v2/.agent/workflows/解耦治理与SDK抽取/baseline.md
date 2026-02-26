# Baseline 2026-02-26 (P0v2 审计更新)

> P0v2 审计确认: P1/P1.5/P1.6/P2/P7 均已完成。剩余工作: P3/P4/P5/P6。

## SDK 层 (pkg/) — 已完成迁移

| 包 | 有效行数 |
|---|---:|
| pkg/toolsdk/lsp | 6,094 |
| pkg/codexsdk/codex | 3,775 |
| pkg/toolsdk/tools | 1,796 |
| pkg/toolsdk/tooladapter | 962 |
| pkg/logger | 581 |
| pkg/util | 365 |
| pkg/codexsdk/agentcore | 203 |
| pkg/errors | 69 |
| **pkg 合计** | **12,183** |

## 业务层 (internal/) — 待瘦身

| 包 | 有效行数 | 目标 |
|---|---:|---:|
| internal/apiserver/codexadapter | 6,480 | ~4,500 (P3) |
| internal/apiserver (顶层) | 5,939 | ~5,000 (P4) |
| internal/uistate | 3,271 | ~2,700 (P5) |
| internal/store | 2,012 | - |
| internal/service | 1,649 | - |
| internal/dashboard | 1,549 | +增长 (P4 接收) |
| internal/executor | 1,084 | - |
| internal/bus | 846 | - |
| internal/runner | 659 | - |
| internal/skills | 625 | - |
| internal/orchestrator | 545 | - |
| 其余小包 | 1,965 | - |
| **internal 合计** | **28,624** | **≤26,000** |

## 入口层 (cmd/)

| 包 | 有效行数 |
|---|---:|
| cmd/agent-terminal | 1,844 |
| 其余 4 cmd | 221 |
| **cmd 合计** | **2,065** |

## 全仓合计

| 范围 | 有效行数 |
|---|---:|
| 全仓生产代码 | 42,272 |
