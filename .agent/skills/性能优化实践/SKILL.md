---
name: 性能优化实践
description: 全栈性能优化指南，涵盖前端渲染、后端处理、数据库查询和系统监控。适用于诊断和解决性能问题。
tags: [performance, optimization, frontend, backend, database, 性能优化, 调优, 前端性能, 后端性能, 监控]
---

# 性能优化实践

适用于全栈应用性能优化的规范指南。

## 何时使用

在以下场景使用此技能：

- 诊断性能瓶颈
- 优化页面加载速度
- 提升 API 响应时间
- 优化数据库查询
- 配置系统监控

---

## 第一部分：前端性能优化

### 首屏加载优化

```tsx
// ✅ 代码分割和懒加载
const Dashboard = lazy(() => import('./pages/Dashboard'));
const Settings = lazy(() => import('./pages/Settings'));

// ✅ 预加载关键资源
<link rel="preload" href="/fonts/inter.woff2" as="font" crossOrigin />
<link rel="preconnect" href="https://api.example.com" />

// ✅ 关键 CSS 内联
<style dangerouslySetInnerHTML={{ __html: criticalCSS }} />
```

### 图片优化

```tsx
// ✅ 响应式图片
<picture>
  <source media="(min-width: 768px)" srcSet="/hero-lg.webp" />
  <source media="(min-width: 480px)" srcSet="/hero-md.webp" />
  <img src="/hero-sm.webp" alt="Hero" loading="lazy" />
</picture>

// ✅ 懒加载和占位符
<img
  src={thumbnail}
  data-src={fullImage}
  loading="lazy"
  width={400}
  height={300}
  alt="Product"
/>
```

### 渲染优化

```tsx
// ✅ 使用 memo 避免不必要的重渲染
const ExpensiveComponent = memo(function ExpensiveComponent({ data }) {
  return <div>{/* 复杂渲染逻辑 */}</div>;
});

// ✅ 使用 useMemo 缓存计算结果
const sortedItems = useMemo(() => {
  return items.sort((a, b) => a.price - b.price);
}, [items]);

// ✅ 使用 useCallback 稳定回调引用
const handleClick = useCallback((id: string) => {
  setSelectedId(id);
}, []);

// ✅ 虚拟列表处理大数据
import { useVirtualizer } from '@tanstack/react-virtual';
```

### 请求优化

```tsx
// ✅ 数据预取
const queryClient = useQueryClient();

// 预取下一页
useEffect(() => {
  if (data?.hasNextPage) {
    queryClient.prefetchQuery({
      queryKey: ['items', page + 1],
      queryFn: () => fetchItems(page + 1),
    });
  }
}, [data, page, queryClient]);

// ✅ 请求去重和缓存
const { data } = useQuery({
  queryKey: ['user', userId],
  queryFn: () => fetchUser(userId),
  staleTime: 5 * 60 * 1000,  // 5分钟内不重新请求
  gcTime: 30 * 60 * 1000,    // 缓存30分钟
});

// ✅ 批量请求
const results = await Promise.all([
  fetchUser(userId),
  fetchOrders(userId),
  fetchNotifications(userId),
]);
```

---

## 第二部分：后端性能优化

### Go 并发优化

```go
// ✅ Worker Pool 模式
func ProcessTasks(tasks []Task, maxWorkers int) []Result {
    results := make([]Result, len(tasks))
    taskCh := make(chan int, len(tasks))
    
    var wg sync.WaitGroup
    for i := 0; i < maxWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for idx := range taskCh {
                results[idx] = processTask(tasks[idx])
            }
        }()
    }
    
    for i := range tasks {
        taskCh <- i
    }
    close(taskCh)
    wg.Wait()
    
    return results
}

// ✅ 带超时的并发请求
func FetchWithTimeout(ctx context.Context, urls []string) []Response {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    
    results := make([]Response, len(urls))
    var wg sync.WaitGroup
    
    for i, url := range urls {
        wg.Add(1)
        go func(idx int, url string) {
            defer wg.Done()
            select {
            case <-ctx.Done():
                results[idx] = Response{Error: ctx.Err()}
            default:
                results[idx] = fetch(ctx, url)
            }
        }(i, url)
    }
    
    wg.Wait()
    return results
}
```

### 缓存策略

```go
// ✅ 多级缓存
type CacheService struct {
    local   *sync.Map      // 本地缓存
    redis   *redis.Client  // Redis 缓存
    db      *gorm.DB       // 数据库
}

func (s *CacheService) GetUser(ctx context.Context, id string) (*User, error) {
    // 1. 本地缓存
    if v, ok := s.local.Load(id); ok {
        return v.(*User), nil
    }
    
    // 2. Redis 缓存
    data, err := s.redis.Get(ctx, "user:"+id).Bytes()
    if err == nil {
        var user User
        json.Unmarshal(data, &user)
        s.local.Store(id, &user)  // 回填本地缓存
        return &user, nil
    }
    
    // 3. 数据库
    var user User
    if err := s.db.First(&user, "id = ?", id).Error; err != nil {
        return nil, err
    }
    
    // 回填缓存
    s.setCache(ctx, id, &user)
    return &user, nil
}

// ✅ 缓存击穿防护（singleflight）
var sf singleflight.Group

func (s *CacheService) GetUserSafe(ctx context.Context, id string) (*User, error) {
    v, err, _ := sf.Do(id, func() (interface{}, error) {
        return s.GetUser(ctx, id)
    })
    if err != nil {
        return nil, err
    }
    return v.(*User), nil
}
```

