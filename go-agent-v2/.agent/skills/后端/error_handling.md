# Go 错误处理规范

> **加载条件**: 错误包装、哨兵错误、自定义错误类型时加载。

---

## 三层日志合规审计口径

> [!IMPORTANT]
> 以下是「三层日志合规审计」的统一评判标准，所有审计 MUST 以此为准。

| 层 | 审计内容 | 关键规则 | 标准来源 |
|---|---------|---------|----------|
| **L1 禁用 API** | `fmt.Print*`, 标准 `log`, `panic`, `os.Exit` | 零容忍 | `SKILL.md` 核心规则 |
| **L2 错误处理** | `_, _ :=` 忽略 / `fmt.Errorf` 跨层包装 / `errors.Wrap` 使用 / `EngineError`·`TradeError` 类型 | 同包 `return err`, 跨层 `errors.Wrap` | 本文件 |
| **L3 日志规范** | `FromContext` / `FieldXxx` 常量 / 结构化 / 级别 | Handler 层 MUST `FromContext` + `FieldXxx` | 本文件 + `日志与错误处理/SKILL.md` |

---

## 核心原则: 三层错误体系 + 日志系统统一处理

> [!IMPORTANT]
> 项目使用 `pkg/errors` 三层错误体系，**禁止**在每个调用点用 `fmt.Errorf("xxx: %w", err)` 单独包装错误。
> 错误上下文由日志系统 (`pkg/logger`) 在边界层统一记录。

| 层级 | 类型 | 用途 |
|------|------|------|
| L1 哨兵错误 | `errors.ErrNotFound` 等 | 标准错误类型判断 |
| L2 引擎错误 | `errors.EngineError` | 引擎内部带 Op/Code/Message |
| L3 交易错误 | `errors.TradeError` | 交易专用带 TradeCode + Retryable |

### 禁止 ❌

```go
// ❌ 禁止: 每个调用点单独包装
if err != nil {
    return fmt.Errorf("create order for customer %s: %w", customerID, err)
}

// ❌ 禁止: 重复包装导致嵌套 "a: b: c: d: actual error"
return fmt.Errorf("step3: %w", fmt.Errorf("step2: %w", fmt.Errorf("step1: %w", err)))
```

### 正确 ✅

```go
// ✅ 同包内部: 直接返回
if err != nil {
    return err
}

// ✅ 跨层边界 (导出方法入口): 用 pkg/errors.Wrap 或构造 EngineError/TradeError
func (s *Strategy) OnBar(ctx context.Context, bar *entity.Kline) error {
    if err := s.calculate(bar); err != nil {
        return errors.Wrap(err, "Strategy.OnBar", "signal calculation failed")
    }
    return nil
}

// ✅ 交易相关: 用 TradeError
if err := exchange.PlaceOrder(order); err != nil {
    return errors.NewTradeErrorWithCause(errors.CodeNetTimeout, "Executor.Submit", "place order failed", err)
}

// ✅ Handler/入口层: 用 context-aware 日志记录上下文 (自动注入 trace_id/span_id)
log := logger.FromContext(ctx)
log.Error("backtest failed",
    logger.String(logger.FieldUserID, userID),
    logger.String(logger.FieldStrategyID, strategyID),
    logger.Any(logger.FieldError, err),
)
```

> [!NOTE]
> **prod_handler 自动行为**: Error+ 级别日志会自动附加 `source` (文件:行号)、`function`、`stacktrace` 字段，无需手动添加。

---

## 日志系统关键能力

| 能力 | 说明 | 使用方式 |
|------|------|---------|
| Context 感知 | 自动注入 `trace_id`/`span_id` | `logger.FromContext(ctx)` |
| 预留字段常量 | 18 个标准字段名，MUST 使用常量 | `logger.FieldUserID`, `logger.FieldOrderID` 等 |
| 自动 Stacktrace | Error+ 级别自动附加调用栈 | 无需手动处理 |
| 高频采样 | WS/Orderbook/gRPC 日志自动采样 | 通过 `component` 字段触发 |
| slog 兼容 | re-export slog 类型，engine 模块无需直接 import slog | `logger.Logger`, `logger.Attr` |

