# 多Agent编排 (Multi-Agent Orchestration)

基于 **LangGraph + MCP** 的动态多 Agent 编排系统。

## 架构

```
Master (LangGraph StateGraph)
├── Dispatcher (基于能力/依赖提示分配)
├── Gateway A → Agents (由 config.json 动态定义)
├── Gateway B → Agents (由 config.json 动态定义)
└── Gateway N → Agents (由 config.json 动态定义)
```

## 快速开始

### 1. 安装依赖

```bash
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
# 开发/测试依赖
pip install -r requirements-dev.txt
```

### 2. 配置环境

```bash
cp .env.example .env
# 编辑 .env 填入你的 LLM API Key
```

### 3. 运行

```bash
# 运行完整编排
python3 run.py "你的任务描述"

# 启动任意动态 Agent
python3 -m agents.runtime_agent --id agent_13 --name "自定义代理"

# 启动并启用插件
python3 -m agents.runtime_agent --id agent_13 --name "自定义代理" --plugins http_fetch,db_query
```

`config.json` 的 agent 节点支持 `plugins` 字段，例如：

```json
{
  "id": "agent_13",
  "name": "动态代理",
  "plugins": ["http_fetch", "db_query"]
}
```

## 项目结构

```
├── master.py          # Master 编排器 (LangGraph)
├── run.py             # CLI 入口
├── config/
│   └── settings.py    # 全局配置 + config.json 动态加载
├── config.json        # Gateway/Agent 架构定义（主控数量来源）
├── agents/
│   ├── base_agent.py      # Agent 基类
│   ├── factory.py         # Agent 工厂（工具函数生成）
│   ├── runtime_control.py # 单线程执行 + 周期 GC
│   ├── specs.py           # 默认 Agent 规格
│   └── runtime_agent.py   # 动态 Agent 运行入口
├── plugins/
│   ├── loader.py          # 插件扫描与加载
│   ├── http_fetch/        # 示例插件：HTTP 获取（受限模拟）
│   └── db_query/          # 示例插件：只读查询（受限模拟）
└── gateways/
    └── gateway.py     # Gateway (MCP Client + 路由)
```

## 技术栈

- **LangGraph** — 多 Agent 编排
- **MCP (Model Context Protocol)** — Agent 工具协议
- **langchain-mcp-adapters** — LangChain ↔ MCP 桥接

## iTerm 启动与 I/O

- 启动脚本：`scripts/iterm_launch_agents.py`
- 支持数量：`4/6/8/12`（最少 `4`）
- 布局模式：
  - `--layout panes`：同一页面等分（推荐观测）
  - `--layout tabs`：多标签
- 启动后会写入 `data/iterm_launch_state.json`（包含 `session_id` 映射）

示例：

```bash
# 自动决策数量 + 同页等分
python3 scripts/iterm_launch_agents.py --task "做一次多agent并发全链路压测" --layout panes

# 指定 4/6/8/12，按等分网格拉起
python3 scripts/iterm_launch_agents.py --tabs 4 --layout panes
python3 scripts/iterm_launch_agents.py --tabs 6 --layout panes
python3 scripts/iterm_launch_agents.py --tabs 8 --layout panes
python3 scripts/iterm_launch_agents.py --tabs 12 --layout panes

# 保留旧行为：多标签
python3 scripts/iterm_launch_agents.py --tabs 8 --layout tabs
```

I/O 脚本：`scripts/iterm_agent_io.py`

```bash
# 列出会话
python3 scripts/iterm_agent_io.py --action list

# 群发输入并读取输出
python3 scripts/iterm_agent_io.py --action send --all --text "echo hello" --lines 20

# 对指定 agent 输入/输出
python3 scripts/iterm_agent_io.py --action send --agent agent_01 --text "echo ping"
python3 scripts/iterm_agent_io.py --action read --agent agent_01 --lines 30
```

说明：
- 依赖 iTerm Python API（`iterm2`）
- 默认每个 pane/tab 启动 `python3 -m agents.runtime_agent ...`
- 可用 `--start-template` 自定义启动命令模板
- 为避免空白 shell 窗口，`--start-template` 禁止使用 shell-only 命令：`zsh/bash/sh/fish`
- 推荐使用可见前台程序，例如：`codex --no-alt-screen "..."`
- `agents/all_in_one.py` 已注册 iTerm MCP 工具：`iterm_list_sessions`、`iterm_send_input`、`iterm_read_output`

## Agent 运行时（单线程 + 周期 GC）

- 每个 Agent 进程使用单 worker 线程执行工具逻辑（串行，避免并发放大内存）
- 默认开启后台 GC 线程，周期执行 `gc.collect(2)`
- 可选内存告警（仅日志，不会强杀进程）

环境变量：
- `AGENT_GC_ENABLED`：是否启用周期 GC（默认 `1`）
- `AGENT_GC_INTERVAL_SEC`：GC 间隔秒数（默认 `30`）
- `AGENT_GC_GENERATION`：`gc.collect(gen)` 的代际参数（默认 `2`）
- `AGENT_MEMORY_WARN_MB`：内存告警阈值 MB（默认 `0`，关闭）

