---
name: 注释规范
description: 编写或审查代码注释时使用 - 新建文件、重构后注释补全、注释质量审查、或代码可读性改进
aliases: ["@注释规范", "@注释", "@comment-standards"]
---

# 注释规范

## 核心哲学

**应注释尽注释。** 宁可多写注释，也不要让读者猜测意图。

> [!IMPORTANT]
> 黄金样板：`backend/cmd/engine/main.go`
> 任何注释风格的争议，以此文件为准。

## 6 层注释体系

### 层级 1：文件头（package 注释）

`package` 声明上方，说明包的职责。入口文件（`main.go`）可省略。

```go
// Package risk 实现基于配置的风控策略，
// 包括黑天鹅检测、波动率仓位调整和信号衰减。
package risk
```

---

### 层级 2：Doc Comment — 一个都不能少

**规则：每个 type、func、method、interface 都必须有 doc comment，无论导出还是非导出。**

以名称开头，一句话说明职责：

```go
// resolveMode 校验并标准化运行模式字符串。
func resolveMode(raw string) (string, error) {

// liveEngineDispatch 将多个账户引擎封装为统一的 engine.Engine 接口。
// Run 按序启动所有子引擎，任一失败则回滚已启动的；Stop 按逆序停止。
type liveEngineDispatch struct {

// Shutdown 按固定顺序关闭所有资源，收集所有错误后合并返回。
func (s *engineShutdowner) Shutdown(ctx context.Context) error {

// engineStopper 是引擎停止抽象（engine.Engine 满足此接口）。
type engineStopper interface {
```

**复杂函数用多行 doc comment，补充调用方需要知道的信息：**

```go
// setupLiveAccounts 解析 engine.accounts 配置，为每个账户构建引擎、Runtime、TradeListener，
// 返回聚合的 wiring 产物。失败时内部已回滚所有已启动资源，调用方无需额外清理。
func setupLiveAccounts(...) (*liveSetupResult, error) {

// startLiveFeed 启动 gRPC feed、Binance ticker feed 和 OrderBook 订阅。
// 成功时返回 provider + 合并后的 stop/cancel；失败时返回 error（调用方负责 shutdown 清理）。
func startLiveFeed(...) (*data.LiveGrpcProvider, func(), context.CancelFunc, error) {

// shutdownOnLiveFeedStartupFailure 在 live feed 启动失败时执行紧急清理。
// 将 liveRuntimeStop/liveCancel 注入 shutdowner 后立即触发完整 Shutdown。
func shutdownOnLiveFeedStartupFailure(...) error {
```

**未使用的代码也要注释标注：**

```go
// composeLiveRuntimeStop 为单个账户组合 listener+runtime 的停止函数。（当前未使用，保留备用）
func composeLiveRuntimeStop(...) func() {
```

---

### 层级 3：段落标题 — 长函数的路标

函数体超过 ~80 行时，用编号段落标题划分执行阶段。紧跟一行说明本段目标或约束。

**格式：** `// ── N. 标题 ──`

```go
func main() {
    // ── 1. 命令行参数定义 ──
    // 必须在 config.MustLoad() 之前定义，因为 config 内部调用 flag.Parse()。
    mode := flag.String(...)

    // ── 2. 配置加载 ──
    // config.MustLoad() 内部调用 flag.Parse()，解析上方定义的所有 flag。
    cfg := config.MustLoad()

    // ── 3. 审计日志初始化 ──
    runtimeCfg := runtimeConfigForMode(cfg, *mode)

    // ...

    // ── 11. 模式分发（核心 wiring）──
    // 各模式负责填充 eng（引擎实例）；live 模式额外产出 liveSetup 供后续 feed 启动。

    // ── 13. 信号监听 & 优雅关闭 ──
    // 此 goroutine 等待 SIGINT/SIGTERM，收到信号后执行 shutdowner.Shutdown() 释放全部资源。
    // 注意：liveRuntimeStop/liveCancel/liveProvider 在信号到达时才注入 shutdowner，
    // 因为它们的值可能在 eng.Run() 之后才被 startLiveFeed 赋值（闭包捕获 main 局部变量）。

    // ── 16. 收尾 ──
    // 回测类模式（backtest/paper/tick/vectorized）：打印结果后直接退出。
    // Live 模式：阻塞等待 SIGINT/SIGTERM 触发优雅关闭。
}
```

