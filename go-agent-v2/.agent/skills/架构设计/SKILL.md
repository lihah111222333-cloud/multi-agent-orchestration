---
name: 架构设计
description: 涵盖整洁架构 (Clean Architecture)、六边形架构 (Hexagonal)、DDD 与 WJBoot V2 项目的代码映射关系。
tags: [architecture, ddd, clean-architecture, hexagonal, design-patterns, 架构, 设计模式]
---

# 系统架构设计规范 (WJBoot V2)

> 🏛️ **核心理念**: 本项目采用 **六边形架构 (Hexagonal Architecture)**，强调业务核心与基础设施的分离。

## 何时使用

- 设计新的微服务或复杂模块
- 界定代码应放置的分层位置 (Layer)
- 进行领域建模 (Entity/Aggregate)
- 解决循环依赖问题

---

## 第一部分：WJBoot V2 架构映射

我们将经典的六边形架构映射到 Go 项目目录：

| 架构层级 | 职责 | 对应目录 | 依赖规则 |
|---|---|---|---|
| **Domain (核心)** | 业务实体、领域事件、核心接口 | `internal/{module}/domain` (或 `service/model`) | **不依赖任何层** |
| **Application (应用)** | 用例逻辑、事务编排 | `internal/{module}/service` | 依赖 Domain |
| **Adapter (适配器-输入)** | HTTP 处理器、RPC 服务 | `internal/{module}/handler` | 依赖 Application |
| **Adapter (适配器-输出)** | 数据库、Redis、外部 API | `internal/{module}/repo` | 依赖 Domain 接口 |

> 🔄 **依赖倒置**: `Service` 层只依赖 `Repo` 的**接口**，而 `Repo` 的**实现** 依赖基础设施。

---

## 第二部分：领域驱动设计 (DDD) 核心概念

### 1. 实体 (Entity)
具有唯一标识，且生命周期内状态可变的对象。

```go
// Order 是一个聚合根
type Order struct {
    ID     string
    Status OrderStatus
    Items  []OrderItem
}

// 业务行为 (Rich Domain Model)
func (o *Order) Pay() error {
    if o.Status != StatusCreated {
        return ErrInvalidStatus
    }
    o.Status = StatusPaid
    return nil
}
```

### 2. 值对象 (Value Object)
无唯一标识，通过属性值定义的不可变对象。

```go
// Money 是值对象
type Money struct {
    Amount   decimal.Decimal
    Currency string
}

func (m Money) Add(other Money) Money {
    // 返回新对象，不修改原有对象
    return Money{Amount: m.Amount.Add(other.Amount), Currency: m.Currency}
}
```

### 3. 用例 (Use Case / Service)
协调领域对象完成业务目标。

```go
func (s *OrderService) PayOrder(ctx context.Context, orderID string) error {
    // 1. 获取聚合
    order, _ := s.repo.GetByID(orderID)
    
    // 2. 执行领域逻辑
    if err := order.Pay(); err != nil {
        return err
    }
    
    // 3. 持久化
    return s.repo.Save(order)
}
```

---

## 第三部分：常见反模式 (Anti-Patterns)

### ❌ 贫血模型 (Anemic Domain Model)
Entity 只有字段没有方法，逻辑全泄露到 Service。

```go
// ❌ 错误：Service 操作字段
func (s *Service) Pay(o *Order) {
    if o.Status == "CREATED" {
        o.Status = "PAID" // 逻辑泄露
    }
}
```

### ❌ 基础设施穿透
Service 层直接引用具体数据库实现（如 `*gorm.DB`），而不是接口。

```go
// ❌ 错误：直接依赖 GORM
type Service struct {
    db *gorm.DB 
}

// ✅ 正确：依赖 Repo 接口
type Service struct {
    repo OrderRepository
}
```

---

## 第四部分：模块化单体 vs 微服务

WJBoot V2 支持由 **模块化单体** 平滑过渡到 **微服务**。

- **模块化**: `internal/user`, `internal/trade` 物理隔离，禁止跨模块直接调用代码。
- **通信**: 模块间通信应通过 `internal/api` 定义的公开接口，或通过 gRPC/EventBus。

---

## 检查清单

- [ ] 核心业务逻辑是否在 Domain/Service 层？
- [ ] 是否存在 Controller 直接调 DB 的情况？(禁止)
- [ ] 模块间依赖是否清晰？(避免循环依赖)
