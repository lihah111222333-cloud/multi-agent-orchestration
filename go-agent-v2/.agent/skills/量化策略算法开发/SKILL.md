---
name: WJBoot 量化策略开发
description: 专注于 WJBoot 量化引擎的策略研发指南，涵盖 Strategy 接口实现、Signal 生成、OnBar/OnTick 回调处理。
tags: [quant, strategy, trading, execution, algorithm, 量化, 策略, 回测]
---

# WJBoot 量化策略开发指南

> 📈 **核心定义**: 策略是交易系统的核心逻辑单元，负责接收市场数据 (`OnBar`/`OnTick`) 并生成交易信号。

## 核心接口 (`Strategy`)

所有策略必须实现 `internal/engine/core/types.go` 定义的 `Strategy` 接口：

```go
type Strategy interface {
    // 基础信息
    Name() string
    WarmupBars() int  // 返回策略需要的预热 K 线数量，0 = 使用引擎默认值

    // 生命周期回调
    OnInit(ctx StrategyContext) error   // 注意: 参数是 StrategyContext，非 context.Context
    OnStop(ctx context.Context) error

    // 数据驱动回调
    OnBar(ctx context.Context, bar *Bar) error
    OnTick(ctx context.Context, tick *Tick) error

    // 事件回调 (可选实现，返回 nil 表示不处理)
    OnOrderFill(ctx StrategyContext, order *Order) error        // 订单成交时触发
    OnPositionChange(ctx StrategyContext, pos *Position) error  // 持仓变化时触发
}
```

> [!IMPORTANT]
> `OnInit` 接收的是 `StrategyContext`（提供行情+交易能力），**不是** `context.Context`。
> `OnOrderFill` / `OnPositionChange` 是可选回调，返回 `nil` 即可跳过。

---

## 第一部分：编写一个新策略

### 1. 结构体定义

推荐将策略参数（Parameters）作为结构体字段，并在 `OnInit` 中初始化。

```go
package strategy

import (
    "context"
    "fmt"
    "github.com/shopspring/decimal"
    "github.com/quant-trading-system/wjboot/v2/internal/engine/core"
)

type SMACrossStrategy struct {
    name        string
    symbol      string
    shortWindow int
    longWindow  int

    // 运行时注入
    ctx core.StrategyContext

    // 状态变量
    prices []decimal.Decimal
}

func NewSMACross(symbol string, short, long int) *SMACrossStrategy {
    return &SMACrossStrategy{
        name:        "SMA_Cross_" + symbol,
        symbol:      symbol,
        shortWindow: short,
        longWindow:  long,
        prices:      make([]decimal.Decimal, 0, long+10),
    }
}

func (s *SMACrossStrategy) Name() string { return s.name }

// WarmupBars 返回策略需要的预热 K 线数量
func (s *SMACrossStrategy) WarmupBars() int { return s.longWindow }
```

### 2. 初始化逻辑 (`OnInit`)

用于保存 StrategyContext、初始化指标或订阅行情。

```go
// 注意: 参数是 StrategyContext，提供行情读取和交易操作能力
func (s *SMACrossStrategy) OnInit(ctx core.StrategyContext) error {
    s.ctx = ctx // 保存 context 供后续 OnBar/OnTick 使用
    fmt.Printf("[%s] Strategy Initialized\n", s.name)
    return nil
}
```

### 3. 行情驱动 (`OnBar`)

核心交易逻辑通常在此处实现。

```go
func (s *SMACrossStrategy) OnBar(ctx context.Context, bar *core.Bar) error {
    if bar.Symbol != s.symbol {
        return nil
    }

    s.prices = append(s.prices, bar.Close)
    
    // 保持窗口大小
    if len(s.prices) > s.longWindow {
        s.prices = s.prices[1:]
    }
    
    // 计算指标
    if len(s.prices) < s.longWindow {
        return nil // 数据不足
    }
    
    shortSMA := CalculateSMA(s.prices, s.shortWindow)
    longSMA := CalculateSMA(s.prices, s.longWindow)
    
    // 生成信号
    if shortSMA.GreaterThan(longSMA) {
        // 金叉买入
        fmt.Println("Buy Signal:", bar.Close)
        // TODO: 调用 Execution Context 发单
    }
    
    return nil
}

// 辅助函数
func CalculateSMA(prices []decimal.Decimal, period int) decimal.Decimal {
    sum := decimal.Zero
    // ... 简单实现
    return sum.Div(decimal.NewFromInt(int64(period)))
}
```

