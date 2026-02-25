# 数据流与通信架构

## 1. 请求处理链路

### 1.1 桌面端 (agent-terminal) 完整链路

> 以 Codex CLI 为例 (其他 CLI 后端通过 `agentcore.Client` 接口适配)

```mermaid
sequenceDiagram
    participant UI as WebView 前端
    participant Wails as Wails v3 Bridge
    participant API as apiserver.Server
    participant Runner as runner.AgentManager
    participant AC as agentcore.Client
    participant CLI as Codex CLI (适配器)

    UI->>Wails: 用户请求({prompt})
    Wails->>API: WebSocket Request
    API->>Runner: Launch(id, prompt, tools)
    Runner->>AC: SpawnAndConnect()
    AC->>CLI: 启动 CLI 子进程
    
    CLI-->>AC: events (CLI 专属协议)
    AC-->>Runner: EventHandler(Event) → CLI 无关事件
    Runner-->>API: notification broadcast
    API-->>Wails: WebSocket Notification
    Wails-->>UI: WebView update
```

### 1.2 app-server (WebSocket 客户端) 链路

```mermaid
sequenceDiagram
    participant Client as WebSocket 客户端
    participant API as apiserver.Server
    participant Conn as server_conn
    participant Method as methods_*.go
    participant Store as store.*Store

    Client->>API: WebSocket 连接
    API->>Conn: handleConnection()
    
    Client->>Conn: Request
    Conn->>Method: dispatch(method, params)
    Method->>Store: DB 操作
    Store-->>Method: 结果
    Method-->>Conn: Response
    Conn-->>Client: WebSocket 消息
```

---

## 2. 事件流架构

### 2.1 Agent 事件传播

```
CLI 子进程 (Codex / Claude Code / Gemini / OpenCode)
    │ 各 CLI 专属协议 (如 Codex 用 JSON-RPC 2.0)
    ▼
agentcore.Client 实现 (适配器)
    │ EventHandler → CLI 无关事件 (agentcore.Event)
    ▼
runner.AgentManager
    │ onEvent callback
    ▼
apiserver.Server
    │
    ├── uistate.RuntimeManager  → UI 状态更新
    │       │
    │       └── runtime_event_handlers.go → 事件规范化
    │
    ├── bus.MessageBus → 进程内发布
    │       │
    │       └── Subscriber channels → dashboard / orchestrator
    │
    └── WebSocket connections → 广播 JSON-RPC Notification
            │
            └── 前端 WebView / 外部客户端
```

### 2.2 事件类型

| 事件类型 | 来源 | 说明 |
| :--- | :--- | :--- |
| `agent_message_delta` | Codex CLI | LLM 流式输出片段 |
| `agent_reasoning_delta` | Codex CLI | 推理过程 |
| `agent_tool_call` | Codex CLI | 工具调用请求 |
| `agent_tool_result` | 动态工具 | 工具执行结果返回 |
| `approval_requested` | Codex CLI | 命令审批请求 |
| `turn_complete` | Codex CLI | 轮次完成 |
| `ui/state/changed` | uistate | UI 状态变更 (节流 500ms) |

---

## 3. 消息总线 (bus)

### 3.1 Topic 路由

```
发布消息 topic = "agent.a0.output"

匹配规则 (前缀匹配):
  ✓ 订阅 "agent.a0"     → 匹配 (前缀)
  ✓ 订阅 "agent."       → 匹配 (前缀)
  ✓ 订阅 "*"            → 匹配 (广播)
  ✗ 订阅 "agent.a1"     → 不匹配
  ✗ 订阅 "system"       → 不匹配
```

### 3.2 两层通信架构

```mermaid
graph LR
    subgraph "进程内 (MessageBus)"
        P["Publish"] --> S1["Subscriber A"]
        P --> S2["Subscriber B"]
        P --> S3["Subscriber *"]
    end
    
    subgraph "跨 Agent (Router)"
        R["Router"] --> |"查 agent_threads"| DB["PostgreSQL"]
        R --> |"HTTP/WS 直连"| A1["Agent #1"]
        R --> |"HTTP/WS 直连"| A2["Agent #2"]
    end
    
    P -.-> R
```

---

## 4. 动态工具调度流

```mermaid
sequenceDiagram
    participant Agent as Codex Agent
    participant API as apiserver
    participant TA as tooladapter
    participant Handler as tool handler

    Agent->>API: tool_call(name, args)
    API->>TA: LookupRuntimeTool(name)
    TA-->>API: handler function
    API->>Handler: handler(ToolCallContext, args)
    
    alt LSP 工具
        Handler->>Handler: lsp.Manager 调用
    else code_run
        Handler->>Handler: executor.CodeRunner
    else 编排工具
        Handler->>Handler: runner.AgentManager
    else 资源工具
        Handler->>Handler: store.*Store
    end
    
    Handler-->>API: result string
    API-->>Agent: tool_result(result)
```

---

## 5. 审批流

```mermaid
sequenceDiagram
    participant Agent as Codex Agent
    participant API as apiserver
    participant UI as 前端/客户端
    
    Agent->>API: approval_requested(command, isDangerous)
    API->>UI: notification: approval/requested
    
    UI->>API: JSON-RPC: approval/resolve({approved: true})
    API-->>Agent: approval result → 继续执行
```

---

## 6. Dashboard SSE 推送

```mermaid
sequenceDiagram
    participant Browser as Dashboard 浏览器
    participant Dash as dashboard.Server
    participant BUS as bus.MessageBus

    Browser->>Dash: GET /events (SSE)
    Dash->>BUS: Subscribe("*")
    
    loop 每个事件
        BUS-->>Dash: Message
        Dash-->>Browser: SSE event
    end
```

Dashboard 同步间隔由 `DASHBOARD_SSE_SYNC_SEC` 控制 (默认 5 秒)。
