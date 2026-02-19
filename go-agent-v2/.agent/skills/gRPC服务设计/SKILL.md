---
name: gRPC 服务设计
description: gRPC 微服务架构设计与 Go 集成最佳实践，涵盖 Protobuf 定义、服务实现、拦截器、负载均衡和错误处理。适用于高性能微服务通信。
tags: [gRPC, Protobuf, 微服务, Go, RPC, 负载均衡, 拦截器]
---

# gRPC 服务设计

## 概述

gRPC 是 Google 开发的高性能 RPC 框架，特点：
- **高性能** - 基于 HTTP/2 和 Protobuf
- **强类型** - IDL 定义接口契约
- **多语言** - 自动生成多语言客户端
- **流式支持** - 支持双向流式通信

## Protobuf 定义规范

### 基础服务定义

```protobuf
syntax = "proto3";

package trading.v1;

option go_package = "github.com/quant-trading-system/go-engine/api/trading/v1;tradingv1";

import "google/protobuf/timestamp.proto";
import "google/protobuf/empty.proto";

// 订单服务
service OrderService {
  // 一元 RPC
  rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse);
  rpc GetOrder(GetOrderRequest) returns (Order);
  rpc CancelOrder(CancelOrderRequest) returns (google.protobuf.Empty);
  
  // 服务端流式 RPC
  rpc StreamOrders(StreamOrdersRequest) returns (stream Order);
  
  // 客户端流式 RPC
  rpc BatchCreateOrders(stream CreateOrderRequest) returns (BatchCreateOrdersResponse);
  
  // 双向流式 RPC
  rpc OrderUpdates(stream OrderUpdateRequest) returns (stream OrderUpdate);
}

// 请求消息
message CreateOrderRequest {
  string symbol = 1;
  OrderSide side = 2;
  OrderType type = 3;
  string quantity = 4;  // 使用 string 表示精确数值
  string price = 5;     // 可选，限价单需要
}

// 响应消息
message CreateOrderResponse {
  string order_id = 1;
  OrderStatus status = 2;
  google.protobuf.Timestamp created_at = 3;
}

// 枚举定义
enum OrderSide {
  ORDER_SIDE_UNSPECIFIED = 0;
  ORDER_SIDE_BUY = 1;
  ORDER_SIDE_SELL = 2;
}

enum OrderType {
  ORDER_TYPE_UNSPECIFIED = 0;
  ORDER_TYPE_MARKET = 1;
  ORDER_TYPE_LIMIT = 2;
  ORDER_TYPE_STOP_LOSS = 3;
}

enum OrderStatus {
  ORDER_STATUS_UNSPECIFIED = 0;
  ORDER_STATUS_PENDING = 1;
  ORDER_STATUS_FILLED = 2;
  ORDER_STATUS_CANCELLED = 3;
  ORDER_STATUS_REJECTED = 4;
}

// 实体消息
message Order {
  string id = 1;
  string symbol = 2;
  OrderSide side = 3;
  OrderType type = 4;
  string quantity = 5;
  string price = 6;
  string filled_quantity = 7;
  OrderStatus status = 8;
  google.protobuf.Timestamp created_at = 9;
  google.protobuf.Timestamp updated_at = 10;
}
```

### Proto 文件组织

```
api/
├── trading/
│   └── v1/
│       ├── order.proto
│       ├── position.proto
│       └── account.proto
├── market/
│   └── v1/
│       ├── quote.proto
│       └── kline.proto
└── common/
    └── v1/
        ├── pagination.proto
        └── error.proto
```

## 服务端实现

### 基础服务实现

```go
package server

import (
    "context"
    
    tradingv1 "github.com/quant-trading-system/go-engine/api/trading/v1"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

type OrderServer struct {
    tradingv1.UnimplementedOrderServiceServer
    orderRepo OrderRepository
}

func NewOrderServer(repo OrderRepository) *OrderServer {
    return &OrderServer{orderRepo: repo}
}

func (s *OrderServer) CreateOrder(ctx context.Context, req *tradingv1.CreateOrderRequest) (*tradingv1.CreateOrderResponse, error) {
    // 参数验证
    if req.Symbol == "" {
        return nil, status.Error(codes.InvalidArgument, "symbol is required")
    }
    if req.Side == tradingv1.OrderSide_ORDER_SIDE_UNSPECIFIED {
        return nil, status.Error(codes.InvalidArgument, "side is required")
    }

    // 业务逻辑
    order, err := s.orderRepo.Create(ctx, req)
    if err != nil {
        return nil, status.Errorf(codes.Internal, "failed to create order: %v", err)
    }

    return &tradingv1.CreateOrderResponse{
        OrderId:   order.ID,
        Status:    tradingv1.OrderStatus_ORDER_STATUS_PENDING,
        CreatedAt: timestamppb.Now(),
    }, nil
}

func (s *OrderServer) GetOrder(ctx context.Context, req *tradingv1.GetOrderRequest) (*tradingv1.Order, error) {
    order, err := s.orderRepo.GetByID(ctx, req.OrderId)
    if err != nil {
        if errors.Is(err, ErrOrderNotFound) {
            return nil, status.Error(codes.NotFound, "order not found")
        }
        return nil, status.Errorf(codes.Internal, "failed to get order: %v", err)
    }
    return order.ToProto(), nil
}
```

