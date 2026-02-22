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
| `DYN_TOOL_ROUTING_MODE` | 动态工具路由模式 (`legacy`/`v2`) | `legacy` |
| `DYN_TOOL_ROUTER_MODEL` | 路由小模型名称（为空=仅规则路由） | — |
| `DYN_TOOL_ROUTER_BASE_URL` | 路由模型网关（支持本地 OpenAI 兼容地址） | — |
| `DISABLE_OFFLINE_52_METHODS` | 下线 52 个低频 RPC 入口（`1`=下线，`0`=回滚恢复） | `1` |

完整列表见 `internal/config/config.go`。

回滚方式：将 `DISABLE_OFFLINE_52_METHODS=0` 后重启服务，即可恢复这 52 个入口注册。

## Make Targets

| 命令 | 说明 |
| :--- | :--- |
| `make build` | 编译所有包 |
| `make run` | 启动 app-server |
| `make test` | 运行单元测试 (含 race detector) |
| `make test-e2e` | 运行 E2E 测试 (需先启动 app-server) |
| `make vet` | 静态分析 |
| `make fmt` | 格式化代码 |
| `make ui-cover-build` | 构建带覆盖率插桩的 `agent-terminal` |
| `make ui-cover-run` | 启动插桩 UI（默认 `--debug`） |
| `make ui-cover-report` | 生成触发/未触发方法清单 |

## 业务流触发方法审查（覆盖率）

用于“手工跑一轮真实业务流后，区分被触发/未触发方法”：

### 1) UI 路径（agent-terminal）

```bash
# 1) 构建带覆盖率插桩的 UI 二进制
make ui-cover-build

# 2) 启动 UI（会写入 .tmp/ui-cover）
make ui-cover-run
# 手工执行一轮 UI 功能后退出程序

# 3) 生成报告
make ui-cover-report
```

输出文件：

- `.tmp/ui-cover-summary.txt`：完整函数覆盖率汇总
- `.tmp/ui-triggered.txt`：覆盖率 > 0%（本次 UI 流程触发）
- `.tmp/ui-untriggered.txt`：覆盖率 = 0%（本次 UI 流程未触发）

### 2) API 路径（app-server）

```bash
# 1) 构建带覆盖率插桩的 app-server
TARGET=app-server scripts/ui-coverage.sh build

# 2) 启动 app-server（会写入 .tmp/app-cover）
TARGET=app-server scripts/ui-coverage.sh run --listen ws://127.0.0.1:4500
# 手工执行一轮业务（桌面端、rpc-test、或你的真实客户端）

# 3) 生成报告
TARGET=app-server scripts/ui-coverage.sh report
```

输出文件：

- `.tmp/app-cover-summary.txt`：完整函数覆盖率汇总
- `.tmp/app-triggered.txt`：覆盖率 > 0%（本次业务流触发）
- `.tmp/app-untriggered.txt`：覆盖率 = 0%（本次业务流未触发）

说明：`未触发` 代表“本次采样路径未覆盖”，不等于“可直接删除”，建议结合多场景采样与调用链确认再判定是否废弃。

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
