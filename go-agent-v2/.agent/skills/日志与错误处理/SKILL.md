---
name: 日志与错误处理
description: 日志系统设计与错误处理规范，涵盖日志分级、错误码设计、链路追踪和监控告警。适用于交易系统的审计日志、错误追踪和故障排查。
tags: [logging, error-handling, monitoring, tracing, 日志, 错误处理, 监控, 告警, 审计, 链路追踪]
---

# 日志与错误处理

适用于量化交易系统的日志规范与错误处理最佳实践。

## 何时使用

在以下场景使用此技能：

- 设计日志系统架构
- 定义错误码规范
- 实现链路追踪
- 配置监控告警
- 排查生产故障
- 审计日志需求

---

## 第一部分：日志分级规范

### 日志级别定义

```go
// 日志级别（从低到高）
const (
    LevelDebug = "DEBUG"  // 调试信息，生产环境关闭
    LevelInfo  = "INFO"   // 常规业务事件
    LevelWarn  = "WARN"   // 潜在问题，需关注
    LevelError = "ERROR"  // 错误，需处理
    LevelFatal = "FATAL"  // 致命错误，系统崩溃
)
```

### 级别使用指南

| 级别 | 用途 | 示例 |
|------|------|------|
| DEBUG | 开发调试详情 | 变量值、SQL语句、请求详情 |
| INFO | 关键业务事件 | 用户登录、订单创建、策略启动 |
| WARN | 非致命异常 | 重试成功、降级处理、配置缺失 |
| ERROR | 需处理的错误 | API调用失败、数据库连接断开 |
| FATAL | 系统级致命错误 | 启动失败、核心组件崩溃 |

### 交易系统专用日志

```go
// ✅ 交易日志 - 审计级别
type TradeLog struct {
    Timestamp   time.Time `json:"ts"`
    TraceID     string    `json:"trace_id"`
    UserID      string    `json:"user_id"`
    StrategyID  string    `json:"strategy_id"`
    Action      string    `json:"action"`      // SIGNAL, ORDER, FILL, CANCEL
    Symbol      string    `json:"symbol"`
    Side        string    `json:"side"`        // BUY, SELL
    Quantity    float64   `json:"qty"`
    Price       float64   `json:"price"`
    OrderID     string    `json:"order_id,omitempty"`
    Status      string    `json:"status"`
    Latency     int64     `json:"latency_us"`  // 微秒
    Error       string    `json:"error,omitempty"`
}

// ✅ 风控日志 - 必须记录
type RiskLog struct {
    Timestamp   time.Time `json:"ts"`
    TraceID     string    `json:"trace_id"`
    UserID      string    `json:"user_id"`
    RuleID      string    `json:"rule_id"`
    RuleName    string    `json:"rule_name"`
    Triggered   bool      `json:"triggered"`
    Action      string    `json:"action"`      // BLOCK, WARN, ALERT
    Details     string    `json:"details"`
}
```

---

## 第二部分：结构化日志

### 日志格式

```go
// ✅ 使用 pkg/logger (基于 slog，禁止直接 import "log/slog")
import logger "github.com/quant-trading-system/wjboot/v2/pkg/logger"

// 初始化 (应用启动时调用)
logger.Init("production") // 或 "development"

// 带上下文的日志 (Attr 风格)
logger.FromContext(ctx).Info("order created",
    logger.String(logger.FieldOrderID, order.ID),
    logger.String(logger.FieldUserID, order.UserID),
    logger.Float64("amount", order.Amount),
    logger.Duration(logger.FieldLatencyMS, latency),
)

// Sugar 风格 (引擎内部允许)
logger.Infow("order created",
    "order_id", order.ID,
    "user_id", order.UserID,
    "amount", order.Amount,
)

// 输出:
// {"time":"2026-01-19T02:30:00Z","level":"INFO","msg":"order created",
//  "order_id":"ORD123","user_id":"U456","amount":1000.50,"latency":"5.2ms"}
```

### Context 传递