### 连接池优化

```go
// ✅ HTTP 客户端连接池
var httpClient = &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     90 * time.Second,
    },
    Timeout: 30 * time.Second,
}

// ✅ 数据库连接池
db.SetMaxOpenConns(100)
db.SetMaxIdleConns(10)
db.SetConnMaxLifetime(time.Hour)
db.SetConnMaxIdleTime(10 * time.Minute)

// ✅ Redis 连接池
rdb := redis.NewClient(&redis.Options{
    PoolSize:     100,
    MinIdleConns: 10,
    PoolTimeout:  4 * time.Second,
})
```

---

## 第三部分：数据库性能优化

### 查询优化

```sql
-- ✅ 使用覆盖索引
SELECT id, name, email FROM users WHERE email = 'test@example.com';
-- 索引：idx_users_email_name_id

-- ✅ 避免 SELECT *
SELECT id, name FROM users WHERE id = 1;  -- 只查需要的字段

-- ✅ 批量查询代替循环
SELECT * FROM products WHERE id IN (1, 2, 3, 4, 5);

-- ✅ 游标分页
SELECT * FROM orders WHERE id > 1000 ORDER BY id LIMIT 20;

-- ✅ 使用 EXPLAIN 分析
EXPLAIN ANALYZE SELECT * FROM orders WHERE user_id = 1;
```

### N+1 问题

```go
// ❌ N+1 问题
users := db.Find(&users)
for _, user := range users {
    db.Where("user_id = ?", user.ID).Find(&user.Orders)  // N 次查询
}

// ✅ 预加载解决 N+1
db.Preload("Orders").Find(&users)  // 2 次查询

// ✅ 手动批量查询
userIDs := extractIDs(users)
var orders []Order
db.Where("user_id IN ?", userIDs).Find(&orders)
orderMap := groupByUserID(orders)
for i := range users {
    users[i].Orders = orderMap[users[i].ID]
}
```

### 读写分离

```go
// ✅ 读写分离配置
db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})

db.Use(dbresolver.Register(dbresolver.Config{
    Sources:  []gorm.Dialector{mysql.Open(masterDSN)},
    Replicas: []gorm.Dialector{
        mysql.Open(replica1DSN),
        mysql.Open(replica2DSN),
    },
    Policy: dbresolver.RandomPolicy{},
}))

// 自动路由：读走从库，写走主库
db.Find(&users)           // 从库
db.Create(&user)          // 主库
db.Clauses(dbresolver.Write).Find(&users)  // 强制主库
```

---

## 第四部分：监控与诊断

### 性能指标

```go
// ✅ Prometheus 指标
var (
    httpRequestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "http_requests_total",
            Help: "Total HTTP requests",
        },
        []string{"method", "path", "status"},
    )
    
    httpRequestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "http_request_duration_seconds",
            Help:    "HTTP request duration",
            Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5},
        },
        []string{"method", "path"},
    )
)

// 中间件记录指标
func MetricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        
        rw := &responseWriter{ResponseWriter: w, status: 200}
        next.ServeHTTP(rw, r)
        
        duration := time.Since(start).Seconds()
        httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(rw.status)).Inc()
        httpRequestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
    })
}
```

### 性能分析

```go
// ✅ pprof 性能分析
import _ "net/http/pprof"

go func() {
    http.ListenAndServe("localhost:6060", nil)
}()

// 分析命令
// go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
// go tool pprof http://localhost:6060/debug/pprof/heap
// go tool pprof http://localhost:6060/debug/pprof/goroutine
```

### 日志追踪

```go
// ✅ 请求追踪
func TracingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        traceID := r.Header.Get("X-Trace-ID")
        if traceID == "" {
            traceID = uuid.New().String()
        }
        
        ctx := context.WithValue(r.Context(), "trace_id", traceID)
        
        logger := slog.With("trace_id", traceID)
        ctx = context.WithValue(ctx, "logger", logger)
        
        w.Header().Set("X-Trace-ID", traceID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

---

## 第五部分：原子化提交规范

> **强制要求**：性能优化变更必须遵循原子化提交原则。

### 原子提交原则

| 规则 | 说明 |
|------|------|
| **单一职责** | 一次提交只做一件事 |
| **可编译** | 每次提交后代码应能编译通过 |
| **可测试** | 每次提交后测试应能通过 |
| **可回滚** | 可以独立撤销而不影响其他功能 |

> 📚 **完整规范**：参考 [Git原子提交规范](../Git原子提交规范/SKILL.md)

---

## 性能优化清单

### 前端
- [ ] 代码分割和懒加载
- [ ] 图片压缩和懒加载
- [ ] 使用 memo/useMemo/useCallback
- [ ] 虚拟列表处理大数据
- [ ] 请求缓存和去重
- [ ] 预加载关键资源

### 后端
- [ ] 并发处理使用 goroutine 池
- [ ] 实现多级缓存
- [ ] 连接池配置合理
- [ ] 请求超时控制
- [ ] 批量操作替代循环

### 数据库
- [ ] 索引覆盖查询条件
- [ ] 解决 N+1 问题
- [ ] 慢查询日志开启
- [ ] 读写分离（如需要）
- [ ] 分页使用游标

### 监控
- [ ] 核心指标采集
- [ ] 链路追踪配置
- [ ] 告警规则设置
- [ ] 定期性能分析


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
