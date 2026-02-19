# Gin 中间件与生产部署

> **加载条件**: 写中间件 (CORS/JWT/日志/限流)、优雅关闭、健康检查、Prometheus、Service 层实现、gRPC 时加载。

---

## 中间件

### CORS

```go
func CORS() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Header("Access-Control-Allow-Origin", "*")
        c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
        if c.Request.Method == "OPTIONS" {
            c.AbortWithStatus(http.StatusNoContent)
            return
        }
        c.Next()
    }
}
```

### JWT 认证

```go
func AuthRequired() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")
        if token == "" {
            c.JSON(http.StatusUnauthorized, gin.H{
                "success": false,
                "error":   gin.H{"code": "missing_token", "message": "Token required"},
            })
            c.Abort()
            return
        }
        if len(token) > 7 && token[:7] == "Bearer " {
            token = token[7:]
        }
        claims, err := validateJWT(token)
        if err != nil {
            c.JSON(http.StatusUnauthorized, gin.H{
                "success": false,
                "error":   gin.H{"code": "invalid_token", "message": err.Error()},
            })
            c.Abort()
            return
        }
        c.Set("admin_id", claims.AdminID)
        c.Next()
    }
}
```

### 日志中间件

```go
import logger "github.com/quant-trading-system/wjboot/v2/pkg/logger"

func RequestLogger() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        path := c.Request.URL.Path
        c.Next()
        logger.FromContext(c.Request.Context()).Info("request",
            logger.String("method", c.Request.Method),
            logger.String("path", path),
            logger.Int(logger.FieldStatus, c.Writer.Status()),
            logger.Duration(logger.FieldLatencyMS, time.Since(start)),
        )
    }
}
```

---

## 优雅关闭

```go
func main() {
    r := gin.Default()
    router.Setup(r, handlers)

    srv := &http.Server{Addr: ":8081", Handler: r}

    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Fatalf("listen: %s", err)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    logger.Info("[Shutdown] 正在优雅关闭...")

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if err := srv.Shutdown(ctx); err != nil {
        logger.Fatalf("[Shutdown] 强制关闭: %v", err)
    }
    logger.Info("[Shutdown] 服务已退出")
}
```

---

## Panic 恢复

`gin.Default()` 已包含 Recovery 中间件。手动配置:

```go
r := gin.New()
r.Use(gin.Logger())
r.Use(gin.Recovery())
```

---

## 健康检查

```go
func healthCheck(db *gorm.DB) gin.HandlerFunc {
    return func(c *gin.Context) {
        sqlDB, err := db.DB()
        if err != nil {
            logger.FromContext(c.Request.Context()).Error("get sql db failed",
                logger.Any(logger.FieldError, err))
            c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy"})
            return
        }
        if err := sqlDB.Ping(); err != nil {
            c.JSON(http.StatusServiceUnavailable, gin.H{
                "status": "unhealthy", "error": err.Error(),
            })
            return
        }
        c.JSON(http.StatusOK, gin.H{"status": "healthy", "service": "admin"})
    }
}
```

---

## 限流中间件

```go
import "golang.org/x/time/rate"

func rateLimitMiddleware(limiter *rate.Limiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !limiter.Allow() {
                http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}

// 10 请求/秒，突发 20
limiter := rate.NewLimiter(rate.Limit(10), 20)
```

---

## Prometheus 监控 (基础)

```go
import "github.com/prometheus/client_golang/prometheus"

// Counter: 只增不减 (请求总数、错误总数)
var httpRequestsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{Name: "http_requests_total", Help: "Total HTTP requests"},
    []string{"method", "endpoint", "status"},
)

// Gauge: 可增可减 (活跃连接数、队列长度)
var activeConnections = prometheus.NewGauge(
    prometheus.GaugeOpts{
        Name: "active_connections",
        Help: "Number of active connections",
    },
)

// Histogram: 分布统计 (响应时间、请求大小)
var httpDuration = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name:    "http_request_duration_seconds",
        Help:    "HTTP request latency",
        Buckets: prometheus.DefBuckets,
    },
    []string{"method", "endpoint"},
)

func init() {
    prometheus.MustRegister(httpRequestsTotal)
    prometheus.MustRegister(activeConnections)
    prometheus.MustRegister(httpDuration)
}

// 使用示例
func handler(w http.ResponseWriter, r *http.Request) {
    timer := prometheus.NewTimer(httpDuration.WithLabelValues(r.Method, r.URL.Path))
    defer timer.ObserveDuration()

    httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, "200").Inc()
    // 处理请求...
}
```

---

## Service 层实现模式

```go
import applog "github.com/quant-trading-system/wjboot/v2/pkg/logger"

type UserServiceImpl struct {
    db     *gorm.DB
    cache  *redis.Client
    logger *applog.Logger  // ✅ 使用 pkg/logger.Logger (= slog.Logger 别名)
}

func NewUserService(db *gorm.DB, cache *redis.Client, logger *applog.Logger) UserService {
    return &UserServiceImpl{db: db, cache: cache, logger: logger}
}

func (s *UserServiceImpl) GetUser(ctx context.Context, userID uint) (*dto.UserResponse, error) {
    // 先查缓存
    if user, err := s.getFromCache(ctx, userID); err == nil {
        return user, nil
    }
    // 查数据库
    var user model.User
    if err := s.db.WithContext(ctx).First(&user, userID).Error; err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return nil, ErrUserNotFound
        }
        return nil, err  // ✅ 同包内部直接返回，NEVER fmt.Errorf 逐层包装
    }
    go s.updateCache(context.Background(), &user)
    return dto.ToUserResponse(&user), nil
}
```

---

## gRPC 快速示例

> 完整指南: [gRPC服务设计](../gRPC服务设计/SKILL.md)

```go
// 1. Protobuf
service StrategyService {
  rpc Execute(Request) returns (Response);
}

// 2. 实现
type StrategyServer struct {
    pb.UnimplementedStrategyServiceServer
}

func (s *StrategyServer) Execute(ctx context.Context, req *pb.Request) (*pb.Response, error) {
    return &pb.Response{Result: "success"}, nil
}
```