```go
// ✅ pkg/logger 已提供 Context 集成，无需自定义
import logger "github.com/quant-trading-system/wjboot/v2/pkg/logger"

// 将 logger 绑定到 context
ctx = logger.WithContext(ctx, logger.Get().With("request_id", reqID))

// 从 context 获取 logger (自动附带 trace_id/span_id)
log := logger.FromContext(ctx)
log.Error("request failed",
    logger.Any(logger.FieldError, err),
)

// 中间件自动注入 (Gin 示例)
func TraceMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        traceID := c.GetHeader("X-Trace-ID")
        if traceID == "" {
            traceID = uuid.New().String()
        }
        ctx := logger.WithContext(c.Request.Context(),
            logger.Get().With(
                logger.FieldTraceID, traceID,
                "method", c.Request.Method,
                "path", c.Request.URL.Path,
            ),
        )
        c.Request = c.Request.WithContext(ctx)
        c.Header("X-Trace-ID", traceID)
        c.Next()
    }
}
```

---

## 第三部分：错误码规范

### 错误码结构

```go
// 错误码格式: [模块][类型][序号]
// 示例: TRADE-VAL-001 = 交易模块-验证错误-001

type ErrorCode struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    HTTP    int    `json:"-"`
}

// ✅ 错误码定义
var (
    // 通用错误 (SYS-)
    ErrInternal     = ErrorCode{"SYS-INT-001", "内部服务错误", 500}
    ErrUnauthorized = ErrorCode{"SYS-AUTH-001", "未授权访问", 401}
    ErrForbidden    = ErrorCode{"SYS-AUTH-002", "权限不足", 403}
    ErrNotFound     = ErrorCode{"SYS-RES-001", "资源不存在", 404}
    ErrRateLimit    = ErrorCode{"SYS-RATE-001", "请求过于频繁", 429}
    
    // 交易错误 (TRADE-)
    ErrInsufficientBalance = ErrorCode{"TRADE-BAL-001", "余额不足", 400}
    ErrInvalidSymbol       = ErrorCode{"TRADE-VAL-001", "无效的交易对", 400}
    ErrOrderNotFound       = ErrorCode{"TRADE-ORD-001", "订单不存在", 404}
    ErrOrderCancelled      = ErrorCode{"TRADE-ORD-002", "订单已取消", 400}
    ErrPositionLimit       = ErrorCode{"TRADE-POS-001", "超过持仓限制", 400}
    
    // 风控错误 (RISK-)
    ErrRiskBlocked    = ErrorCode{"RISK-BLK-001", "风控拦截", 403}
    ErrMaxLossReached = ErrorCode{"RISK-LOSS-001", "达到最大亏损限制", 403}
    ErrMaxOrderLimit  = ErrorCode{"RISK-ORD-001", "超过下单频率限制", 429}
    
    // 策略错误 (STRAT-)
    ErrStrategyNotFound  = ErrorCode{"STRAT-NFD-001", "策略不存在", 404}
    ErrStrategyDisabled  = ErrorCode{"STRAT-DIS-001", "策略已禁用", 400}
    ErrBacktestFailed    = ErrorCode{"STRAT-BT-001", "回测执行失败", 500}
)
```

### 错误包装

```go
// ✅ 自定义错误类型
type AppError struct {
    ErrorCode
    Cause   error             `json:"-"`
    Details map[string]any    `json:"details,omitempty"`
}

func (e *AppError) Error() string {
    if e.Cause != nil {
        return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Cause)
    }
    return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
    return e.Cause
}

// 创建错误
func NewError(code ErrorCode) *AppError {
    return &AppError{ErrorCode: code}
}

func (e *AppError) WithCause(err error) *AppError {
    e.Cause = err
    return e
}

func (e *AppError) WithDetails(details map[string]any) *AppError {
    e.Details = details
    return e
}

// 使用示例
err := NewError(ErrInsufficientBalance).
    WithCause(dbErr).
    WithDetails(map[string]any{
        "required": 1000,
        "available": 500,
    })
```

---

## 第四部分：链路追踪

### Trace ID 规范

