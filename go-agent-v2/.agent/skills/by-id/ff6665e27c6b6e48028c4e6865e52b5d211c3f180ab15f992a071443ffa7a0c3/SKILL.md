---
name: WebSocket 实时通信
description: WebSocket 服务端架构与实时行情推送最佳实践，涵盖 Hub 模式、连接管理、消息订阅和性能优化。
tags: [websocket, realtime, hub, gorilla, 实时通信, 推送, 行情, Go, 连接管理]
---

# WebSocket 实时通信

适用于 Go 后端实现实时数据推送的规范指南。

## 何时使用

在以下场景使用此技能：

- 实现实时行情推送
- 设计 WebSocket 服务架构
- 管理客户端连接
- 实现消息订阅机制
- 优化推送性能

---

## 第一部分：Hub 架构模式

```go
// Hub 管理所有客户端连接
type Hub struct {
    clients    map[*Client]bool
    broadcast  chan []byte
    register   chan *Client
    unregister chan *Client
    mu         sync.RWMutex
}

func NewHub() *Hub {
    return &Hub{
        clients:    make(map[*Client]bool),
        broadcast:  make(chan []byte, 256),
        register:   make(chan *Client),
        unregister: make(chan *Client),
    }
}

func (h *Hub) Run() {
    for {
        select {
        case client := <-h.register:
            h.mu.Lock()
            h.clients[client] = true
            h.mu.Unlock()
            
        case client := <-h.unregister:
            h.mu.Lock()
            if _, ok := h.clients[client]; ok {
                delete(h.clients, client)
                close(client.send)
            }
            h.mu.Unlock()
            
        case message := <-h.broadcast:
            h.mu.RLock()
            for client := range h.clients {
                select {
                case client.send <- message:
                default:
                    close(client.send)
                    delete(h.clients, client)
                }
            }
            h.mu.RUnlock()
        }
    }
}
```

---

## 第二部分：客户端连接

```go
type Client struct {
    hub     *Hub
    conn    *websocket.Conn
    send    chan []byte
    userID  string
    symbols map[string]bool  // 订阅的交易对
}

func (c *Client) ReadPump() {
    defer func() {
        c.hub.unregister <- c
        c.conn.Close()
    }()
    
    c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
    c.conn.SetPongHandler(func(string) error {
        c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
        return nil
    })
    
    for {
        _, message, err := c.conn.ReadMessage()
        if err != nil {
            break
        }
        c.handleMessage(message)
    }
}

func (c *Client) WritePump() {
    ticker := time.NewTicker(30 * time.Second)
    defer func() {
        ticker.Stop()
        c.conn.Close()
    }()
    
    for {
        select {
        case message, ok := <-c.send:
            if !ok {
                c.conn.WriteMessage(websocket.CloseMessage, []byte{})
                return
            }
            c.conn.WriteMessage(websocket.TextMessage, message)
            
        case <-ticker.C:
            c.conn.WriteMessage(websocket.PingMessage, nil)
        }
    }
}
```

---

## 第三部分：消息订阅

```go
// 订阅消息格式
type SubscribeMsg struct {
    Action  string   `json:"action"`  // subscribe, unsubscribe
    Channel string   `json:"channel"` // ticker, depth, trade
    Symbols []string `json:"symbols"`
}

func (c *Client) handleMessage(data []byte) {
    var msg SubscribeMsg
    if err := json.Unmarshal(data, &msg); err != nil {
        return
    }
    
    switch msg.Action {
    case "subscribe":
        for _, symbol := range msg.Symbols {
            c.symbols[symbol] = true
        }
    case "unsubscribe":
        for _, symbol := range msg.Symbols {
            delete(c.symbols, symbol)
        }
    }
}

// 定向推送
func (h *Hub) BroadcastToSymbol(symbol string, data []byte) {
    h.mu.RLock()
    defer h.mu.RUnlock()
    
    for client := range h.clients {
        if client.symbols[symbol] {
            select {
            case client.send <- data:
            default:
            }
        }
    }
}
```

---

## 第四部分：行情推送

```go
// 行情数据结构
type TickerData struct {
    Symbol    string  `json:"s"`
    Price     string  `json:"p"`
    Volume    string  `json:"v"`
    Timestamp int64   `json:"t"`
}

// 批量推送优化
func (h *Hub) StartTickerBroadcast(tickerChan <-chan *TickerData) {
    batch := make(map[string]*TickerData)
    ticker := time.NewTicker(100 * time.Millisecond)
    
    for {
        select {
        case data := <-tickerChan:
            batch[data.Symbol] = data
            
        case <-ticker.C:
            if len(batch) > 0 {
                for symbol, data := range batch {
                    msg, _ := json.Marshal(data)
                    h.BroadcastToSymbol(symbol, msg)
                }
                batch = make(map[string]*TickerData)
            }
        }
    }
}
```

---

## 第五部分：HTTP 升级

```go
var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        return true  // 生产环境需验证
    },
}

func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    
    client := &Client{
        hub:     hub,
        conn:    conn,
        send:    make(chan []byte, 256),
        symbols: make(map[string]bool),
    }
    
    hub.register <- client
    
    go client.WritePump()
    go client.ReadPump()
}
```

---

## 审查清单

- [ ] Hub 使用 goroutine 安全的 map 操作
- [ ] 设置读写超时和心跳
- [ ] send channel 有缓冲避免阻塞
- [ ] 连接关闭时正确清理资源
- [ ] 批量推送减少发送频率


---

## ⚠️ 强制输出 Token 空间

> **重要规则**：使用此技能时，必须在每次重要输出前检查上下文空间。

### 输出规范

所有对话回复内容都要输出

### 输出格式

```
📊 剩余上下文空间: ~{百分比}%
```

### 告警与自动保存

**当剩余上下文空间 ≤ 30%（即已使用 ≥ 70%）时，必须执行：**

1. **立即暂停当前工作**
2. **保存工作进度**：创建 `.agent/workflows/checkpoint-{timestamp}.md`
3. **通知用户**：
   ```
   ⚠️ 上下文空间即将耗尽 (剩余 ~{百分比}%)
   📋 工作进度已保存至: .agent/workflows/checkpoint-{timestamp}.md
   请检查后决定是否继续或开启新对话
   ```
