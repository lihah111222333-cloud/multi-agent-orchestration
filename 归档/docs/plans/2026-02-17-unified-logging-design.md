# 统一日志接入设计 (v2)

统一 4 类日志源 (UI / codex / system / agent 聚合) 到 `system_logs` 表，通过 slog `DBHandler` 异步批量写入 PG。

## 架构

```
桌面UI → slog("ui")         ──┐
codex stderr → 行解析 → slog  ──┤→ MultiHandler ──→ DBHandler → system_logs
codex event → slog            ──┤     ↓
Go 后端 → slog("system")     ──┘   TextHandler → stdout (保留)
```

**"agent 日志"** 不是独立源，是按 `agent_id` 聚合 system + codex + ui 的查询视图。

## 表结构 (15 列)

在 `system_logs` 上 `ALTER TABLE ADD` 9 列:

| # | 列 | 类型 | 现有/新增 | 说明 | 前端用途 |
|---|-----|------|----------|------|---------|
| 1 | `id` | BIGSERIAL PK | 现有 | — | — |
| 2 | `ts` | TIMESTAMPTZ | 现有 | — | 时间线 |
| 3 | `level` | TEXT | 现有 | INFO/WARN/ERROR/DEBUG | 级别过滤 |
| 4 | `logger` | TEXT | 现有 | 保留兼容旧数据 | — |
| 5 | `message` | TEXT | 现有 | 日志消息 | 全文搜索 |
| 6 | `raw` | TEXT | 现有 | 原始文本 | 详情展开 |
| 7 | `source` | TEXT | **新增** | `system`/`codex`/`ui` | 标签页 |
| 8 | `component` | TEXT | **新增** | 细分组件 (见下) | 下拉过滤 |
| 9 | `agent_id` | TEXT | **新增** | 关联 agent | agent 视图聚合 |
| 10 | `thread_id` | TEXT | **新增** | 关联 thread | 对话追踪 |
| 11 | `trace_id` | TEXT | **新增** | 分布式追踪 | 链路追踪 |
| 12 | `event_type` | TEXT | **新增** | 结构化事件类型 | 面板分类 |
| 13 | `tool_name` | TEXT | **新增** | LSP/编排工具名 | 工具筛选/统计 |
| 14 | `duration_ms` | INT | **新增** | 调用耗时 | 性能面板 |
| 15 | `extra` | JSONB | **新增** | 扩展字段 | 详情展开 |

新增列默认值均为空字符串/0/null，兼容旧数据。

### component 枚举值

| source | component |
|--------|-----------|
| `system` | `apiserver`, `runner`, `store`, `lsp`, `bus`, `monitor`, `config`, `mcp` |
| `codex` | `stderr`, `event`, `tool_call` |
| `ui` | `window`, `tray`, `dialog`, `ws` |

### 索引

```sql
CREATE INDEX idx_system_logs_source ON system_logs (source) WHERE source != '';
CREATE INDEX idx_system_logs_agent ON system_logs (agent_id) WHERE agent_id != '';
CREATE INDEX idx_system_logs_thread ON system_logs (thread_id) WHERE thread_id != '';
CREATE INDEX idx_system_logs_event ON system_logs (event_type) WHERE event_type != '';
CREATE INDEX idx_system_logs_tool ON system_logs (tool_name) WHERE tool_name != '';
```

## 核心组件

### 1. DBHandler (`pkg/logger/db_handler.go`)

```go
type DBHandler struct {
    pool  *pgxpool.Pool
    buf   chan LogEntry   // 容量 1024
    attrs []slog.Attr    // With() 固定字段
    group string
}
```

- 实现 `slog.Handler` 接口 (`Enabled`, `Handle`, `WithAttrs`, `WithGroup`)
- `Handle()` 从 slog.Record + attrs 提取 15 列 → 构造 `LogEntry` → 推入 chan
- 后台 goroutine 批量消费: 每 **100 条** 或 **500ms** 做一次批量 INSERT
- chan 满时 **drop oldest** (不阻塞主流程)
- `Shutdown()` 方法: flush 剩余日志 + 关闭 goroutine

### 2. MultiHandler (`pkg/logger/db_handler.go`)

```go
type MultiHandler struct {
    handlers []slog.Handler
}
```

将 TextHandler (stdout) + DBHandler 组合，每条日志双写。

### 3. AttachDBHandler (`pkg/logger/db_handler.go`)

> **修正 #5**: `logger.Init()` 在 `database.NewPool()` 之前调用，DBHandler 需要 pool。

```go
// AttachDBHandler 在 pool 初始化后调用，动态挂载 DBHandler。
// 启动前几条日志只写 stdout，pool ready 后开始双写。
func AttachDBHandler(pool *pgxpool.Pool)
```

调用时机:
```go
// cmd/server/main.go + cmd/desktop/main.go 都需要:
pool, err := database.NewPool(ctx, cfg)
// ...
logger.AttachDBHandler(pool) // ← pool ready 后
```

### 4. StderrCollector (`pkg/logger/stderr_collector.go`)

> **修正 #2/#3**: 两个 codex client 都要改，且 Stdout 保持原逻辑。

```go
// StderrCollector 实现 io.Writer，将 codex stderr 逐行转为 slog 日志。
type StderrCollector struct {
    agentID string
    logger  *slog.Logger
}
```