**main.go 样板中的完整 16 段编号：**

| # | 标题 | 职责 |
|---|------|------|
| 1 | 命令行参数定义 | flag 注册 |
| 2 | 配置加载 | config.MustLoad() |
| 3 | 审计日志初始化 | auditlog wiring |
| 4 | 数据层初始化 | Spider + MySQL |
| 5 | HTTP API 服务 | gin router + httpSrv |
| 6 | ML 策略注册 | mlRegistry |
| 7 | 策略插件加载 | loader |
| 8 | 引擎配置组装 | EngineConfig |
| 9 | 撮合引擎 | matching |
| 10 | 资金费率结算 | funding |
| 11 | 模式分发（核心 wiring）| switch *mode |
| 12 | 引擎实例化 | NewEngine |
| 13 | 信号监听 & 优雅关闭 | SIGINT/SIGTERM goroutine |
| 14 | 引擎启动 | eng.Run() |
| 15 | Live feed 连接 | startLiveFeed |
| 16 | 收尾 | backtest print / shutdown wait |

---

### 层级 4：分区标签 — 文件级函数分组

文件包含多组相关函数时，用分区标签划分区域，帮助快速导航。

**格式：** `// ----- 分组名称 -----`，独占一行，前后各留一个空行。

```go
// ----- 日志快捷方法 -----

func engineLog() *applog.Logger { ... }
func engineInfo(msg string, args ...any) { ... }
func engineInfof(format string, args ...any) { ... }

// ----- 接口/类型定义 -----

type shutdownHTTPServer interface { ... }
type engineStopper interface { ... }
type liveEngineDispatch struct { ... }

// ----- Live 模式辅助函数 -----

type liveSetupResult struct { ... }
func setupLiveAccounts(...) { ... }
func startLiveFeed(...) { ... }

// ----- 配置解析辅助函数 -----

func resolvePluginDir(...) { ... }
func resolveMode(...) { ... }

// ----- fanout 合并函数 -----
// 将多个 recorder/sink 合并为一个，事件广播给所有非 nil 目标。

func fanoutEventRecorders(...) { ... }
func fanoutTradeFlowSinks(...) { ... }
```

---

### 层级 5：行内注释 — 解释"为什么"

**原则：不复述代码做什么，只解释为什么这样做、有什么要注意的。**

```go
// 审计：记录人工停止事件
if businessAudit != nil { ... }

// 5s 超时内完成：live runtime → feed cancel → gRPC provider → engine → HTTP server
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)

// 正常返回触发 defer close(shutdownDone)，main() 通过 <-shutdownDone 退出，
// 确保所有 defer（otel/audit/mlRegistry/db）被执行。

// 审计日志桥接：auditlog.Logger → core.EventLogger → engine.EventLogger
if engineEventLogger != nil { ... }

// Binance ticker feed（可选，失败可忽略）
if resolveEngineBinanceFeedEnabled() { ... }

// gRPC feed（必须成功）
provider, err := enginecli.StartLiveGrpcFeed(...)

// OrderBook 订阅（可选）
if cfg.Spider.CoinAPI.EnableOrderBook { ... }
```

**特别需要行内注释的场景：**

- goroutine 的生命周期和退出条件
- 闭包捕获的变量语义
- 错误路径的清理责任归属（"调用方负责" vs "内部回滚"）
- 资源关闭顺序
- 可选 vs 必须成功的操作

---

### 层级 6：结构体字段注释

关键结构体的每个字段加行尾注释，对齐到相同列：

