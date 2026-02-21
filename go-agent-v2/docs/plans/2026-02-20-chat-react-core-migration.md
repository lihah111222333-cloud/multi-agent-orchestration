# 2026-02-20 Chat React Core Migration Plan (Wails App)

## 结论 (先说清楚)

本次迁移的主范围是：**React 渲染层替换**。  
Go/Wails 现有能力已覆盖 Chat 核心业务流，不需要重写前端状态机。

## 目标

在保持桌面 App 架构（Go + Wails bridge）不变的前提下，把 `agent-terminal` Chat 核心页面从 Vue 迁到 React，并对齐 `codex-react` 的 Chat 视觉与交互体验。

## 硬约束

- 前端 UI-only，业务状态机由 Go 维护（SSOT）。
- 系统能力只通过 Wails bridge，不做浏览器 fallback。
- 迁移优先做行为等价，不做业务能力降级。
- 有状态逻辑优先下沉 Go；若后端实现成本过高或暂无接口，允许前端持有必要流程状态（不覆盖 Go 真值）。

## 放宽策略（本轮确认）

- `compact`/`approval` 这类流程，如当前必须前端编排，可先保留前端有状态实现。
- 前端持有的流程状态必须以 `ui/state/get` 和 bridge 事件为最终收敛依据。

## 已核对的 Go/Wails 能力 (2026-02-20)

### 1) Chat 主流程方法已具备

- 方法注册已包含：`thread/start`、`thread/list`、`thread/messages`、`thread/name/set`、`thread/resolve`、`thread/compact/start`、`turn/start`、`turn/interrupt`、`turn/forceComplete`、`ui/state/get`
- 依据：`go-agent-v2/internal/apiserver/methods.go`

### 2) `ui/state/get` 已给出 React Chat 渲染所需主数据

- 线程与状态：`threads`、`statuses`、`interruptibleByThread`
- 状态文案：`statusHeadersByThread`、`statusDetailsByThread`
- 时间线与改动：`timelinesByThread`、`diffTextByThread`
- 资源与指标：`tokenUsageByThread`、`activityStatsByThread`、`alertsByThread`
- 元信息：`agentMetaById`、`agentRuntimeById`
- 依据：`go-agent-v2/internal/apiserver/methods_ui_state.go`

### 3) 历史加载与流式补页已是后端能力

- `thread/messages` 首屏 hydrate 到 UI runtime，剩余页后台追加并通知 `thread/messages/page`
- 依据：`go-agent-v2/internal/apiserver/methods_thread.go`

### 4) 中断与强制完成已后端实现

- `turn/interrupt`：包含确认/无活动 turn/等待收敛等结果语义
- `turn/forceComplete`：best-effort interrupt + tracked turn 清理
- 依据：`go-agent-v2/internal/apiserver/methods_turn.go`

### 5) timeline/event/status/token 派生已在 Go `uistate`

- 事件归一化：`NormalizeEvent...`
- timeline item 构建：`user/assistant/thinking/command/file/tool/approval/plan/error`
- 状态派生：thread state + header/details + interruptible
- token usage 解析与百分比计算
- diff/activity/alerts 累积
- 依据：`go-agent-v2/internal/uistate/event_normalizer.go`、`go-agent-v2/internal/uistate/runtime_event_handlers.go`、`go-agent-v2/internal/uistate/runtime_timeline.go`、`go-agent-v2/internal/uistate/timeline_tokens.go`、`go-agent-v2/internal/uistate/runtime_clone.go`

### 6) Wails 事件桥接与 UI 状态变更节流已具备

- bridge 事件：`bridge-event` + 兼容 `agent-event`
- `ui/state/changed` 全局节流推送
- 依据：`go-agent-v2/cmd/agent-terminal/app.go`、`go-agent-v2/internal/apiserver/server_payload.go`

### 7) 通知映射覆盖 token/compact/turn/item 等关键事件

