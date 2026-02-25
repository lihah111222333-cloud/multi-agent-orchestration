# 多 Agent 编排系统 — 架构文档

> 本文档集描述 `go-agent-v2` 项目的整体架构、核心模块、数据流与部署方式。
>
> 系统通过 `agentcore.Client` 抽象接口实现多 CLI 适配，当前已实现 Codex 适配器，可扩展接入 Claude Code、Gemini CLI、OpenCode 等 CLI 后端。

## 目录

| 文档 | 说明 |
| :--- | :--- |
| [architecture-overview.md](./architecture-overview.md) | 系统架构总览、分层设计与核心依赖 |
| [module-reference.md](./module-reference.md) | 所有 internal/ 子包的职责说明与接口清单 |
| [frontend.md](./frontend.md) | 前端架构、组件、状态管理与 Wails 桥接 |
| [data-flow.md](./data-flow.md) | 请求链路、事件流与消息总线数据流图 |
| [database-schema.md](./database-schema.md) | 数据库表结构、迁移策略与 Store 层设计 |
| [deployment.md](./deployment.md) | 构建、部署、环境变量与 Docker 容器化指南 |

## 技术栈

| 分类 | 技术选型 |
| :--- | :--- |
| 语言 | Go 1.25.6 |
| 桌面 UI | Wails v3 (WebView) |
| 通信层 | WebSocket (Gorilla) |
| 数据库 | PostgreSQL ≥ 14 (pgx driver) |
| HTTP 框架 | Gin (Dashboard SSE) |
| Agent 抽象 | `agentcore.Client` 接口 (CLI 无关) |
| 已实现适配 | Codex CLI (`codexadapter`) |
| 可扩展适配 | Claude Code / Gemini CLI / OpenCode 等 |
| 代码智能 | LSP (Language Server Protocol) |
| 工具协议 | MCP (Model Context Protocol) |

## 快速入口

```bash
# 克隆 & 启动
cp ../.env.example ../.env   # 填写 API Key 与 PG 连接串
go run ./cmd/migrate/        # 数据库迁移
make run                     # 启动 app-server
```

---

*生成时间: 2026-02-25 · 基于 commit HEAD*