### 流式 RPC 实现

```go
// 服务端流式
func (s *OrderServer) StreamOrders(req *tradingv1.StreamOrdersRequest, stream tradingv1.OrderService_StreamOrdersServer) error {
    ctx := stream.Context()
    
    orderChan := s.orderRepo.Subscribe(ctx, req.UserId)
    defer s.orderRepo.Unsubscribe(req.UserId)

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case order := <-orderChan:
            if err := stream.Send(order.ToProto()); err != nil {
                return err
            }
        }
    }
}

// 双向流式
func (s *OrderServer) OrderUpdates(stream tradingv1.OrderService_OrderUpdatesServer) error {
    for {
        req, err := stream.Recv()
        if err == io.EOF {
            return nil
        }
        if err != nil {
            return err
        }

        // 处理请求
        update := s.processUpdate(stream.Context(), req)
        
        if err := stream.Send(update); err != nil {
            return err
        }
    }
}
```

## 拦截器 (Interceptor)

### 日志拦截器

```go
import (
    "context"
    "time"
    
    "go.uber.org/zap"
    "google.golang.org/grpc"
)

func LoggingUnaryInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        start := time.Now()
        
        resp, err := handler(ctx, req)
        
        duration := time.Since(start)
        
        if err != nil {
            logger.Error("RPC failed",
                zap.String("method", info.FullMethod),
                zap.Duration("duration", duration),
                zap.Error(err),
            )
        } else {
            logger.Info("RPC completed",
                zap.String("method", info.FullMethod),
                zap.Duration("duration", duration),
            )
        }
        
        return resp, err
    }
}
```

### 认证拦截器

```go
func AuthUnaryInterceptor(authService AuthService) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        // 跳过健康检查
        if info.FullMethod == "/grpc.health.v1.Health/Check" {
            return handler(ctx, req)
        }

        // 提取 token
        md, ok := metadata.FromIncomingContext(ctx)
        if !ok {
            return nil, status.Error(codes.Unauthenticated, "missing metadata")
        }

        tokens := md.Get("authorization")
        if len(tokens) == 0 {
            return nil, status.Error(codes.Unauthenticated, "missing token")
        }

        // 验证 token
        userID, err := authService.ValidateToken(tokens[0])
        if err != nil {
            return nil, status.Error(codes.Unauthenticated, "invalid token")
        }

        // 注入用户信息到上下文
        ctx = context.WithValue(ctx, UserIDKey, userID)
        
        return handler(ctx, req)
    }
}
```

### 恢复拦截器

```go
func RecoveryUnaryInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
        defer func() {
            if r := recover(); r != nil {
                logger.Error("Panic recovered",
                    zap.String("method", info.FullMethod),
                    zap.Any("panic", r),
                    zap.Stack("stack"),
                )
                err = status.Error(codes.Internal, "internal server error")
            }
        }()
        return handler(ctx, req)
    }
}
```

## 服务端配置

### 完整服务启动

```go
import (
    "net"
    
    "google.golang.org/grpc"
    "google.golang.org/grpc/health"
    "google.golang.org/grpc/health/grpc_health_v1"
    "google.golang.org/grpc/keepalive"
    "google.golang.org/grpc/reflection"
)

func NewGRPCServer(logger *zap.Logger, authService AuthService) *grpc.Server {
    opts := []grpc.ServerOption{
        // 拦截器链
        grpc.ChainUnaryInterceptor(
            RecoveryUnaryInterceptor(logger),
            LoggingUnaryInterceptor(logger),
            AuthUnaryInterceptor(authService),
        ),
        // Keep-alive 配置
        grpc.KeepaliveParams(keepalive.ServerParameters{
            MaxConnectionIdle:     15 * time.Minute,
            MaxConnectionAge:      30 * time.Minute,
            MaxConnectionAgeGrace: 5 * time.Second,
            Time:                  5 * time.Second,
            Timeout:               1 * time.Second,
        }),
        grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
            MinTime:             5 * time.Second,
            PermitWithoutStream: true,
        }),
        // 消息大小限制
        grpc.MaxRecvMsgSize(4 * 1024 * 1024), // 4MB
        grpc.MaxSendMsgSize(4 * 1024 * 1024), // 4MB
    }

    server := grpc.NewServer(opts...)

    // 注册服务
    tradingv1.RegisterOrderServiceServer(server, NewOrderServer(orderRepo))
    
    // 健康检查
    healthServer := health.NewServer()
    grpc_health_v1.RegisterHealthServer(server, healthServer)
    healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
    
    // 反射（开发环境）
    reflection.Register(server)

    return server
}

func main() {
    lis, err := net.Listen("tcp", ":50051")
    if err != nil {
        log.Fatalf("failed to listen: %v", err)
    }

    server := NewGRPCServer(logger, authService)
    
    log.Printf("gRPC server listening at %v", lis.Addr())
    if err := server.Serve(lis); err != nil {
        log.Fatalf("failed to serve: %v", err)
    }
}
```

