# 构建、部署与运维指南

## 1. 本地开发

### 1.1 先决条件

| 依赖 | 版本要求 |
| :--- | :--- |
| Go | ≥ 1.25 |
| PostgreSQL | ≥ 14 |
| Codex CLI | 已安装 (Node.js shim 或 Rust 二进制) |
| Docker | 可选 (容器化部署) |

### 1.2 首次安装

```bash
# 1. 复制配置文件
cp ../.env.example ../.env

# 2. 编辑 .env (必填)
#    - OPENAI_API_KEY
#    - POSTGRES_CONNECTION_STRING

# 3. macOS CGO 兼容 (仅首次)
make setup-cgo

# 4. 数据库迁移
go run ./cmd/migrate/

# 5. 启动
make run
```

### 1.3 Make Targets

| 命令 | 说明 |
| :--- | :--- |
| `make build` | 编译所有包 |
| `make run` | 启动 app-server (`cmd/server/main.go`) |
| `make mcp` | 启动 MCP 协议服务器 |
| `make test` | 单元测试 (含 race detector) |
| `make test-e2e` | E2E 测试 (需先启动 app-server) |
| `make vet` | 静态分析 |
| `make fmt` | `goimports` 格式化 |
| `make protocol-sync-check` | 协议方法同步检查 |
| `make ui-cover-build` | 构建带覆盖率插桩的 agent-terminal |
| `make ui-cover-run` | 启动插桩 UI |
| `make ui-cover-report` | 生成覆盖率报告 |

---

## 2. 环境变量

### 2.1 必填项

| 变量 | 说明 |
| :--- | :--- |
| `OPENAI_API_KEY` | OpenAI API 密钥 |
| `POSTGRES_CONNECTION_STRING` | PostgreSQL 连接串 |

### 2.2 LLM 配置

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `LLM_MODEL` | `gpt-4o` | 模型名称 |
| `LLM_TEMPERATURE` | `0.7` | 温度 (≥ 0) |
| `LLM_TIMEOUT` | `120` | 超时秒数 |
| `LLM_MAX_RETRIES` | `3` | 最大重试次数 |
| `OPENAI_BASE_URL` | — | 自定义 API 地址 |

### 2.3 动态工具路由

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `DYN_TOOL_ROUTING_MODE` | `legacy` | 路由模式 (`legacy` / `v2`) |
| `DYN_TOOL_ROUTER_MODEL` | — | 路由小模型 (空=仅规则路由) |
| `DYN_TOOL_ROUTER_BASE_URL` | — | 路由模型网关地址 |
| `DYN_TOOL_ROUTER_API_KEY` | — | 路由模型 API Key |
| `DYN_TOOL_ROUTER_CONFIDENCE_THRESHOLD` | `0.65` | 置信度阈值 |
| `DYN_TOOL_ROUTER_TIMEOUT_SEC` | `8` | 路由超时 |

### 2.4 Gateway 配置

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `GATEWAY_TIMEOUT` | `240` | Gateway 超时秒数 |
| `GATEWAY_MAX_ATTEMPTS` | `2` | 最大尝试次数 |
| `GATEWAY_MIN_QUALITY_SCORE` | `25` | 最低质量分 |
| `COMMAND_CARD_TIMEOUT_SEC` | `240` | 命令卡执行超时 |

### 2.5 数据库连接池

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `POSTGRES_SCHEMA` | `public` | Schema |
| `POSTGRES_POOL_MIN_SIZE` | `1` | 最小连接数 |
| `POSTGRES_POOL_MAX_SIZE` | `10` | 最大连接数 |
| `POSTGRES_POOL_TIMEOUT_SEC` | `10` | 连接超时 |

### 2.6 运行时开关

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `LOG_LEVEL` | `INFO` | 日志级别 |
| `GIN_MODE` | `release` | Gin 模式 |
| `DISABLE_OFFLINE_52_METHODS` | `true` | 下线低频 RPC 方法 |
| `ACP_BUS_SINGLETON_ENABLED` | `false` | 总线单例模式 |
| `STALL_THRESHOLD_SEC` | `480` | Stall 检测阈值 |
| `STALL_HEARTBEAT_SEC` | `300` | 保活心跳间隔 |

---

## 3. Docker 部署

### 3.1 构建镜像

```bash
docker build -t go-agent-v2 .
```

### 3.2 运行容器

```bash
docker run -p 4500:4500 --env-file ../.env go-agent-v2
```

### 3.3 Dockerfile 分析

```dockerfile
# 多阶段构建
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /app/server ./cmd/server/main.go

FROM alpine:latest
COPY --from=builder /app/server /app/server
EXPOSE 4500
CMD ["/app/server"]
```

---

## 4. 入口程序对比

| 入口 | 命令 | 用途 | 包含 UI |
| :--- | :--- | :--- | :--- |
| server | `make run` | 生产/开发 app-server | ✗ |
| app-server | `go run ./cmd/app-server/` | 独立 app-server | ✗ |
| agent-terminal | Wails 构建 | 桌面客户端 | ✓ (WebView) |
| mcp-server | `make mcp` | MCP 协议服务 | ✗ |
| migrate | `go run ./cmd/migrate/` | 数据库迁移 | ✗ |

---

## 5. 覆盖率分析 (业务流审查)

### 5.1 UI 路径

```bash
make ui-cover-build    # 构建插桩二进制
make ui-cover-run      # 启动 UI (手工操作后退出)
make ui-cover-report   # 生成报告
```

输出：
- `.tmp/ui-cover-summary.txt` — 函数覆盖率汇总
- `.tmp/ui-triggered.txt` — 已触发方法
- `.tmp/ui-untriggered.txt` — 未触发方法

### 5.2 API 路径

```bash
make app-cover-build
make app-cover-run     # 启动后执行业务
make app-cover-report
```

---

## 6. 端口分配

| 服务 | 默认端口 | 说明 |
| :--- | :--- | :--- |
| app-server | 4500 | WebSocket JSON-RPC |
| Agent 子进程 | 19836+ | 自动分配 (basePort 起步) |
| Dashboard | Gin 路由 | 与 app-server 同端口 |

Agent 子进程端口分配策略：从 `basePort` (19836) 开始，逐个探测空闲端口，最多尝试 20 个连续端口。

---

## 7. 回滚策略

### 7.1 52 个低频 RPC 方法

```bash
# 回滚 (恢复方法注册)
DISABLE_OFFLINE_52_METHODS=0  # 重启后生效
```

### 7.2 迁移回滚

当前迁移不支持自动回滚。如需回滚，手动执行反向 SQL：

```bash
# 设置 MIGRATION_NON_FATAL=true 允许迁移失败不阻塞启动
MIGRATION_NON_FATAL=true
```
