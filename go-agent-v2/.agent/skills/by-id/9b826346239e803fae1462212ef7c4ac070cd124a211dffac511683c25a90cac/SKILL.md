---
name: Go后端
description: 完整的 Go 后端开发指南，涵盖 Effective Go 最佳实践、并发模式、Web 服务器、数据库集成、微服务架构和生产部署。在编写、审查或重构 Go 代码时使用此技能。
tags: [golang, go, concurrency, web-servers, microservices, backend, goroutines, channels, grpc, rest-api, Go语言, Go后端, 后端开发, 微服务, 并发编程, API设计, 数据库, 性能优化]
---

# Go 后端开发规范

## 子文件按需加载

详细内容拆分在同目录子文件中。读完本文件后，根据任务用 `view_file` 加载**仅需的那 1 个子文件**。

| 加载场景 | 内容摘要 | 子文件 |
|---------|---------|-------|
| 命名变量/函数/包、格式化代码、写注释 | 命名 (MixedCaps/包名/接口名)、格式化 (gofmt/行宽120)、导入分组 (3组)、文档注释规范、Happy Path 编码 | `./naming_formatting.md` |
| 处理错误、定义错误码、errors.Is/As | 错误包装 (`%w`)、哨兵错误 (ErrNotFound/ErrUnauthorized)、自定义错误类型 (ValidationError/errors.As)、pkg/errors | `./error_handling.md` |
| 重构文件、消除重复、代码审查 | 文件组织红线 (_chains/50行/15文件)、DRY/工厂/Options 模式、接口设计 (1-3方法)、零值可用 (Buffer)、代码审查检查清单 | `./code_organization.md` |
| 新建模块、添加 API、配置加载、Wire 注入 | 目录结构 (cmd/internal/pkg)、业务模块结构、Config 模块 (Viper/mapstructure)、公共组件 (pkg/logger·pkg/errors)、添加新 API 的 SOP、GORM Gen 类型安全查询 | `./project_structure.md` |
| 写并发代码、goroutine/channel/mutex | Goroutine/Channel (有缓冲·无缓冲·方向)/Select/Context (WithCancel·WithTimeout)/WaitGroup/Mutex (RWMutex)、Pipeline/Fan-Out·Fan-In/Worker Pool/信号量 | `./concurrency_basics.md` |
| 金融并发控制、交易所 API 集成、限流/熔断 | 锁层次文档化 (Lock Hierarchy)、死锁踩坑与审查清单、订单簿细粒度锁 (读写分离)、原子操作 CAS (Account.Deduct)、Token Bucket 限流 (rate.Limiter 3种模式)、熔断器 (CircuitBreaker 3态+ExchangeAPI) | `./financial_concurrency.md` |
| 写 HTTP Handler、定义 Service 接口、配置路由 | 服务分层结构 (V2 标准 4层)、Handler 工厂模式 (NewHandlers)、统一响应格式 (success/badRequest/unauthorized/notFound/serverError)、Handler 实现示例、Service 接口定义 (StrategyService·AuthService)、路由分组 | `./gin_handler.md` |
| 写中间件、部署、监控、Service 实现 | 中间件 (CORS/JWT/结构化日志)、优雅关闭 (signal+Shutdown)、Panic 恢复 (gin.Recovery)、健康检查 (DB+Redis)、限流中间件 (rate.Limiter)、Prometheus 监控 (Counter/Gauge/Histogram/NewTimer)、Service 层实现 (缓存+GORM)、gRPC 快速示例 | `./gin_production.md` |
| 写测试、排查 bug、代码审查 | 表驱动测试、Gin Handler 测试 (httptest)、基准测试 (Benchmark)、测试辅助函数 (t.Helper/t.Cleanup/testing.TB)、测试组织规则、常见陷阱 (竞态/-race/goroutine泄漏/闭包捕获/nil接口)、错误速查表、Go 1.24+ wg.Go 新语法 | `./testing_pitfalls.md` |
| 快速查阅 Go 惯用写法、原则确认 | Effective Go 官方规则精选: 格式化/命名/错误处理/接口设计/并发/文档注释/控制结构/零值可用 (8 个章节，每条链接官方文档) | `./effective_go_rules.md` |
| 使用 SliceMap 泛型容器存储 map[K][]V 数据、防止 append 未回写 Bug | `pkg/collections.SliceMap[K,V]` 泛型容器使用指南 (Append/AppendWithLimit/GetRef/Get/Len/Range/Set/Delete)、API 速查表、滑动窗口用法 (AppendWithLimit)、`Get` 安全副本 vs `GetRef` 零拷贝选择、三种构建模式下行为一致性、已知高风险 `map[K][]V` 模块清单 | `./泛容器.md` |
| 使用运行时不变量校验验证数据一致性、slice 增长断言、debug 构建自动检查 | `AssertMinLen(key, n)` 单 key 校验、`AssertAllMinLen(n)` 全量校验、`buildmode.IsDebug()` const 编译时消除原理、`InvariantError` 结构化错误类型、三种构建模式行为 (Debug 执行校验/Release·Dev 编译器消除为 `return nil`)、自定义模块添加校验模式、测试中 `-tags=debug` 用法 | `./运行时不变量.md` |
| 区分 Release/Dev/Debug 三种环境、配置日志级别、添加仅调试代码 | `pkg/buildmode` 三级模式 (Release/Dev/Debug) const 常量 + 编译器死代码消除原理、`IsDebug()`/`IsDev()`/`IsRelease()` 便捷方法、logger 三层集成映射 (Production→Info+JSON / Development→Debug+Text / ModeDebug→Debug+Text+Source)、条件日志避免无用计算、Makefile 推荐配置 (dev/debug/release/test)、优先级规则 (debug>dev>release) | `./三级构建模式.md` |
| 检测 float64 累积计算精度漂移、长回测结果偏差、decimal vs float64 对比 | `AssertPrecisionBound(label, decimalVal, float64Val, maxDriftBPS)` 相对误差检测 (BPS)、`AssertAbsPrecisionBound` 绝对误差检测 (适用近零值)、影子对比/累积值校验/单点检查三种使用模式、推荐阈值表 (价格0.1BPS/ZScore5BPS/PnL1BPS)、`PrecisionDriftError` 结构化错误、与 SliceMap 不变量配合使用 | `./精度漂移不变量.md` |
| 写注释、审查注释质量、重构后注释补全 | 6 层注释体系 (文件头/doc comment/段落标题/分区标签/行内注释/字段注释)、应注释尽注释、const/var 组注释、语言规范 (中文注释+英文错误消息)、审查清单 10 项、黄金样板 `backend/cmd/engine/main.go` | `../注释规范/SKILL.md` |

