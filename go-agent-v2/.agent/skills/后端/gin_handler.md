# Gin Handler、路由与 Service 层

> **加载条件**: 写 HTTP Handler、配置路由分组、定义 Service 接口、统一响应格式时加载。

---

## 服务分层结构 (V2 标准)

技术栈: Gin + Validator v10 + Swag (OpenAPI)

```text
backend/internal/{service}/
├── dto/           # 请求/响应 DTO
├── handler/       # HTTP 控制器 (Gin)
├── service/       # 业务聚合层 (接口定义)
├── repo/          # 数据访问层 (GORM)
├── middleware/    # 中间件
└── router/        # 路由定义
```

---

## Handler 工厂模式

```go
type Handlers struct {
    Auth     *AuthHandler
    Strategy *StrategyHandler
    Config   *ConfigHandler
    Risk     *RiskHandler
    Stats    *StatsHandler
}

// Wire 注入依赖
func NewHandlers(
    authSvc service.AuthService,
    strategySvc service.StrategyService,
    configSvc service.ConfigService,
    riskSvc service.RiskService,
    statsSvc service.StatsService,
) *Handlers {
    return &Handlers{
        Auth:     NewAuthHandler(authSvc),
        Strategy: NewStrategyHandler(strategySvc),
        Config:   NewConfigHandler(configSvc),
        Risk:     NewRiskHandler(riskSvc),
        Stats:    NewStatsHandler(statsSvc),
    }
}
```

---

## 统一响应格式

MUST 使用统一响应辅助函数:

```go
func success(c *gin.Context, data interface{}) {
    c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func badRequest(c *gin.Context, code, message string) {
    c.JSON(http.StatusBadRequest, gin.H{
        "success": false,
        "error":   gin.H{"code": code, "message": message},
    })
}

func unauthorized(c *gin.Context, message string) {
    c.JSON(http.StatusUnauthorized, gin.H{
        "success": false,
        "error":   gin.H{"code": "unauthorized", "message": message},
    })
}

func notFound(c *gin.Context, message string) {
    c.JSON(http.StatusNotFound, gin.H{
        "success": false,
        "error":   gin.H{"code": "not_found", "message": message},
    })
}

func serverError(c *gin.Context, err error) {
    // ✅ 日志记录完整错误 (内部可见)
    logger.FromContext(c.Request.Context()).Error("internal error",
        logger.Any(logger.FieldError, err),
    )
    // ✅ 用户只看到通用消息 (NEVER 暴露 err.Error())
    c.JSON(http.StatusInternalServerError, gin.H{
        "success": false,
        "error":   gin.H{"code": "internal_error", "message": "服务器内部错误"},
    })
}
```

---

## Handler 实现示例

```go
type StrategyHandler struct {
    svc service.StrategyService
}

func NewStrategyHandler(svc service.StrategyService) *StrategyHandler {
    return &StrategyHandler{svc: svc}
}

func (h *StrategyHandler) List(c *gin.Context) {
    ctx := c.Request.Context()
    strategies, err := h.svc.List(ctx)
    if err != nil {
        logger.FromContext(ctx).Error("list strategies failed",
            logger.Any(logger.FieldError, err))
        serverError(c, err)
        return
    }
    success(c, strategies)
}

func (h *StrategyHandler) Get(c *gin.Context) {
    id := c.Param("id")
    strategy, err := h.svc.GetByID(c.Request.Context(), id)
    if err != nil {
        notFound(c, "Strategy not found")
        return
    }
    success(c, strategy)
}

func (h *StrategyHandler) Create(c *gin.Context) {
    var req dto.CreateStrategyRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        badRequest(c, "invalid_request", err.Error())
        return
    }
    ctx := c.Request.Context()
    strategy, err := h.svc.Create(ctx, &req)
    if err != nil {
        logger.FromContext(ctx).Error("create strategy failed",
            logger.Any(logger.FieldError, err))
        serverError(c, err)
        return
    }
    c.JSON(http.StatusCreated, gin.H{"success": true, "data": strategy})
}
```

---

## Service 接口定义

```go
package service

type StrategyService interface {
    List(ctx context.Context) ([]dto.StrategyResponse, error)
    GetByID(ctx context.Context, id string) (*dto.StrategyResponse, error)
    Create(ctx context.Context, req *dto.CreateStrategyRequest) (*dto.StrategyResponse, error)
    Update(ctx context.Context, id string, req *dto.UpdateStrategyRequest) (*dto.StrategyResponse, error)
    Delete(ctx context.Context, id string) error
    StartInstance(ctx context.Context, id string) error
    StopInstance(ctx context.Context, id string) error
}

type AuthService interface {
    Login(ctx context.Context, username, password string) (*dto.LoginResponse, error)
    GetAdminByID(ctx context.Context, id uint) (*dto.AdminInfo, error)
    GenerateToken(adminID uint) (string, time.Time)
}
```

---

## 路由分组

```go
package router

func Setup(r *gin.Engine, h *handler.Handlers) {
    v1 := r.Group("/v1/admin")

    // 公开路由 (无需认证)
    auth := v1.Group("/auth")
    {
        auth.POST("/login", h.Auth.Login)
    }

    // 需要认证的路由
    protected := v1.Group("")
    protected.Use(middleware.AuthRequired())
    {
        protected.GET("/auth/me", h.Auth.GetProfile)
        protected.POST("/auth/logout", h.Auth.Logout)

        strategy := protected.Group("/strategies")
        {
            strategy.GET("", h.Strategy.List)
            strategy.GET("/:id", h.Strategy.Get)
            strategy.POST("", h.Strategy.Create)
            strategy.PUT("/:id", h.Strategy.Update)
            strategy.DELETE("/:id", h.Strategy.Delete)
            strategy.POST("/:id/start", h.Strategy.StartInstance)
            strategy.POST("/:id/stop", h.Strategy.StopInstance)
        }

        config := protected.Group("/config")
        {
            config.GET("/system", h.Config.GetSystemConfig)
            config.PUT("/system", h.Config.UpdateSystemConfig)
        }
    }
}
```
