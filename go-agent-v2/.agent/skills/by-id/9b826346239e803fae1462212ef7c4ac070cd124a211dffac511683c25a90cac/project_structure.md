# WJBoot V2 项目结构与开发 SOP

> **加载条件**: 新建模块/文件、组织代码目录、添加新 API 时加载。

---

## 核心目录 (`backend/`)

| 路径 | 说明 | 关键组件 |
|------|------|----------|
| `cmd/` | 程序入口 | `main.go`, `wire_gen.go` |
| `configs/` | 配置文件 | `config.yaml` |
| `internal/` | 核心业务代码 | 见下文 |
| `pkg/` | 公共库 (可对外) | `logger`, `errors` |
| `migrations/` | 数据库迁移 | `.sql` 文件 |

> 架构详解: [架构设计](../架构设计/SKILL.md)

---

## 业务模块 (`internal/`)

```text
internal/
├── admin/          # 管理后台服务 (运营/风控)
├── api/            # 公网网关服务 (WebSocket/REST)
├── common/         # 共享内核
│   ├── config/     # 全局配置加载 (Viper, YAML → Struct)
│   ├── database/   # 数据库连接
│   ├── model/      # GORM Model
│   └── repo/       # Repository 模式
├── engine/         # 量化交易核心 (回测/实盘/撮合)
├── orchestrator/   # 策略编排与调度
├── spider/         # 市场行情采集
│   ├── common/     # 通用类型 (Kline, Ticker)
│   ├── exchange/   # 交易所适配器 (binance/)
│   ├── miner/      # K线聚合器
│   ├── server/     # WebSocket Hub
│   └── storage/    # 数据存储 (Redis/Timescale)
└── user/           # 用户中心与鉴权
```

---

## 模块内部结构标准

每个业务模块 MUST 包含:

```text
internal/{module}/
├── dto/            # 数据传输对象 (Request/Response)
├── handler/        # 接口层 (Gin/gRPC Handler)
├── service/        # 业务逻辑层 (Interface & Impl)
├── repo/           # 数据访问 (仅定义接口)
└── router/         # 路由注册
```

---

## Config 模块 (`internal/common/config/`)

| 文件 | 说明 |
|------|------|
| `config.go` | 主配置结构体 (`Config`) 与辅助方法 |
| `loader.go` | `Load()`/`MustLoad()` 函数 (Viper) |

```go
type Config struct {
    App       AppConfig                 `mapstructure:"app"`
    Database  DatabaseConfig            `mapstructure:"database"`
    Timescale TimescaleConfig           `mapstructure:"timescale"`
    Redis     RedisConfig               `mapstructure:"redis"`
    JWT       JWTConfig                 `mapstructure:"jwt"`
    Exchange  map[string]ExchangeConfig `mapstructure:"exchange"`
    Storage   StorageConfig             `mapstructure:"storage"`
    Services  ServicesConfig            `mapstructure:"services"`
    Spider    SpiderConfig              `mapstructure:"spider"`
    Backtest  BacktestConfig            `mapstructure:"backtest"`
    Risk      RiskConfig                `mapstructure:"risk"`
    Trading   TradingConfig             `mapstructure:"trading"`
    Log       LogConfig                 `mapstructure:"log"`
}
```

使用:
```go
cfg := config.MustLoad()
if cfg.IsDevelopment() {
    gin.SetMode(gin.DebugMode)
}
```

---

## 公共组件 (`pkg/`)

MUST 使用以下公共组件，NEVER 直接使用标准库 `log` 包:

| 组件 | 路径 | 替代 |
|------|------|------|
| 日志 | `pkg/logger` | 替代 `log` 包，基于 slog |
| 错误 | `pkg/errors` | 替代 `errors.New`，提供预定义哨兵错误 |

```go
// ❌ import "log"
// ✅ import "project/pkg/logger"

// ❌ import "errors"
// ✅ import "project/pkg/errors"
```

---

## 添加新 API 的 SOP

遵循 **DTO → Service 接口 → Service 实现 → Handler → Wire → Router** 顺序:

### 1. 定义 DTO (`internal/{module}/dto`)

```go
type CreateOrderRequest struct {
    Symbol   string          `json:"symbol" binding:"required"`
    Side     core.OrderSide  `json:"side" binding:"required,oneof=BUY SELL"`
    Quantity decimal.Decimal `json:"quantity" binding:"required,gt=0"`
}
```

### 2. 定义 Service 接口 (`internal/{module}/service`)

```go
type OrderService interface {
    Create(ctx context.Context, req *dto.CreateOrderRequest) (*dto.OrderResponse, error)
}
```

### 3. 实现 Service

```go
type orderService struct {
    repo repo.OrderRepository
}
```

### 4. 实现 Handler (`internal/{module}/handler`)

```go
func (h *OrderHandler) Create(c *gin.Context) {
    // 绑定 → 调用 Service → 返回
}
```

### 5. 注册依赖 (Wire)

在 `internal/common/wire/providers.go` 中添加构造函数。

### 6. 注册路由

在 `internal/{module}/router` 中绑定 Handler。

---

## GORM Gen 类型安全查询

> 详细指南: [GORM数据库操作](../GORM数据库操作/SKILL.md)
>
> 软删除、关联操作、Gen 代码生成等内容在 GORM Skill 中。
