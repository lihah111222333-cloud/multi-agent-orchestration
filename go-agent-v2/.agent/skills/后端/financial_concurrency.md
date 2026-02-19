# 金融级并发控制

> **加载条件**: 锁层次设计、死锁排查、订单簿并发、原子操作/CAS、交易所 API 限流、熔断器时加载。

---

## 锁层次文档化 (Lock Hierarchy)

**强制规范**: 任何包含多个 `sync.Mutex` 或 `sync.RWMutex` 的结构体 MUST 在代码中注释锁层次顺序。

原因: 多 goroutine 同时持有多个锁时，不同获取顺序导致**死锁**。

```go
type TradingContext struct {
    // ========================================
    // 锁层次 (Lock Hierarchy)
    // ========================================
    // 获取顺序: accountMu < positionMu < ordersMu
    // 任何需要同时持有多个锁的情况,必须按此顺序获取
    // ========================================

    capital    decimal.Decimal
    accountMu  sync.RWMutex // 锁层次: 1 (最高优先级)

    positions  map[string]*Position
    positionMu sync.RWMutex // 锁层次: 2

    orders   []*Order
    ordersMu sync.Mutex   // 锁层次: 3 (最低优先级)
}
```

**踩坑: defer 顺序导致死锁**:

```go
// ❌ 获取顺序错误
func (s *Service) updatePosition() error {
    s.positionMu.Lock()  // 层次 2 ← 先获取了低优先级锁!
    s.balanceMu.Lock()   // 层次 1
    // ...
}

// 另一个 goroutine 按正确顺序获取锁 → 💥 死锁
func (s *Service) syncBalance() error {
    s.balanceMu.Lock()   // 层次 1
    s.positionMu.Lock()  // 层次 2
    // ... 与 updatePosition 形成死锁!
}

// ✅ 严格按层次顺序
func (s *Service) updatePosition() error {
    s.balanceMu.Lock()   // 层次 1
    s.positionMu.Lock()  // 层次 2
    defer s.positionMu.Unlock()  // defer LIFO 自动反序释放
    defer s.balanceMu.Unlock()
}
```

**代码审查检查清单**:
- [ ] 结构体有多个锁时，是否注释了锁层次？
- [ ] 同时获取多个锁时，是否按层次顺序？
- [ ] `defer Unlock()` 声明顺序是否与 `Lock()` 顺序相反？

---

## 订单簿细粒度锁

```go
type OrderBook struct {
    bids    []Order
    asks    []Order
    bidsMu  sync.RWMutex
    asksMu  sync.RWMutex
}

// 读写锁分离
func (ob *OrderBook) AddBid(order Order) {
    ob.bidsMu.Lock()
    defer ob.bidsMu.Unlock()
    ob.bids = append(ob.bids, order)
}

func (ob *OrderBook) GetBestBid() (Order, bool) {
    ob.bidsMu.RLock()
    defer ob.bidsMu.RUnlock()
    if len(ob.bids) == 0 {
        return Order{}, false
    }
    return ob.bids[0], true
}

// 跨队列匹配: 固定顺序 bids → asks
func (ob *OrderBook) Match() {
    ob.bidsMu.Lock()
    ob.asksMu.Lock()
    defer ob.asksMu.Unlock()
    defer ob.bidsMu.Unlock()
    // 撮合逻辑
}
```

---

## 原子操作与账户余额

```go
import "sync/atomic"

type Account struct {
    balance  uint64 // 原子变量 (单位: 分)
    currency string
}

// 原子扣款 (防止超卖)
func (a *Account) Deduct(amount uint64) bool {
    for {
        oldBalance := atomic.LoadUint64(&a.balance)
        if oldBalance < amount {
            return false
        }
        newBalance := oldBalance - amount
        if atomic.CompareAndSwapUint64(&a.balance, oldBalance, newBalance) {
            return true
        }
        // CAS 失败，重试
    }
}

func (a *Account) Deposit(amount uint64) {
    atomic.AddUint64(&a.balance, amount)
}

func (a *Account) Balance() uint64 {
    return atomic.LoadUint64(&a.balance)
}
```

---

## Token Bucket 限流器

```go
import "golang.org/x/time/rate"

// 交易所 API 限流 (10 请求/秒，突发 20)
type ExchangeClient struct {
    limiter *rate.Limiter
}

func NewExchangeClient() *ExchangeClient {
    return &ExchangeClient{
        limiter: rate.NewLimiter(rate.Limit(10), 20),
    }
}

// 阻塞等待
func (c *ExchangeClient) PlaceOrder(order Order) error {
    if err := c.limiter.Wait(context.Background()); err != nil {
        return err
    }
    return c.callAPI("/order", order)
}

// 非阻塞 (失败立即返回)
func (c *ExchangeClient) PlaceOrderNonBlocking(order Order) error {
    if !c.limiter.Allow() {
        return fmt.Errorf("rate limit exceeded")
    }
    return c.callAPI("/order", order)
}

// 批量预留令牌
func (c *ExchangeClient) PlaceOrdersBatch(orders []Order) error {
    r := c.limiter.ReserveN(time.Now(), len(orders))
    if !r.OK() {
        return fmt.Errorf("rate limit exceeded")
    }
    if delay := r.Delay(); delay > 0 {
        time.Sleep(delay)
    }
    return c.callAPIBatch("/orders", orders)
}

// 动态调整
func (c *ExchangeClient) UpdateLimit(rps int) {
    c.limiter.SetLimit(rate.Limit(rps))
}
```

---

## 熔断器

```go
type CircuitBreakerState int
const (
    StateClosed   CircuitBreakerState = iota // 正常
    StateOpen                                // 熔断
    StateHalfOpen                            // 半开 (试探恢复)
)

type CircuitBreaker struct {
    maxFailures   int
    timeout       time.Duration
    resetTimeout  time.Duration
    failures      int
    lastFailure   time.Time
    state         CircuitBreakerState
    mu            sync.Mutex
}

func (cb *CircuitBreaker) Call(fn func() error) error {
    cb.mu.Lock()
    if cb.state == StateOpen {
        if time.Since(cb.lastFailure) > cb.resetTimeout {
            cb.state = StateHalfOpen
        } else {
            cb.mu.Unlock()
            return fmt.Errorf("circuit breaker open")
        }
    }
    cb.mu.Unlock()

    err := fn()

    cb.mu.Lock()
    defer cb.mu.Unlock()
    if err != nil {
        cb.failures++
        cb.lastFailure = time.Now()
        if cb.failures >= cb.maxFailures {
            cb.state = StateOpen
        }
        return err
    }
    if cb.state == StateHalfOpen {
        cb.state = StateClosed
    }
    cb.failures = 0
    return nil
}

// 实战: 交易所 API + 熔断降级
type ExchangeAPI struct {
    client         *http.Client
    circuitBreaker *CircuitBreaker
}

func (api *ExchangeAPI) GetTicker(symbol string) (*Ticker, error) {
    var ticker *Ticker
    err := api.circuitBreaker.Call(func() error {
        resp, err := api.client.Get("/ticker?symbol=" + symbol)
        if err != nil {
            return err
        }
        defer resp.Body.Close()
        if resp.StatusCode >= 500 {
            return fmt.Errorf("server error: %d", resp.StatusCode)
        }
        return json.NewDecoder(resp.Body).Decode(&ticker)
    })
    if err != nil {
        if api.circuitBreaker.state == StateOpen {
            return api.getTickerFromCache(symbol) // 熔断时返回缓存
        }
        return nil, err
    }
    return ticker, nil
}
```
