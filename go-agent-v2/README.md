# go-agent-v2

多 Agent 编排系统的 Go 后端服务。

## 先决条件

- **Go** ≥ 1.25
- **PostgreSQL** ≥ 14 (用于持久化)
- **Docker** (可选, 用于容器化部署)

## 快速启动

```bash
# 1. 复制配置
cp ../.env.example ../.env
# 编辑 .env 填写 OPENAI_API_KEY、POSTGRES_CONNECTION_STRING 等

# 2. 运行数据库迁移
go run ./cmd/migrate/

# 3. 启动 app-server
make run
```

## 环境变量

参见 [.env.example](../.env.example)。关键变量：

| 变量 | 说明 | 默认值 |
| :--- | :--- | :--- |
| `OPENAI_API_KEY` | OpenAI API 密钥 | — |
| `POSTGRES_CONNECTION_STRING` | PG 连接串 | — |
| `LOG_LEVEL` | 日志级别 | `INFO` |
| `LLM_MODEL` | 模型名称 | `gpt-4o` |

完整列表见 `internal/config/config.go`。

## Make Targets

| 命令 | 说明 |
| :--- | :--- |
| `make build` | 编译所有包 |
| `make run` | 启动 app-server |
| `make test` | 运行单元测试 (含 race detector) |
| `make test-e2e` | 运行 E2E 测试 (需先启动 app-server) |
| `make vet` | 静态分析 |
| `make fmt` | 格式化代码 |

## 架构概述

```
cmd/
├── agent-terminal/   # Wails 桌面端入口
├── app-server/       # HTTP/WS 后端入口 (不含 codex)
├── server/           # 独立 app-server
├── mcp-server/       # MCP 协议服务器
├── migrate/          # 数据库迁移工具
└── rpc-test/         # E2E 测试客户端 (需 -tags=e2e)

internal/
├── apiserver/        # JSON-RPC WebSocket 服务器
├── bus/              # 事件总线
├── codex/            # Codex 进程管理
├── config/           # 配置加载
├── store/            # 数据库 Store 层
├── monitor/          # Agent 巡检
├── orchestrator/     # 多 Agent 编排
├── runner/           # Agent 进程运行器
└── lsp/              # LSP 集成

pkg/
├── logger/           # 结构化日志 (slog)
├── util/             # 通用工具
└── errors/           # 错误定义
```

## Docker

```bash
docker build -t go-agent-v2 .
docker run -p 4500:4500 --env-file ../.env go-agent-v2
```
