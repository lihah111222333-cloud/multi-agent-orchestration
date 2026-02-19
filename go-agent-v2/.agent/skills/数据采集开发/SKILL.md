---
name: 数据采集开发
description: 交易所数据采集与分发模块 (Spider) 开发指南，涵盖 HTTP/WebSocket 协议接入、数据标准化与工厂模式集成。
tags: [spider, exchange, websocket, binance, data, 采集, 爬虫]
---

# WJBoot 数据采集开发指南 (Spider)

> 🕷️ **核心职责**: 对接不同交易所 API，将异构数据清洗为标准化的 `Kline` (K线) 和 `Ticker` (行情) 数据。

## 何时使用

- 接入新的交易所 (如 Bybit, OKX)
- 修复数据源接口变更
- 优化采集性能 (WebSocket 调优)

---

## 第一部分：采集器架构

所有交易所适配器位于 `internal/spider/exchange/{exchange}/`，由统一的 Exchange 接口管理。

### 核心接口

```go
// internal/spider/exchange/interface.go
type Exchange interface {
    Name() string
    // 历史数据
    GetKlines(ctx context.Context, symbol, timeframe string, limit int) ([]common.Kline, error)
    // 实时流
    SubscribeKlines(ctx context.Context, symbols []string, handler func(*common.Kline)) error
    Close() error
}
```

### 目录结构

```
internal/spider/
├── common/           # 通用类型和工具
│   ├── types.go      # Kline, Ticker, Timeframe 等
│   └── utils.go      # 辅助函数
├── exchange/         # 交易所适配器
│   ├── interface.go  # Exchange 接口定义
│   └── binance/      # Binance 实现
│       ├── spot.go   # 现货
│       └── futures.go # 合约
├── miner/            # K线聚合器
│   ├── aggregator.go # 1m → 5m/15m/1h 聚合
│   └── miner.go      # 采集主入口
├── server/           # WebSocket Hub
│   ├── hub.go        # 消息分发中心
│   └── subscription.go # 订阅管理
└── storage/          # 数据存储
    ├── redis.go      # 订阅持久化
    └── timescale.go  # 历史数据存储
```

---

## 第二部分：实现一个新的 Exchange

以接入 `Bybit` 为例：

### 1. 创建目录与结构体

```go
// internal/spider/exchange/bybit/spot.go
type BybitSpot struct {
    client *http.Client
    ws     *websocket.Conn
}

func NewBybitSpot() *BybitSpot {
    return &BybitSpot{
        client: &http.Client{Timeout: 10 * time.Second},
    }
}

func (b *BybitSpot) Name() string {
    return "bybit"
}
```

### 2. 实现 REST 接口 (GetKlines)

重点在于**数据标准化**：必须将交易所返回的字符串/浮点数转换为 `decimal.Decimal`。

```go
func (b *BybitSpot) GetKlines(ctx context.Context, symbol, timeframe string, limit int) ([]common.Kline, error) {
    // 1. 调用 API
    resp := b.doRequest("/v5/market/kline", params)
    
    // 2. 清洗数据
    var klines []common.Kline
    for _, item := range resp.List {
        klines = append(klines, common.Kline{
            Exchange:  "bybit",
            Symbol:    symbol,
            Open:      decimal.RequireFromString(item[1]),
            // ...
        })
    }
    return klines, nil
}
```

### 3. 实现 WebSocket (SubscribeKlines)

WebSocket 需处理**心跳保活**与**断线重连**。

```go
func (b *BybitSpot) SubscribeKlines(ctx context.Context, symbols []string, handler func(*common.Kline)) error {
    // 1. 建立连接
    // 2. 发送订阅消息 {"op": "subscribe", "args": ["kline.1." + symbol]}
    // 3. 启动读取循环
    go func() {
        for {
            _, msg, err := b.ws.ReadMessage()
            if err != nil {
                // 断线重连
                continue
            }
            kline := b.parseWSMessage(msg)
            handler(kline)
        }
    }()
    return nil
}
```

---

## 第三部分：核心工具函数

### Decimal 解析
始终使用 `decimal` 包处理价格与数量，防止精度丢失。

```go
// internal/spider/common/utils.go
func ParseDecimal(v interface{}) decimal.Decimal {
    // 封装容错逻辑，处理 string/float64/nil
}
```

### 时间周期转换
交易所的 "1m", "1h" 需转换为标准 `time.Duration`。

```go
// internal/spider/common/types.go
func TimeframeToDuration(tf string) time.Duration {
    switch tf {
    case "1m": return time.Minute
    case "5m": return 5 * time.Minute
    // ...
    }
}
```

---

## 检查清单

- [ ] 是否处理了 HTTP 429 (Rate Limit)？
- [ ] WebSocket 是否有心跳机制 (Ping/Pong)？
- [ ] 价格字段是否使用了 Decimal？
- [ ] 错误日志是否包含具体的 API 响应？