### 预留字段常量

**Attr 风格 (推荐)**: Handler/入口层 MUST 使用常量键名

```go
// ✅ Attr 风格 — MUST 使用 FieldXxx 常量
logger.FromContext(ctx).Error("order failed",
    logger.String(logger.FieldUserID, userID),
    logger.String(logger.FieldOrderID, orderID),
    logger.Any(logger.FieldError, err),
)
```

**Sugar 风格 (允许)**: 引擎内部可使用 `Infow/Warnw/Errorw`，字段名直接写字符串

```go
// ✅ Sugar 风格 — 允许硬编码字段名 (Infow/Warnw/Errorw)
logger.Infow("klines loaded", "symbol", sym, "bars", count)
logger.Errorw("order fill failed", "error", err, "order_id", orderID)
```

完整常量列表:

```go
logger.FieldTraceID    // "trace_id"    logger.FieldSpanID     // "span_id"
logger.FieldRunID      // "run_id"      logger.FieldStrategyID // "strategy_id"
logger.FieldUserID     // "user_id"     logger.FieldOrderID    // "order_id"
logger.FieldSymbol     // "symbol"      logger.FieldExchange   // "exchange"
logger.FieldTimeframe  // "timeframe"   logger.FieldComponent  // "component"
logger.FieldModule     // "module"      logger.FieldError      // "error"
```

---

## 错误处理决策树

```text
收到 error →
├─ 同包内部私有方法？
│   └─ 直接 return err ✅
├─ 导出方法 / 跨层边界？
│   ├─ 引擎错误 → errors.Wrap(err, op, message) 或 &EngineError{...}
│   └─ 交易错误 → errors.NewTradeErrorWithCause(code, op, msg, err)
└─ Handler / CLI 入口？
    └─ logger.FromContext(ctx).Error(...) 结构化记录，返回用户友好消息
    └─ MUST 使用 logger.FieldXxx 常量作为字段名
```

---

## 哨兵错误

使用 `pkg/errors` 预定义的哨兵错误，NEVER 重复定义:

```go
import "github.com/quant-trading-system/wjboot/v2/pkg/errors"

// 已预定义:
//   errors.ErrNotFound, errors.ErrUnauthorized, errors.ErrInvalidInput,
//   errors.ErrTimeout, errors.ErrRateLimited, errors.ErrInsufficientBalance

// 返回
func GetUser(id string) (*User, error) {
    user := db.Find(id)
    if user == nil {
        return nil, errors.ErrNotFound
    }
    return user, nil
}

// 检查
if errors.Is(err, errors.ErrNotFound) { ... }
```

---

## EngineError (引擎专用)

```go
// 构造
return &errors.EngineError{
    Op:      "BacktestEngine.Run",
    Code:    "DATA_MISSING",
    Message: "no klines for BTC/USDT",
    Err:     originalErr,
}

// 或使用便捷方法
return errors.Wrap(err, "BacktestEngine.Run", "data loading failed")

// 检查
var engineErr *errors.EngineError
if errors.As(err, &engineErr) {
    logger.Errorw("engine error", "op", engineErr.Op, "code", engineErr.Code)
}
```

---

## TradeError (交易专用)

```go
// 创建
return errors.NewTradeError(errors.CodeLiquidation, "RiskController.Check", "position liquidated")

// 带原因
return errors.NewTradeErrorWithCause(errors.CodeNetTimeout, "Exchange.PlaceOrder", "timeout", err)

// 检查可重试性
if errors.IsRetryable(err) {
    // 网络错误 (-140 ~ -145) 自动标记为可重试
    retry(ctx, fn)
}
```

---

## 错误码范围

| 范围 | 类别 | 示例 |
|------|------|------|
| -130 ~ -139 | 仓位类 | `CodeLiquidation`, `CodeLowFunds`, `CodePositionMismatch` |
| -140 ~ -149 | 网络类 (可重试) | `CodeNetTimeout`, `CodeExchangeDown` |
| -150 ~ -159 | 数据类 | `CodeKlineStuck`, `CodeDataCorrupted` |