---

## 核心强制规则 (始终生效，无需加载子文件)

### 格式化与命名

| 规则 | 要求 | 示例 |
|------|------|------|
| 格式化 | MUST `gofmt`，推荐 `goimports` 自动排序导入 | `goimports -w .` |
| 导出命名 | MixedCaps (大写开头) | `func NewService()`, `type OrderBook struct` |
| 未导出命名 | mixedCaps (小写开头) | `func parseConfig()`, `var configCache` |
| 包名 | 小写单词，无下划线，简短 | `user`, `httputil`, `config` |
| 接口命名 | 单方法接口: 动词+er 后缀 | `Reader`, `Writer`, `Closer` |
| NEVER | 下划线命名、拼音、缩写含糊 | ❌ `get_user`, `yong_hu`, `proc` |

### 错误处理 (三层错误体系 + 日志系统)

| 规则 | 要求 |
|------|------|
| ALWAYS 检查 | 每个返回 error 的调用 MUST 检查，NEVER `val, _ := fn()` |
| **NEVER** 单独包装 | **禁止** `fmt.Errorf("xxx: %w", err)` 逐层包装 |
| 同包内部 | 直接 `return err`，上下文由边界层负责 |
| 跨层边界 | 用 `errors.Wrap(err, op, msg)` 或构造 `EngineError`/`TradeError` |
| 日志统一记录 | Handler/入口层用 `logger.FromContext(ctx).Error(...)` + `logger.FieldXxx` 常量 |
| 哨兵错误 | 用 `pkg/errors` 预定义: `ErrNotFound`, `ErrUnauthorized` 等 |
| 引擎错误 | `errors.EngineError{Op, Code, Message, Err}` 带操作/错误码 |
| 交易错误 | `errors.NewTradeErrorWithCause(code, op, msg, err)` 带重试标记 |
| NEVER panic | 业务代码 NEVER 使用 `panic`，只在 init/不可恢复时用 |

### 并发

| 规则 | 要求 |
|------|------|
| Channel 优先 | 通信优先于共享内存，channel 优先于 mutex |
| Context 传递 | MUST 将 `context.Context` 作为函数第一个参数 |
| 锁层次 | 多锁结构体 MUST 注释锁获取顺序，防止死锁 |
| Goroutine 生命周期 | MUST 用 context 或 done channel 控制退出 |
| 竞态检测 | MUST 定期 `go test -race ./...` |

### 代码组织

| 规则 | 要求 |
|------|------|
| 禁止 chains | NEVER `_chains.go` 文件，0 容忍 |
| 下划线层数 | 文件名下划线 ≤3 层 |
| 单文件下限 | \<50 行的文件 MUST 合并到父文件 |
| 同包文件上限 | ≤30 个，超出则提取子包 |
| 公共组件 | MUST `pkg/logger` (基于 slog) + `pkg/errors`，NEVER 标准库 `log` |
| Package 声明 | NEVER 重复声明，编辑文件时保留原有 package |

### 接口设计

| 规则 | 要求 |
|------|------|
| 小接口 | 1-3 个方法，大接口拆分为组合 |
| 消费者定义 | 接口在使用方定义，而非实现方 |
| 返回具体类型 | 接受接口参数，返回具体类型 |
| 文档注释 | 导出+未导出符号 MUST 注释，以符号名开头。详见 `@注释` `../注释规范/SKILL.md`（应注释尽注释） |

### DRY 原则

| 信号 | 行动 |
|------|------|
| 3+ 处相似代码 | 提取为工厂函数 |
| 参数 >5 个 | 改为 Options 函数模式 |
| 重复 CRUD 逻辑 | 使用泛型 Repository (`BaseRepository[T]`) |
| 重复 error 处理 | 提取为中间件 |

---

## 项目文档索引

| 文档 | 路径 |
|------|------|
| 文档索引 | `docs/README.md` |
| 架构总览 | `docs/architecture/overview.md` |
| 后端服务 | `docs/architecture/backend-services.md` |
| 量化引擎 | `docs/architecture/quant-engine.md` |
| 前端应用 | `docs/architecture/frontend.md` |
| 基础设施 | `docs/architecture/infrastructure.md` |
| 开发指南 | `docs/guide/development.md` |
| 策略开发 | `docs/guide/strategy-development.md` |

---

## 跨技能引用

| 主题 | 文件 |
|------|------|
| GORM/Gen/Repository | `../GORM数据库操作/SKILL.md` |
| 六边形架构/DDD | `../架构设计/SKILL.md` |
| gRPC | `../gRPC服务设计/SKILL.md` |
| Git 提交 | `../Git原子提交规范/SKILL.md` |
| 注释规范（应注释尽注释） | `../注释规范/SKILL.md`，黄金样板: `backend/cmd/engine/main.go` |
