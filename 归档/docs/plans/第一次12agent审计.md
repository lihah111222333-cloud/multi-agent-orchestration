# 第一次12agent审计（V2汇总）

## 基本信息
- 审计线程: `audit:quant-engine-current:956fd696`
- 模块范围: `backend/internal/engine` 及其子目录
- 裁判: `agent_12`
- 裁决文件: `/Users/mima0000/Desktop/wj/wjboot-v2/audits/audit:quant-engine-current:956fd696/judge/agent_12.md`
- 修复共识: `/Users/mima0000/Desktop/wj/wjboot-v2/audits/audit:quant-engine-current:956fd696/judge/remediation_consensus_v2.md`

## 覆盖与过程核对
- `agent_01..agent_11` round 报告均为 20 轮。
- `agent_01..agent_11` peer discussion 均为 20 轮。
- `agent_01..agent_11` final 报告齐全。
- 裁判结论: `decision: accepted`。

## 审计结论
- 结论: **有条件通过**
- 条件: 按 P0/P1 优先级先修复关键并发与风控问题，再做回归验证。

## 采纳问题（证据 + 影响 + 修复指南）

### [P0] 风控入口空指针
- 证据: `backend/internal/engine/risk/risk_controller.go:587`
- 影响: 风控入口 panic，订单链路中断。
- 修复指南: `CheckOrder` 入口增加 nil guard，返回 typed error，并补充 nil 输入回归测试。

### [P0] Dispatcher Do/Stop 并发竞态
- 证据: `backend/internal/engine/engine/dispatcher.go:84,96-103`
- 影响: `send on closed channel`，导致 panic 或任务丢失。
- 修复指南: Do/Stop 串行化、close 幂等、关闭后 Do 快速失败（`ErrClosed`）。

### [P0] Queue Stop 重复关闭
- 证据: `backend/internal/engine/live/queue/queue.go:159`
- 影响: `close of closed channel`，停机不可靠。
- 修复指南: 使用 `sync.Once` 或 CAS 实现 Stop 幂等，并在 Stop 后拒绝 Enqueue。

### [P1] Funding Settler 幂等键错误
- 证据: `backend/internal/engine/funding/settler.go:30,91,129`
- 影响: 多仓位漏结算/重复结算，PnL 偏移。
- 修复指南: 改为 `position|symbol|utc_slot` 幂等键，统一 UTC 槽位归一。

### [P1] Funding Provider 未透传 ctx
- 证据: `backend/internal/engine/funding/binance_provider.go:121`
- 影响: 取消/超时不透传，关停尾延迟放大。
- 修复指南: request/backoff 全链路透传调用方 `ctx`。

### [P1] Aggregator 有效源与时效权重错误
- 证据: `backend/internal/engine/data/aggregator.go:164,175,178,200`
- 影响: 可用性误判，聚合价格偏差。
- 修复指南: freshness clamp；仅 `Mid()` 有效源进入 required/分母与加权。

### [P1] Trigger Manager 单值覆盖与取消路径错误
- 证据: `backend/internal/engine/live/trigger/manager.go:36,405`
- 影响: 触发状态丢失，本地取消闭环失败。
- 修复指南: 改为多记录结构（`triggerID` 或 `map[orderID][]record`），增加本地取消分支。

### [P2] Iceberg 子单超量与生命周期约束不足
- 证据: `backend/internal/engine/context/live_order.go:167,184`
- 影响: 超额下单；停止后子协程仍可能继续下单。
- 修复指南: 残量 cap、子协程绑定 `ctx`、纳入生命周期托管。

### [P2] Spider Orderbook Fetch 切断取消链
- 证据: `backend/internal/engine/data/source/spider_orderbook_provider_fetch.go:25,42`
- 影响: 请求悬挂、恢复抖动。
- 修复指南: `getLatest/getHistory/getJSON` 全链路透传调用方 `ctx`。

### [P3] Baseline Manifest 漂移
- 证据: `backend/internal/engine/attribution/regression/baseline_manifest_regression_test.go:90`
- 影响: CI/回归门禁噪声增加。
- 修复指南: 按流程刷新并锁定 baseline。

## 驳回/待复现
- `repro-needed`: `backend/internal/engine/engine/live.go:136,162`
  - 原因: 缺稳定复现实验（超时 + 延迟回写场景）。
- `repro-needed`: `backend/internal/engine/live/leverage/watcher.go:141,145`
  - 原因: 缺运行时 CPU/日志证据量化。
- `repro-needed`: `backend/internal/engine/live/startup/startup_checker.go:247,261`
  - 原因: 缺失败注入最小复现证据。
- `rejected-as-P0`: `backend/internal/engine/attribution/regression/baseline_manifest_regression_test.go:90`
  - 原因: 属交付门禁风险，不是运行时崩溃类 P0。

## 落地执行顺序（P0/P1）
1. `P0-A` 风控 nil guard + queue Stop 幂等。
2. `P0-B` dispatcher Do/Stop 竞态修复。
3. `P1-A` funding settler 幂等键修复。
4. `P1-B` funding provider ctx 透传。
5. `P1-C` aggregator valid-source/freshness 修复。
6. `P1-D` trigger 多记录模型 + 本地取消路径。

## 回归验证命令
- `cd backend && go test ./internal/engine/risk -run 'CheckOrder|Nil' -count=1`
- `cd backend && go test ./internal/engine/engine -run 'Dispatcher|DoStop' -count=1`
- `cd backend && go test ./internal/engine/live/queue -run 'Stop|Enqueue' -count=1`
- `cd backend && go test ./internal/engine/funding -run 'Settler|Provider|Context|Cancel' -count=1`
- `cd backend && go test ./internal/engine/data -run 'Aggregator|Freshness|Required' -count=1`
- `cd backend && go test ./internal/engine/live/trigger -run 'Manager|Cancel|Multi' -count=1`
- `cd backend && go test -race ./internal/engine/... -count=1`

## Remediation v2 收件状态
- 已收到: `agent_01, agent_02, agent_03, agent_04, agent_05, agent_06, agent_07, agent_08, agent_11`
- 缺失: `agent_09, agent_10`
- 备注: 现有 `remediation_consensus_v2.md` 中把 `agent_05-remediation-v2.md` 标为缺失，需在下一版共识文件中修正。