## 拓扑审批流

- Master 在执行任务时可自动提出拓扑草案（默认开启）
- 草案进入待审批列表，不会立即生效
- 人工在 Dashboard 的 `Topology Approvals` 区域批准后，才写入 `config.json`
- 审批写入前支持 `config.json` 备份（`CONFIG_BACKUP_ENABLED`、`CONFIG_BACKUP_KEEP`）
- 审批默认过期时间为 `120s`，可配置：`TOPOLOGY_APPROVAL_TTL_SEC`
- 已完成审批单支持按天归档：`TOPOLOGY_APPROVAL_ARCHIVE_DAYS`
- 可通过环境变量关闭自动提案：`TOPOLOGY_PROPOSAL_ENABLED=0`


## PostgreSQL 持久化

系统已切换为 PostgreSQL 持久化（不再依赖本地 JSONL/JSON 文件作为主存储）：

- 审计日志：`audit_events`
- 系统日志：`system_logs`
- 拓扑审批：`topology_approvals`、`topology_approval_archives`
- 共享文件：`shared_files`
- Agent 交互：`agent_interactions`
- 提示词模板：`prompt_templates`
- 命令卡：`command_cards`

必需环境变量：

- `POSTGRES_CONNECTION_STRING`
- 可选：`POSTGRES_SCHEMA`（默认 `public`）
- 可选连接池：`POSTGRES_POOL_ENABLED`、`POSTGRES_POOL_MIN_SIZE`、`POSTGRES_POOL_MAX_SIZE`、`POSTGRES_POOL_TIMEOUT_SEC`
  - 默认开启；若未安装 `psycopg_pool`，自动降级为单连接模式

## 命令卡执行器

基于 `command_cards` + `command_card_runs` 实现命令模板渲染与执行流水：

- `prepare_command_card_run`：渲染命令并创建执行单
- 高风险（`high/critical`）默认进入待审批（写入 `agent_interactions`）
- `review_command_card_run`：人工批准/拒绝
- `execute_command_card_run`：执行并记录 `stdout/stderr/exit_code`
- `execute_command_card`：一键流程（准备 -> 可选自动审批 -> 执行）

对应 MCP 工具已在 `agents/all_in_one.py` 注册。

安全约束：
- 命令卡执行使用 `argv` 直连子进程，不通过 shell (`/bin/zsh -lc`)
- `db_query` 仅允许只读 `SELECT/CTE` 单语句
- `db_execute` 仅允许 DML（`INSERT/UPDATE/DELETE/MERGE`），拒绝 DDL/管理语句

Dashboard 新增 `命令卡` 页面：
- 浏览命令卡清单
- 提交执行（普通风险即时执行）
- 高风险进入待审批，支持在线批准/拒绝/执行
- 页面通过 SSE 实时同步执行流水

## Agent 数据表设计

为便于主 Agent/子 Agent 编排，新增三类核心表：

- `agent_interactions`：记录任务指令、消息链路、审核状态
- `prompt_templates`：存储可复用提示词模板（支持按 `agent_key/tool_name` 过滤）
- `command_cards`：存储命令卡模板与参数 schema、风险级别

`agents/all_in_one.py` 已暴露对应 MCP 工具（创建/查询/启停）。

## 日志与审计

- 全局运行日志写入 PostgreSQL `system_logs`
- 结构化审计日志写入 PostgreSQL `audit_events`
- Dashboard 的 `Logs` 区域并排展示 `Audit Logs` 与 `System Logs`
- `Audit Logs` 支持按 `event_type/action/result/actor/keyword` 过滤
- `System Logs` 支持按 `level/logger/keyword` 过滤，并按级别彩色标识
- `System Logs` 支持导出当前筛选结果（`ndjson`）
- 日志面板通过 SSE 实时同步（`/api/events/stream`），心跳周期可配置：`DASHBOARD_SSE_SYNC_SEC`
- 实时同步周期参数：`DASHBOARD_SSE_SYNC_SEC`
- 日志已落 PostgreSQL，不再依赖本地文件轮转参数

## 健壮性增强

- Gateway 返回结构化结果：`success/output/error/reason/attempts`
- Gateway 内置自动重试（默认最多 `2` 次，可配置 `GATEWAY_MAX_ATTEMPTS`）
- Dispatcher 在故障时进入降级分配模式，并在聚合报告中标注
- Dispatcher 注入 gateway/agent `capabilities` 与 `depends_on` 提示，提升分配可控性
- 聚合前进行结果质量筛选（`GATEWAY_MIN_QUALITY_SCORE`）和去重
- 每次执行使用拓扑快照，报告中显示 `topology_hash`

## Dashboard 运维

- 提供健康检查端点：`GET /health`
- 提供实时事件流端点：`GET /api/events/stream`（SSE）
- Dashboard 使用 `ThreadingHTTPServer`，避免单请求阻塞全局
