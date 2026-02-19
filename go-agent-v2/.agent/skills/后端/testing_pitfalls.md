# Go 测试规范与常见陷阱

> **加载条件**: 编写测试、调试并发问题、代码审查时加载。

---

## 表驱动测试

MUST 使用表驱动测试:

```go
func TestValidateEmail(t *testing.T) {
    tests := []struct {
        name    string
        email   string
        wantErr bool
    }{
        {"valid email", "user@example.com", false},
        {"missing @", "userexample.com", true},
        {"empty string", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validateEmail(tt.email)
            if (err != nil) != tt.wantErr {
                t.Errorf("validateEmail(%q) error = %v, wantErr %v",
                    tt.email, err, tt.wantErr)
            }
        })
    }
}
```

---

## Gin Handler 测试

```go
func TestStrategyHandler_List(t *testing.T) {
    gin.SetMode(gin.TestMode)

    mockSvc := &MockStrategyService{
        Strategies: []dto.StrategyResponse{
            {ID: "1", Name: "SMA Cross"},
        },
    }

    h := handler.NewStrategyHandler(mockSvc)
    r := gin.New()
    r.GET("/strategies", h.List)

    req := httptest.NewRequest("GET", "/strategies", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
    assert.Contains(t, w.Body.String(), "SMA Cross")
}
```

---

## 基准测试

```go
func BenchmarkConcurrentMap(b *testing.B) {
    m := make(map[string]int)
    var mu sync.Mutex

    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            mu.Lock()
            m["key"]++
            mu.Unlock()
        }
    })
}
```

---

## 测试辅助函数

```go
// MUST 使用 t.Helper() 标记辅助函数
func assertNoError(t *testing.T, err error) {
    t.Helper()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}

// MUST 使用 t.Cleanup() 清理资源
func setupTestDB(t *testing.T) *sql.DB {
    db, _ := sql.Open("sqlite3", ":memory:")
    t.Cleanup(func() { db.Close() })
    return db
}

// 使用 testing.TB 接口支持 Test 和 Benchmark
func createTestServer(tb testing.TB) *httptest.Server {
    tb.Helper()
    server := httptest.NewServer(http.DefaultServeMux)
    tb.Cleanup(server.Close)
    return server
}
```

---

## 测试组织规则

| 规则 | 说明 |
|------|------|
| 同包测试 | 白盒测试，可访问私有函数 |
| `_test` 包后缀 | 黑盒测试，只测试公开 API |
| 测试命名 | `Test_函数名_场景` 模式 |
| 使用 `t.Run` | 子测试便于组织和筛选 |
| 覆盖路径 | MUST 同时测试成功和失败路径 |

---

## 常见陷阱

### 竞态条件

MUST 使用 `go test -race ./...` 检测竞态:

```go
// ❌ 竞态
var service map[string]net.Addr
func RegisterService(name string, addr net.Addr) {
    service[name] = addr  // 多 goroutine 并发写!
}

// ✅ 加锁
var (
    service   map[string]net.Addr
    serviceMu sync.Mutex
)
func RegisterService(name string, addr net.Addr) {
    serviceMu.Lock()
    defer serviceMu.Unlock()
    service[name] = addr
}
```

### Goroutine 泄漏

```go
// ❌ 无接收者时永久阻塞
func process() {
    ch := make(chan int)
    go func() {
        ch <- expensive()  // 永久阻塞
    }()
}

// ✅ 缓冲 channel
func process() {
    ch := make(chan int, 1)
    go func() {
        ch <- expensive()
    }()
}
```

### 闭包变量捕获

```go
// ❌ 所有 goroutine 共享同一个 v
for _, v := range values {
    go func() {
        fmt.Println(v)
    }()
}

// ✅ 每个 goroutine 获得副本
for _, v := range values {
    go func(val string) {
        fmt.Println(val)
    }(v)
}
```

### Context 取消

```go
// ❌ Goroutine 泄漏
go func() { ch <- 1 }()

// ✅ 使用 context 取消
go func() {
    select {
    case ch <- 1:
    case <-ctx.Done():
        return
    }
}()
```

### 错误检查

```go
// ❌ NEVER 忽略错误
file, _ := os.Open("file.txt")

// ✅ ALWAYS 检查
file, err := os.Open("file.txt")
if err != nil {
    return fmt.Errorf("open file: %w", err)
}
defer file.Close()
```

---

## 必须避免的错误速查

| 陷阱 | 正确做法 |
|------|---------|
| 不检查错误 | ALWAYS 检查并处理 |
| 竞态条件 | mutex 或 channel |
| Goroutine 泄漏 | context 取消 |
| 并发修改 map | `sync.Map` 或加锁 |
| 遗忘 defer | `defer` 确保清理 |
| nil 接口陷阱 | 明确理解接口零值 |
| 遗忘关闭资源 | `defer file.Close()` |
| 滥用全局变量 | 依赖注入 |
| 滥用 any 类型 | 使用泛型约束 |
| 忽略零值 | 设计零值可用 |

---

## Go 1.24+ WaitGroup 新语法

```go
// Go 1.24+ (实验性)
var wg sync.WaitGroup
wg.Go(task1)
wg.Go(task2)
wg.Wait()
```

---

## 参考资料

- Effective Go: https://go.dev/doc/effective_go
- Go 并发模式: https://go.dev/blog/pipelines
- Context 包: https://go.dev/blog/context
- Git 版本控制: [Git原子提交规范](../Git原子提交规范/SKILL.md)
