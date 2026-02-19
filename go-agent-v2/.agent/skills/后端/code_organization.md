# Go 代码组织与 DRY 模式

> **加载条件**: 文件拆分/合并、_chains 清理、消除重复代码、接口设计、零值可用时加载。

---

## 文件组织强制规则

| 规则 | 阈值 | 违反时行动 |
|------|------|-----------|
| 禁止 `_chains.go` 后缀 | 0 容忍 | 合并到父文件 |
| 文件名下划线层数 | ≤3 层 | 重新归类 |
| 单文件最小行数 | ≥50 行 (仅函数体) | 合并到相近文件 |
| 同包 `.go` 文件数 | ≤30 个 | 提取子包或合并 |

```text
# ❌ 错误：过度拆分 chains
live/
├── manager.go
├── manager_state_chains.go          # 19 行
├── manager_order_chains.go          # 23 行
├── manager_risk_chains.go           # 31 行
├── manager_position_chains.go       # 15 行
└── manager_sync_chains.go           # 12 行

# ✅ 正确：按职责归类
live/
├── manager.go                       # 核心调度 + 状态管理
├── manager_order.go                 # 订单相关方法 (≥50 行)
└── manager_risk.go                  # 风控相关方法 (≥50 行)
```

拆分深度规则:
- `order.go` → 1 层 ✅
- `order_validate.go` → 2 层 ✅
- `order_validate_price.go` → 3 层 ✅ (上限)
- `order_request_validate_price_tif_chains.go` → 5 层 ❌

---

## 接口设计

- 保持小型 (1-3 方法)
- 接受接口，返回具体类型
- 接口由消费方定义

```go
// ✅ 小接口
type Reader interface {
    Read(p []byte) (n int, err error)
}

// ✅ 接受接口，返回具体类型
func NewService(repo UserRepository) *UserService {
    return &UserService{repo: repo}
}
```

---

## 零值可用

设计类型使其零值可直接使用，无需构造函数:

```go
type Buffer struct {
    buf []byte
}

func (b *Buffer) Write(p []byte) (int, error) {
    b.buf = append(b.buf, p...)  // nil slice 也能 append
    return len(p), nil
}

// 无需 NewBuffer() 即可使用
var buf Buffer
buf.Write([]byte("hello"))
```

---

## DRY 原则与工厂函数

MUST 在代码审查时检查 DRY 原则，发现重复代码立即重构。

**重复检测信号**:
| 信号 | 行动 |
|------|------|
| 3+ 处代码结构相似 (仅参数不同) | 提取工厂函数 |
| 复制代码后仅改变少量变量 | 抽象为通用函数 |
| 函数参数超过 5 个 | 使用 Options 模式 |
| 多个 if/switch 分支执行相似逻辑 | 使用策略模式/工厂 |

### 泛型 Handler 工厂

```go
type HandlerFunc[Req any, Resp any] func(ctx context.Context, req *Req) (*Resp, error)

func NewHandler[Req any, Resp any](fn HandlerFunc[Req, Resp]) gin.HandlerFunc {
    return func(c *gin.Context) {
        var req Req
        if err := c.ShouldBindJSON(&req); err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }
        resp, err := fn(c.Request.Context(), &req)
        if err != nil {
            c.JSON(500, gin.H{"error": err.Error()})
            return
        }
        c.JSON(200, resp)
    }
}

// 使用
router.POST("/users", NewHandler(userService.Create))
router.POST("/orders", NewHandler(orderService.Create))
```

### Repository 工厂模式

> 详细指南: [GORM数据库操作](../GORM数据库操作/SKILL.md)
>
> 本项目使用 **泛型 Repository (`BaseRepository[T]`)** 模式。

### Options 模式

```go
type ServerOption func(*Server)

func WithTimeout(d time.Duration) ServerOption {
    return func(s *Server) { s.timeout = d }
}

func WithTLS(cert, key string) ServerOption {
    return func(s *Server) {
        s.tls = true
        s.cert = cert
        s.key = key
    }
}

func NewServer(host string, port int, opts ...ServerOption) *Server {
    s := &Server{host: host, port: port, timeout: 30 * time.Second}
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

---

## 代码审查检查清单

```text
□ 新文件名是否含 _chains？ → 禁止
□ 文件是否低于 50 行？ → 合并
□ 文件名下划线层数是否 >3？ → 精简
□ 同目录 .go 文件是否 >30 个？ → 提取子包
□ 是否有 3+ 处相似代码？ → 提取工厂
□ 函数参数是否超过 5 个？ → Options 模式
□ 是否有重复的 error 处理？ → 中间件/装饰器
□ 是否有重复的数据库操作？ → 泛型 Repository
```