```go
type liveSetupResult struct {
    dispatch      *liveEngineDispatch    // 多账户引擎分发器
    runtimeStop   func()                 // 停止全部 Runtime 和 TradeListener
    eventRecorder core.EventLogRecorder  // 多账户合并的事件记录器
    tradeFlowSink enginecli.TradeFlowSink // 多账户合并的成交流 sink
    snapshotSink  enginecli.SnapshotSink  // 多账户合并的盘口快照 sink
    hub           *liveFeedHub           // Binance ticker 广播 hub
    symbols       []string               // 去重后的全部交易对
    exchange      string                 // 共享交易所
    market        string                 // 共享市场类型
    timeframe     string                 // 共享策略周期
    riskTimeframe string                 // 共享风控周期
    warmup        int                    // 所有策略中最大的预热 bar 数
}
```

**字段类型不超过 3 个时可省略**（如 `struct{ x int; y int }`），其余一律注释。

---

## const / var 组注释

常量和变量按语义分组，每组加一行组注释：

```go
// 引擎运行模式常量。
const (
    modeBacktest   = "backtest"
    modeLive       = "live"
    ...
)

// 引擎默认值与环境变量名称。
const (
    defaultEngineHTTPPort            int    = 9003
    defaultEngineLiveInitialCapital  int64  = 50000
    ...
)

// 构建时注入的版本信息（通过 -ldflags 设置）。
var (
    Version   = "v2.0.0"
    BuildTime = "unknown"
    GitCommit = "unknown"
)
```

---

## 语言规范

| 规则 | 示例 |
|------|------|
| **doc comment 用中文** | `// resolveMode 校验并标准化运行模式字符串。` |
| **错误消息用英文** | `return fmt.Errorf("unsupported mode: %s", raw)` |
| **技术术语保持原文** | gRPC、WebSocket、SIGINT、goroutine、defer |
| **中英混排不加空格** | `// 启动gRPC feed` → OK |
| **doc comment 句号结尾** | `// resolveMode 校验并标准化运行模式字符串。` ← 句号 |

---

## 审查清单

对文件执行注释审查时，按此顺序检查：

- [ ] **覆盖率** — 每个 type / func / method 是否有 doc comment？
- [ ] **名称开头** — doc comment 是否以被注释的标识符名称开头？
- [ ] **准确性** — 注释是否与当前代码行为一致？
- [ ] **段落标题** — 超 80 行的函数是否有编号段落 `── N. ──`？
- [ ] **分区标签** — 超 500 行的文件是否有 `----- 分组 -----`？
- [ ] **字段注释** — 关键结构体的字段是否有行尾注释？
- [ ] **const/var 组** — 是否按语义分组并加了组注释？
- [ ] **行内注释** — goroutine、闭包、错误路径、资源顺序是否有解释？
- [ ] **清洁度** — 无 emoji 前缀、无 tracker 标签、无 `========` 横幅、无过时引用？
- [ ] **未使用代码** — 是否标注了 `（当前未使用，保留备用）`？

## 常见错误

| 问题 | 修复 |
|------|------|
| 复述代码 `// 如果 err != nil 返回` | 删除，或改为 `// 校验 X 配置完整性` |
| doc comment 不以名称开头 | `// 关闭资源` → `// Shutdown 关闭资源` |
| 英文注释混在中文项目 | 统一为中文（技术术语和错误消息除外） |
| doc comment 缺失 | 补全，**即使是私有函数** |
| 过时注释引用已删除代码 | 重构后全局 `grep -rn "旧名称"` |
| 用 `========` 做分隔 | 改为 `── N. 标题 ──` 或 `----- 分组 -----` |
| `//nolint` 替代注释 | 先通过 `.golangci.yml` 排除，再用行内 nolint |
| Tracker 标签残留 | `// P1-15:` → 删除，改为描述性注释 |
| emoji 前缀 | `// 🆕 Flag 定义` → `// Flag 定义` |
