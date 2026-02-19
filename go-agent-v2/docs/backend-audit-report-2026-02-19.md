# 后端代码审查报告（2026-02-19）

## 1. 审查范围

- 仓库：`go-agent-v2`
- 代码范围：`cmd/`、`internal/`、`pkg/` 后端 Go 代码
- 审查目标：识别功能性 bug、并发/稳定性风险、工程合规问题（错误处理、死代码、边界控制等）

## 2. 审查方法与执行结果

### 2.1 静态与测试命令

```bash
go test ./...
go test -race ./...
go vet ./...
golangci-lint run ./...
gosec ./...
```

### 2.2 结果摘要

- `go test ./...`：通过
- `go test -race ./...`：通过
- `go vet ./...`：通过
- `golangci-lint run ./...`：失败，**56 项问题**
  - `errcheck`: 21
  - `staticcheck`: 6
  - `unused`: 29
- `gosec ./...`：当前环境未安装（`command not found`）

## 3. 总体结论

- 当前后端可编译、测试可通过，但存在 **高优先级稳定性/一致性风险**（迁移流程容错语义、接口实现与注册不一致、返回值未消费等）。
- 合规层面主要集中在：
  - 错误返回值未检查（大面积）
  - 未使用代码堆积
  - 调试能力的跨域放宽策略
- 建议先做一轮“最小修复集”（P0/P1），再清理长尾质量问题。

## 4. 关键问题清单（按优先级）

| ID | 等级 | 类型 | 问题描述 | 证据定位 | 影响 | 建议 |
|---|---|---|---|---|---|---|
| B-001 | P0 | 逻辑缺陷 | 迁移脚本执行失败后仅打印错误并继续，最终仍输出“Migration complete” | `cmd/migrate/main.go:47`、`cmd/migrate/main.go:56` | 可能出现部分迁移成功、部分失败，数据库 schema 不一致 | 任一 migration 失败即退出并返回非 0；记录失败文件名与 SQL 错误 |
| B-002 | P0 | 启动一致性 | 主服务启动时迁移失败被标记为 non-fatal 并继续启动 | `cmd/app-server/main.go:56` | 业务在旧 schema 上运行，运行期错误后置爆发 | 生产模式默认 fail-fast；仅开发模式允许降级（需显式开关） |
| B-003 | P1 | 行为不一致 | `account/login/cancel` 被注册为 `noop`，真实实现未被路由使用（且被识别为 unused） | `internal/apiserver/methods.go:103`、`internal/apiserver/methods.go:2472` | API 行为与实现不一致，后续维护易误判 | 路由注册改为真实 handler，或删除未使用实现并更新协议文档 |
| B-004 | P1 | 逻辑可疑 | 广播路径中对 `enqueueConnMessage` 返回值的判断为空效果（SA4006） | `internal/apiserver/server.go:604` | 背压/断连分支语义不清，后续演进可能引入真实 bug | 去掉无效判断或补充失败处理（指标/日志/计数） |
| C-001 | P1 | 合规（错误处理） | 多处未检查 error 返回值 | 示例：`cmd/agent-terminal/main.go:69`、`cmd/agent-terminal/debug_server.go:226`、`internal/apiserver/server.go:2100`、`internal/service/workspace.go:745` | 资源释放、响应写入、环境变量设置等失败被静默吞掉 | 按模块批量补齐错误检查；统一 close/write 错误处理模板 |
| C-002 | P2 | 合规（上下文） | `context.Context` 形参存在但未使用 | `internal/codex/client.go:195` | 无法利用取消信号终止扫描，影响可控性 | 在端口扫描循环中监听 `ctx.Done()` 并尽早返回 |
| C-003 | P2 | 合规（边界控制） | CORS 在中间件和 SSE 路径均为 `*` | `internal/apiserver/server.go:2139`、`internal/apiserver/server.go:2161` | 若监听地址被误配到非 loopback，会扩大跨域访问面 | 绑定本地时可保留；否则使用白名单 Origin 策略 |
| C-004 | P3 | 可维护性 | 未使用函数/常量较多（`unused` 29 项） | 代表：`internal/orchestrator/master_logic.go:19`、`internal/telegram/bridge_logic.go:30`、`internal/apiserver/dashboard_methods.go:22` | 增加认知负担，掩盖真实回归 | 清理死代码或补测试激活；按子模块逐步收敛 |

## 5. 典型问题展开说明

### 5.1 迁移容错语义与生产风险

- CLI 迁移器当前对单个 migration 失败采取“记录后继续”，这在测试环境可接受，但在生产会形成不可预测状态。
- 服务启动路径又将迁移失败降级为告警，叠加后风险放大。
- 建议统一为：**迁移失败阻断启动**，并通过显式配置允许开发态降级。

### 5.2 大面积 errcheck 违规

- 主要集中在：
  - `defer Close()` 未处理返回错误
  - `fmt.Fprint/Fprintf` 未处理返回错误
  - `json.NewEncoder(...).Encode(...)` 未处理返回错误
- 这些问题短期看不一定触发故障，但属于高频“隐形失效”源头。

### 5.3 路由注册与实现脱节

- 代码中存在实现函数但未在方法表绑定（`account/login/cancel`）。
- 这是接口层常见回归源，建议用“方法表覆盖率测试”防止再发。

## 6. 修复优先级建议

### 第一阶段（当日完成）

1. 修复 B-001、B-002（迁移失败策略统一）
2. 修复 B-003（方法注册一致性）
3. 修复 B-004（去除无效判断或补处理）

### 第二阶段（1~2 天）

1. 批量清理 `errcheck`（先运行路径、再测试代码）
2. 收紧 CORS 策略（按环境配置）
3. 补齐 context 取消链路

### 第三阶段（持续治理）

1. 清理 `unused` 项
2. 为路由注册、迁移策略增加回归测试
3. 将 `golangci-lint` 纳入 CI 必过门禁

## 7. 附录：本次重点定位文件

- `cmd/migrate/main.go`
- `cmd/app-server/main.go`
- `internal/apiserver/server.go`
- `internal/apiserver/methods.go`
- `cmd/agent-terminal/main.go`
- `cmd/agent-terminal/debug_server.go`
- `internal/codex/client.go`
- `internal/store/db_query.go`
- `internal/service/workspace.go`
- `internal/database/migrator.go`