```go
// ✅ 请求入口生成 Trace ID
func TraceMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        traceID := r.Header.Get("X-Trace-ID")
        if traceID == "" {
            traceID = generateTraceID()
        }
        
        spanID := generateSpanID()
        
        ctx := context.WithValue(r.Context(), "trace_id", traceID)
        ctx = context.WithValue(ctx, "span_id", spanID)
        
        w.Header().Set("X-Trace-ID", traceID)
        
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// ✅ 跨服务传递
func CallExternalService(ctx context.Context, url string) (*http.Response, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    
    traceID, _ := ctx.Value("trace_id").(string)
    parentSpanID, _ := ctx.Value("span_id").(string)
    
    req.Header.Set("X-Trace-ID", traceID)
    req.Header.Set("X-Parent-Span-ID", parentSpanID)
    req.Header.Set("X-Span-ID", generateSpanID())
    
    return http.DefaultClient.Do(req)
}
```

### 延迟监控

```go
// ✅ 交易链路延迟记录
type LatencyTracker struct {
    TraceID     string
    Checkpoints map[string]time.Time
    mu          sync.Mutex
}

func (t *LatencyTracker) Mark(checkpoint string) {
    t.mu.Lock()
    defer t.mu.Unlock()
    t.Checkpoints[checkpoint] = time.Now()
}

func (t *LatencyTracker) Report(logger *slog.Logger) {
    t.mu.Lock()
    defer t.mu.Unlock()
    
    // 计算各阶段延迟
    var stages []slog.Attr
    var prev time.Time
    
    for _, name := range []string{"tick_received", "signal_generated", "risk_checked", "order_sent", "order_acked"} {
        if ts, ok := t.Checkpoints[name]; ok {
            if !prev.IsZero() {
                stages = append(stages, slog.Int64(name+"_us", ts.Sub(prev).Microseconds()))
            }
            prev = ts
        }
    }
    
    logger.Info("latency_report",
        slog.String("trace_id", t.TraceID),
        slog.Group("stages", stages...),
    )
}
```

---

## 第五部分：监控告警

### 关键指标

```go
// ✅ Prometheus 指标
import "github.com/prometheus/client_golang/prometheus"

var (
    // 请求计数
    httpRequestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "http_requests_total",
            Help: "Total HTTP requests",
        },
        []string{"method", "path", "status"},
    )
    
    // 请求延迟
    httpRequestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "http_request_duration_seconds",
            Help:    "HTTP request duration",
            Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
        },
        []string{"method", "path"},
    )
    
    // 交易延迟（微秒级）
    tradeLatency = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "trade_latency_microseconds",
            Help:    "Trade execution latency",
            Buckets: []float64{100, 500, 1000, 5000, 10000, 50000},
        },
        []string{"strategy", "stage"},
    )
    
    // 风控触发
    riskTriggered = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "risk_triggered_total",
            Help: "Risk rules triggered",
        },
        []string{"rule_id", "action"},
    )
)
```

### 告警规则

```yaml
# ✅ Prometheus 告警规则示例
groups:
  - name: trading
    rules:
      # 错误率告警
      - alert: HighErrorRate
        expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.05
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "API 错误率过高"
          
      # 延迟告警
      - alert: HighTradeLatency
        expr: histogram_quantile(0.99, trade_latency_microseconds) > 10000
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "交易延迟 P99 超过 10ms"
          
      # 风控频繁触发
      - alert: RiskTriggeredFrequently
        expr: rate(risk_triggered_total{action="BLOCK"}[5m]) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "风控拦截频繁触发"
```

---

## 第六部分：审计日志

### 审计要求

```go
// ✅ 审计日志必须包含
type AuditLog struct {
    Timestamp   time.Time `json:"ts"`
    TraceID     string    `json:"trace_id"`
    UserID      string    `json:"user_id"`
    Action      string    `json:"action"`      // CREATE, UPDATE, DELETE, LOGIN, etc.
    Resource    string    `json:"resource"`    // order, strategy, user, etc.
    ResourceID  string    `json:"resource_id"`
    OldValue    string    `json:"old_value,omitempty"`  // JSON
    NewValue    string    `json:"new_value,omitempty"`  // JSON
    IP          string    `json:"ip"`
    UserAgent   string    `json:"user_agent"`
    Result      string    `json:"result"`      // SUCCESS, FAILED
    Error       string    `json:"error,omitempty"`
}

// ✅ 审计日志写入（独立存储，不可篡改）
func WriteAuditLog(ctx context.Context, log AuditLog) error {
    log.Timestamp = time.Now()
    log.TraceID, _ = ctx.Value("trace_id").(string)
    
    // 写入专用审计表（只增不改）
    return auditDB.Create(&log).Error
}
```