- 内部 goroutine + `bufio.Scanner` 逐行读
- 每行 → `slog.Info("codex stderr", "source", "codex", "component", "stderr", "agent_id", agentID)`
- **只挂 Stderr**。Stdout 保持原逻辑 (port==0 时用于捕获端口号)

### 5. Event Logger (在已有事件回调中加 slog)

codex 事件在 `server.go:480-509` 的 `onEvent` 回调中流转。在此添加:

```go
// 所有 codex 事件写入日志
slog.Info("codex event",
    "source", "codex",
    "component", "event",
    "agent_id", agentID,
    "event_type", event.Type,    // turn_started, agent_message_delta, etc.
    "thread_id", threadID,
)
```

`handleDynamicToolCall` 已有可观测性代码 (L801-806)，当前 key 用 `"agent"`, `"tool"`, `"elapsed_ms"`。
**统一改为 logger 常量:**
```go
slog.Info("dynamic-tool: completed",
    logger.FieldComponent, "tool_call",           // 统一用常量
    "source", "codex",
    logger.FieldAgentID, agentID,                  // "agent_id" 非 "agent"
    "tool_name", call.Tool,
    logger.FieldLatencyMS, elapsed.Milliseconds(), // "latency_ms" 非 "elapsed_ms"
    "event_type", "dynamic_tool_call",
    logger.FieldThreadID, proc.ThreadID,            // 从 proc 取真实 threadID
)
```

## 采集管道

| # | 来源 | 改动位置 | source | component |
|---|------|---------|--------|-----------|
| 1 | Go slog | `MultiHandler` 双写 | `system` | 从 slog attr 取 |
| 2 | codex stderr | `client.go` + `client_appserver.go` Stderr → `StderrCollector` | `codex` | `stderr` |
| 3 | codex events | `server.go` onEvent 回调 (L480-509) | `codex` | `event` / `tool_call` |
| 4 | 桌面 UI | UI 回调 → `slog("ui", ...)` | `ui` | 按操作细分 |

## 查询示例

```sql
-- agent 聚合视图
SELECT * FROM system_logs WHERE agent_id = 'agent-123' ORDER BY ts DESC;

-- LSP 调用
SELECT * FROM system_logs WHERE component = 'tool_call' AND tool_name LIKE 'lsp_%';

-- 慢工具 (>500ms)
SELECT * FROM system_logs WHERE component = 'tool_call' AND duration_ms > 500;

-- codex stderr 错误
SELECT * FROM system_logs WHERE source = 'codex' AND component = 'stderr' AND level = 'ERROR';

-- UI 错误
SELECT * FROM system_logs WHERE source = 'ui' AND level = 'ERROR';

-- 工具使用统计
SELECT tool_name, COUNT(*), AVG(duration_ms) FROM system_logs
WHERE component = 'tool_call' GROUP BY tool_name;
```

## 文件清单

| 文件 | 类型 | 内容 |
|------|------|------|
| `pkg/logger/db_handler.go` | NEW | DBHandler + MultiHandler + AttachDBHandler + LogEntry |
| `pkg/logger/stderr_collector.go` | NEW | codex stderr → slog 管道 |
| `db/migrations/0010_system_logs_v2.sql` | NEW | ALTER TABLE ADD 9 列 + 5 索引 |
| `internal/store/system_log.go` | MOD | 查询方法支持新字段过滤 |
| `internal/store/models.go` | MOD | SystemLog struct 加 9 字段 |
| `internal/codex/client.go` | MOD | L111: Stderr → StderrCollector |
| `internal/codex/client_appserver.go` | MOD | L145: Stderr → StderrCollector (Stdout 保持 Discard) |
| `internal/apiserver/server.go` | MOD | onEvent + handleDynamicToolCall 加结构化日志 |
| `cmd/server/main.go` | MOD | L26 后: `logger.AttachDBHandler(pool)` |
| `cmd/desktop/main.go` | MOD | L36 后: `logger.AttachDBHandler(pool)` |

## 注意事项

1. **Stdout 不动**: `client.go` 的 Stdout 在 port==0 时用于捕获端口，不能替换
6. **slog key 统一**: 所有 slog 调用改用 `logger.Field*` 常量 (`agent_id` 非 `agent`, `latency_ms` 非 `elapsed_ms`)
7. **threadID 来源**: event logger 中 `thread_id` 从 `proc.ThreadID` 获取，不从 payload (payload 的 threadId 实际是 agentID)
2. **init 时序**: `logger.Init()` (L24) → stdout only → `NewPool()` (L26) → `AttachDBHandler()` → 双写
3. **migration 编号**: 现有有两个 `0006_*`，migrator 按 `sort.Strings` 排序，`0010` 安全
4. **旧数据兼容**: 新增列全用默认值，现有 `SystemLogStore.List()` 不受影响
5. **性能**: DBHandler 异步批量写，chan 溢出 drop，不阻塞主流程

## 验证计划

1. **单元测试**: DBHandler 批量写入 + 溢出 drop + MultiHandler 双写
2. **集成测试**: 启动 server → 触发工具调用 → 查 system_logs 有记录
3. **E2E**: codex stderr 输出 → 查 DB 有对应 source=codex 记录
4. **性能**: 1000 条/秒写入 → 确认主流程 latency 不受影响
