---
name: GORM 数据库操作
description: WJBoot V2 数据库开发规范，涵盖通用 Repository 模式、泛型封装、模型定义与目录结构标准。
tags: [gorm, database, mysql, repository-pattern, generics, transaction, 数据库, GORM, 泛型]
---

# GORM 数据库操作规范 (WJBoot V2)

> 🗄️ **项目核心**: 本项目使用 GORM + 泛型 Repository 模式 (`BaseRepository[T]`)。所有数据库操作**必须**通过 Repository 层进行，禁止在 Service/Handler 层直接使用 `*gorm.DB`。

## 何时使用

- 添加新的数据表 (Model)
- 编写数据访问层代码 (Repository)
- 需要使用事务时
- 执行复杂的 SQL 查询

---

## 第一部分：Repository 模式

### 核心接口 (`BaseRepository[T]`)

所有 Repository **必须** 组合 `BaseRepository[T]` 接口：

```go
// internal/common/repo/base.go
type BaseRepository[T any] interface {
    Create(ctx context.Context, entity *T) error
    GetByID(ctx context.Context, id any) (*T, error)
    Update(ctx context.Context, entity *T) error
    Delete(ctx context.Context, id any) error
    List(ctx context.Context, opts ListOptions) (*ListResult[T], error)
}
```

### 定义特定 Repository

**标准位置**: `internal/common/repo/{model_name}_repo.go` (如果公用) 或 `internal/{service}/repo/{model_name}_repo.go` (如果私有)。

```go
// 1. 定义接口 (组合 BaseRepository)
type UserRepository interface {
    repo.BaseRepository[model.User] // 👈 继承基础方法的 Type Safe 版本
    GetByEmail(ctx context.Context, email string) (*model.User, error) // 👈 自定义方法
}

// 2. 定义实现结构体
type userRepository struct {
    *repo.BaseRepo[model.User] // 👈 继承基础实现 (注意是 BaseRepo 结构体, 需确认 common/repo 下的具体命名)
}

// 3. 构造函数 (Wire Provider)
func NewUserRepository(db *gorm.DB) UserRepository {
    return &userRepository{
        BaseRepo: repo.NewBaseRepo[model.User](db), // 👈 注入基础实现
    }
}

// 4. 实现自定义方法
func (r *userRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
    var user model.User
    // ⚠️ BaseRepo 内部持有 db，可通过扩展方法访问
    // 假设 BaseRepo 提供了 DB() 方法，或直接在 userRepository 中也保存 db
    // 推荐模式：在 userRepository 中显式保存 db 以便灵活使用
    err := r.BaseRepo.DB().WithContext(ctx).Where("email = ?", email).First(&user).Error
    if err != nil {
        return nil, err
    }
    return &user, nil
}
```

---

## 第二部分：Model 定义规范

**标准位置**: `internal/common/model/{table_name}.go`

```go
package model

import (
    "time"
    "github.com/shopspring/decimal"
    "gorm.io/gorm"
)

// User 用户模型
type User struct {
    ID        uint            `gorm:"primarykey"`
    Email     string          `gorm:"uniqueIndex;type:varchar(100);not null"`
    Password  string          `gorm:"type:varchar(255);not null"`
    Balance   decimal.Decimal `gorm:"type:decimal(20,8);default:0"`
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt `gorm:"index"`
}

// TableName 强制指定表名 (复数)
func (User) TableName() string {
    return "users"
}
```

> ⚠️ **Decimal 注意**: 金额字段必须使用 `decimal.Decimal`，禁止使用 `float64`。

---

## 第三部分：事务处理

使用 `NewTransaction` 创建事务管理器。

```go
// Service 层使用示例
func (s *OrderService) CreateOrder(ctx context.Context, req *OrderRequest) error {
    return s.txManager(ctx, func(tx *gorm.DB) error {
        // 在此处调用 Repo 方法
        // 注意：GORM 的某些 Transaction 模式需要传递 tx 给 Repo
        // WJBoot V2 推荐：通过 Context 传递 tx，或 Repo 方法接受 tx 参数
        return nil
    })
}
```

---

## 第四部分：常用查询范式

### 分页查询

```go
opts := repo.ListOptions{
    Page:     1,
    PageSize: 20,
    OrderBy:  "created_at",
    OrderDir: "desc",
}
result, err := userRepo.List(ctx, opts)
// result.Items, result.Total, result.TotalPages
```

---

## 检查清单

- [ ] Model 包含 `gorm` tag 类型定义
- [ ] 金额字段使用 `decimal.Decimal`
- [ ] Repository 组合了 `BaseRepository[T]`
- [ ] 构造函数已添加到 Wire Provider