### 4. 实时驱动 (`OnTick`)

用于高频策略或止损监控。

```go
func (s *SMACrossStrategy) OnTick(ctx context.Context, tick *core.Tick) error {
    // 实时更新价格，检查止损
    return nil
}
```

### 5. 事件回调 (`OnOrderFill` / `OnPositionChange`)

可选实现，用于订单成交后的逻辑（如加仓/减仓通知）。

```go
func (s *SMACrossStrategy) OnOrderFill(ctx core.StrategyContext, order *core.Order) error {
    // 订单成交后的回调 (可选)
    return nil
}

func (s *SMACrossStrategy) OnPositionChange(ctx core.StrategyContext, pos *core.Position) error {
    // 持仓变化后的回调 (可选)
    return nil
}

func (s *SMACrossStrategy) OnStop(ctx context.Context) error {
    fmt.Printf("[%s] Strategy Stopped\n", s.name)
    return nil
}
```

---

## 第二部分：数据模型详解

### Bar (K线)
```go
type Bar struct {
    Symbol    string          `json:"symbol"`
    Timeframe string          `json:"timeframe"` // e.g., "1m", "1h"
    Open      decimal.Decimal `json:"open"`
    High      decimal.Decimal `json:"high"`
    Low       decimal.Decimal `json:"low"`
    Close     decimal.Decimal `json:"close"`
    Volume    decimal.Decimal `json:"volume"`
}
```

### Order (订单)

订单类型定义在 `internal/engine/entity`，通过 `core` 包重新导出：

```go
// core/types.go 中的类型别名
type Order = entity.Order
type OrderSide = entity.OrderSide
type OrderType = entity.OrderType
type Position = entity.Position
```

### OrderOptions (下单选项)

通过 `OrderOpt` 函数式选项定制下单行为：

```go
type OrderOptions struct {
    Type          OrderType          // 订单类型 (默认 market)
    Symbol        string             // 交易对 (回测多币种可选)
    ClientID      string             // 客户端订单ID
    StopPrice     decimal.Decimal    // 止损价
    Tags          map[string]string  // 自定义标签
    TimeInForce   OrderTimeInForce   // 有效期 (GTC/IOC/FOK)
    PostOnly      bool               // 仅挂单
    ReduceOnly    bool               // 仅减仓
    OCOGroup      string             // OCO 组 (一取消另一)
    OTOGroup      string             // OTO 组 (一触发另一)
    ParentOrderID string             // OTO 父单ID
    ExecStrategy  *ExecutionStrategy // 执行策略 (TWAP/VWAP/Iceberg)
}
```

常用选项函数：`WithOrderType()`, `WithStopPrice()`, `WithTimeInForce()`, `WithPostOnly()`, `WithOCOGroup()`, `WithExecutionStrategy()`。

在使用 `decimal` 时，务必时刻警惕精度问题。

---

## 检查清单

- [ ] 策略名唯一
- [ ] 实现了 `Strategy` 所有 7 个接口方法 (Name/WarmupBars/OnInit/OnStop/OnBar/OnTick/OnOrderFill/OnPositionChange)
- [ ] `OnInit` 参数类型是 `StrategyContext`（非 `context.Context`）
- [ ] `OnOrderFill` / `OnPositionChange` 至少返回 nil
- [ ] 价格计算使用 `decimal.Decimal`
- [ ] 只处理目标 Symbol 的数据
- [ ] 逻辑无死循环 / 阻塞操作
- [ ] 通过 `factory.RegisterStrategy` 注册