## 客户端实现

### 客户端连接

```go
import (
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    "google.golang.org/grpc/keepalive"
)

func NewGRPCClient(target string) (*grpc.ClientConn, error) {
    opts := []grpc.DialOption{
        grpc.WithTransportCredentials(insecure.NewCredentials()),
        grpc.WithKeepaliveParams(keepalive.ClientParameters{
            Time:                10 * time.Second,
            Timeout:             3 * time.Second,
            PermitWithoutStream: true,
        }),
        // 负载均衡
        grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
    }

    conn, err := grpc.Dial(target, opts...)
    if err != nil {
        return nil, err
    }

    return conn, nil
}
```

### 带重试的客户端

```go
import "google.golang.org/grpc/codes"

retryPolicy := `{
    "methodConfig": [{
        "name": [{"service": "trading.v1.OrderService"}],
        "retryPolicy": {
            "maxAttempts": 3,
            "initialBackoff": "0.1s",
            "maxBackoff": "1s",
            "backoffMultiplier": 2,
            "retryableStatusCodes": ["UNAVAILABLE", "RESOURCE_EXHAUSTED"]
        }
    }]
}`

conn, _ := grpc.Dial(target,
    grpc.WithDefaultServiceConfig(retryPolicy),
)
```

## 错误处理

### 错误码映射

| gRPC Code | HTTP | 场景 |
|-----------|------|------|
| `OK` | 200 | 成功 |
| `INVALID_ARGUMENT` | 400 | 参数错误 |
| `NOT_FOUND` | 404 | 资源不存在 |
| `ALREADY_EXISTS` | 409 | 资源已存在 |
| `PERMISSION_DENIED` | 403 | 权限不足 |
| `UNAUTHENTICATED` | 401 | 未认证 |
| `RESOURCE_EXHAUSTED` | 429 | 限流 |
| `INTERNAL` | 500 | 内部错误 |
| `UNAVAILABLE` | 503 | 服务不可用 |

### 带详情的错误

```go
import (
    "google.golang.org/genproto/googleapis/rpc/errdetails"
    "google.golang.org/grpc/status"
)

func validationError(field, desc string) error {
    st := status.New(codes.InvalidArgument, "validation failed")
    
    badRequest := &errdetails.BadRequest{
        FieldViolations: []*errdetails.BadRequest_FieldViolation{
            {
                Field:       field,
                Description: desc,
            },
        },
    }
    
    st, _ = st.WithDetails(badRequest)
    return st.Err()
}
```

## 最佳实践

1. **版本控制** - 使用 `v1`, `v2` 包路径
2. **向后兼容** - 只添加字段，不修改/删除
3. **使用 string 表示精确数值**（金融场景）
4. **合理设置超时**（推荐 30s 内）
5. **启用健康检查**用于负载均衡探测

---

## 超时与 Deadline 最佳实践

> 📚 参考来源：[grpc.io](https://grpc.io) + 社区最佳实践

### 核心原则

| 规则 | 说明 |
|------|------|
| **始终设置 deadline** | 永远不要发起无 deadline 的 RPC |
| **timeout vs deadline** | timeout 是持续时间，deadline 是绝对时间 |
| **传播 deadline** | 通过 context 自动向下游传播 |
| **处理 DEADLINE_EXCEEDED** | 客户端必须处理此错误 |

### 客户端超时设置

```go
// ✅ 正确：始终设置超时
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

resp, err := client.CreateOrder(ctx, req)
if err != nil {
    if status.Code(err) == codes.DeadlineExceeded {
        // 超时处理
        log.Warn("request timeout")
    }
    return err
}
```

### 服务端 deadline 检查

```go
func (s *OrderServer) CreateOrder(ctx context.Context, req *Request) (*Response, error) {
    // 检查是否已超时
    if ctx.Err() == context.DeadlineExceeded {
        return nil, status.Error(codes.DeadlineExceeded, "deadline exceeded")
    }

    // 长操作前检查 deadline
    deadline, ok := ctx.Deadline()
    if ok && time.Until(deadline) < 100*time.Millisecond {
        return nil, status.Error(codes.DeadlineExceeded, "insufficient time")
    }

    // 继续处理...
}
```

### 超时建议值

| 场景 | 推荐超时 |
|------|---------|
| 查询操作 | 1-5s |
| 写入操作 | 5-10s |
| 批量操作 | 30s |
| 流式 RPC | 按需设置 |

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