- `token_count -> thread/tokenUsage/updated`
- `context_compacted -> thread/compacted`
- `turn_* -> turn/*`
- item/tool/file/command 事件映射齐全
- 依据：`go-agent-v2/internal/apiserver/notifications.go`

## `codex-react` Chat 需求映射矩阵

| 需求（对齐 `codex-react`） | Go/Wails 现状 | React 前端职责 |
| --- | --- | --- |
| 会话列表/切换/命名 | 已有 `thread/list` / `thread/name/set` / `ui/state/get` | 渲染与交互编排 |
| 时间线流式渲染（assistant/thinking/command/file/tool/approval/plan/error） | `uistate` 已统一建模并持续更新 | 纯展示与样式对齐 |
| 历史消息加载 + 后台补页 | `thread/messages` + `thread/messages/page` 已实现 | 初次拉取触发、滚动体验 |
| 状态栏（header/details/interruptible/meta） | `ui/state/get` 已返回完整字段 | 文案展示与布局 |
| Composer：发送/附件/技能 | `turn/start` 输入结构与技能合并已在后端 | 输入控件、IME、本地临时态 |
| Esc 中断与强制结束 | `turn/interrupt` / `turn/forceComplete` 已具备结果语义 | 按键绑定与按钮状态反馈 |
| compact + token 使用率 | `thread/compact/start` + token usage 通知/快照已具备 | 触发按钮、文案格式化 |
| Code change / 活动 / 告警面板 | `diffTextByThread` + `activityStatsByThread` + `alertsByThread` 已有 | 面板 UI 组织与可读性优化 |
| 桥接事件机制 | `bridge-event` 与 `ui/state/changed` 节流链路已稳定 | 订阅与触发 `syncRuntimeState` |

## 缺口清单（按“必须/可选”区分）

### 必须补齐（P0）

- 当前未发现阻断 React Chat 迁移的后端能力缺口。  
  结论：可直接进入渲染层迁移。

### 可选优化（P1，非阻断）

- 命令组摘要目前在前端 `chat-core/timeline-compact.js` 做展示聚合，可评估是否下沉 Go 直接返回聚合字段（当前可继续前端实现）。
- `compact` 流程当前由前端编排 interrupt+compact，可先保留；后续若要进一步“前端无状态化”再收敛到单一后端语义。
- `approval` 交互若短期仍由前端承接，也视为可接受实现；后续再评估统一协议字段。
- 状态耗时目前前端可计算（UI 语义），若后续要多端统一可补充后端标准字段。

## 迁移范围（更新后）

### In scope

- `frontend/react-app` 实现 Chat 主页面（Timeline / Composer / Side panels）
- 复用 `style-lib` 统一视觉基线（与 `codex-react` 风格对齐）
- 复用 `chat-core` 中纯展示逻辑（逐步最小化）
- 彻底移除 Vue3 Chat 页面入口，不做双实现并存

### Out of scope

- 重写 Go 状态机或协议
- 将业务规则迁回前端
- 一次性迁移所有非 Chat 页面

## 实施阶段

1. Phase A（已进行）：抽离 `style-lib` / `chat-core`，Vue 先复用。
2. Phase B：实现 React Chat 壳层（Page + Timeline + Composer + Side panels），仅接现有 Go 合同。
3. Phase C：逐条对齐 `codex-react` 的交互细节（输入区、AI 流程视觉、改动面板信息架构）。
4. Phase D：切换前端入口到 React Chat，并删除 Vue3 Chat 入口/页面实现（不做灰度）。

## 验收标准

- 行为等价：Chat 核心流程由现有 Go 合同完整驱动，无前端业务回流。
- 视觉等价：Chat 页面与输入区达到 `codex-react` 风格与交互质量基线。
- 稳定性：高频输入、流式事件风暴、线程切换下不出现明显闪烁/错态。
- 验收方式：由你主测，发现问题后按缺陷单持续修复并复测，不设灰度回退要求。
