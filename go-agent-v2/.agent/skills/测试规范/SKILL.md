---
name: 测试规范
description: WJBoot V2 测试体系指南，涵盖单元测试、Mock 策略、Repository 集成测试与回测验证。
tags: [testing, unit-test, integration-test, mock, gomock, 测试, 单元测试]
---

# WJBoot 测试规范

> 🧪 **核心原则**: 金融系统零容忍。所有核心逻辑（资金计算、订单状态流转、策略信号）必须有 100% 的分支覆盖率。

## 第一部分：单元测试 (Unit Test)

### 策略逻辑测试

使用 Mock Context 验证策略行为，而不启动完整引擎。

```go
// internal/strategy/grid/grid_test.go

func TestGridStrategy_OnBar(t *testing.T) {
    // 1. 准备 Mock
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()
    
    mockCtx := mocks.NewMockStrategyContext(ctrl)
    
    // 2. 设定预期行为
    // 预期：当收盘价 < 9000 时，调用 OpenLong
    mockCtx.EXPECT().Close(0).Return(decimal.NewFromInt(8900))
    mockCtx.EXPECT().OpenLong(gomock.Any(), gomock.Any()).Return(nil, nil)
    
    // 3. 执行测试
    strategy := NewGridStrategy()
    strategy.OnBar(mockCtx)
}
```

### 工具函数测试

对于纯函数（如技术指标计算），使用 Table-Driven Tests。

```go
func TestCalculateRSI(t *testing.T) {
    tests := []struct {
        name   string
        input  []float64
        period int
        want   float64
    }{
        {"BaseCase", []float64{10, 12, 11, 13, 15}, 14, 65.5},
        {"ZeroInput", []float64{}, 14, 0},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := CalculateRSI(tt.input, tt.period); got != tt.want {
                t.Errorf("RSI() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

---

## 第二部分：Repository 集成测试

DB 层测试**不要 mock SQL**，必须连接真实数据库（推荐使用 Docker 或 SQLite 内存模式）。

```go
// internal/common/repo/user_test.go

func TestUserRepository_Create(t *testing.T) {
    // 1. Setup: 连接测试库
    db := setupTestDB() // 自动迁移 Schema
    repo := NewUserRepository(db)
    
    // 2. Action
    user := &entity.User{Email: "test@example.com"}
    err := repo.Create(context.Background(), user)
    
    // 3. Assert
    assert.NoError(t, err)
    assert.NotZero(t, user.ID)
    
    // 4. Verify in DB
    var saved entity.User
    db.First(&saved, user.ID)
    assert.Equal(t, "test@example.com", saved.Email)
}
```

---

## 第三部分：回测一致性验证 (Golden Record)

为防止重构改变策略逻辑，需锁定回测结果。

1.  **录制**: 运行一次标准回测，将结果 (`pnl`, `max_drawdown`, `trade_count`) 保存为 JSON。
2.  **验证**: 每次 CI 运行时，重新跑回测，比对结果是否偏差 > 0.01%。

```bash
# 运行回测一致性检查
go test ./internal/engine/backtest -run TestConsistency
```

---

## 检查清单

- [ ] 核心算法是否有 Table-Driven Tests？
- [ ] Repo 层是否使用了真实数据库环境？
- [ ] 是否在 `OnBar` 中处理了错误？
- [ ] 是否运行了 `go test -race` 检查并发问题？