### 敏感操作审计

```go
// ✅ 必须审计的操作
var auditRequiredActions = []string{
    "user.login",
    "user.logout", 
    "user.password_change",
    "order.create",
    "order.cancel",
    "strategy.enable",
    "strategy.disable",
    "risk.override",
    "balance.withdraw",
    "api_key.create",
    "api_key.revoke",
}
```

---

## 第七部分：量化引擎日志规范

### 引擎日志级别指南

| 场景 | 级别 | 示例 |
|------|------|------|
| K线加载、策略初始化、引擎启动 | INFO | `logger.Infow("klines loaded", "symbol", sym, "bars", count)` |
| 信号生成 | DEBUG | `logger.Debugw("signal generated", "signal", signal, "price", price)` |
| 下单请求 | INFO | `logger.Infow("order submitted", "order_id", orderID, "side", side)` |
| 成交通知 | INFO | `logger.Infow("order filled", "order_id", orderID, "fill_price", price)` |
| 风控拦截 | WARN | `logger.Warnw("risk blocked", "rule", rule, "reason", reason)` |
| API/WS 重连 | WARN | `logger.Warnw("ws reconnecting", "attempt", n, "error", err)` |
| 策略异常 | ERROR | `logger.FromContext(ctx).Error("strategy error", logger.Any(logger.FieldError, err))` |

### 无 Context 的 Goroutine 日志

实盘中存在大量无 `ctx` 的 goroutine，MUST 在启动时注入 logger:

```go
// ✅ 实盘 goroutine 日志模式
type Manager struct {
    logger *logger.Logger  // 依赖注入
}

func NewManager(log *logger.Logger) *Manager {
    if log == nil {
        log = logger.Get()
    }
    return &Manager{logger: log.With(logger.FieldComponent, "live_manager")}
}

// loggerFromContext: 优先从 ctx 获取，降级到注入实例
func (m *Manager) loggerFromContext(ctx context.Context) *logger.Logger {
    if ctx != nil {
        return logger.FromContext(ctx)
    }
    return m.logger
}
```

### 高频数据日志采样

K线/Tick/OrderBook 日志 MUST 通过 `component` 字段触发自动采样:

```go
// ✅ pkg/logger/sampling.go 会对含 "ws"/"orderbook"/"grpc" component 的日志采样
log := logger.Get().With(logger.FieldComponent, "orderbook")
log.Debug("orderbook updated", "bids", len(bids), "asks", len(asks))
// 生产环境: 每 100 条只输出 1 条 Debug 级别
```

---

## 审查清单

### L1 禁用 API
- [ ] 无 `fmt.Println` / `fmt.Printf` (CLI 工具除外)
- [ ] 无 `import "log"` (标准库)
- [ ] 无 `panic()` (init 除外)
- [ ] 无 `os.Exit` (main 除外)

### L2 错误处理
- [ ] 无 `_, _ :=` 错误忽略 (`hash.Write` 等永不失败的除外)
- [ ] 同包内部直接 `return err`，NEVER `fmt.Errorf` 逐层包装
- [ ] 跨层边界使用 `errors.Wrap(err, op, msg)` 或 `EngineError`/`TradeError`
- [ ] Handler 层返回用户友好消息，NEVER `err.Error()` 暴露内部错误
- [ ] 使用 `pkg/errors` 哨兵错误，NEVER 重复定义

### L3 日志规范
- [ ] 使用 `pkg/logger`，NEVER 直接 `import "log/slog"`
- [ ] 使用结构化日志（JSON 格式）
- [ ] 日志级别正确使用 (DEBUG/INFO/WARN/ERROR)
- [ ] Error 级别日志包含 trace_id (`logger.FromContext(ctx)`)
- [ ] Handler/入口层使用 `logger.FieldXxx` 常量
- [ ] 敏感信息已脱敏 (密码、密钥、Token)
- [ ] 高频日志使用 `component` 字段触发采样

### 监控
- [ ] 关键指标已采集 (Prometheus)
- [ ] 延迟直方图已配置
- [ ] 告警规则已设置
- [ ] 审计日志已记录



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
