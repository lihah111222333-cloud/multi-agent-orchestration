# Unified Chat/CMD 设计文档

状态：已通过评审（用户逐节确认）

## 1. 目标

- 保留两个入口：`Chat` 与 `CMD`。
- 只维护一套核心页面与交互逻辑（DRY）。
- `Chat` 用于主 Agent 视角；`CMD` 用于子 Agent 管理与干预。
- 严格遵循桌面架构边界：Go/Wails3 负责系统与事件处理，Vue/JS 只做 UI。

## 2. 信息架构（已确认）

- `Chat` 与 `CMD` 两个导航入口保留不变。
- 两个入口都使用同一个核心页面实现（仅 `mode` 不同）。
- `Chat` 模式可设置主 Agent。
- `CMD` 模式自动隐藏主 Agent，仅展示子 Agent。
- 视图预设：
  - `Chat`：默认“对话优先”。
  - `CMD`：默认“混合视图（总览 + 最近活跃子 Agent 会话）”。

## 3. 主/子 Agent 规则（已确认）

- 主 Agent 判定：名称 + 开关并存，开关优先（C 方案）。
- 仅 `Chat` 页面允许激活/切换主 Agent。
- `CMD` 页面不做“主会话激活卡片”语义，只管理/干预子 Agent。
- `CMD` 右侧卡片区不显示主 Agent。

## 4. 布局与交互（已确认）

- 右侧固定宽度、可滚动卡片区（D 方案）。
- `CMD` 默认 `C` 视图，允许切换到 `A` 或 `B`。
- `CMD` 中点击子 Agent 卡片显示对应会话与输入框（可直接干预）。
- `CMD` 会话能力与 `Chat` 完全一致（A 方案）：消息、思考、diff、审批、附件等都保留。

## 5. 分栏比例策略（已确认）

- `Chat` 与 `CMD` 分栏比例独立持久化，不共享。
- `CMD` 内部子会话共享一套比例（避免逐个调整）。

## 6. 状态模型（已确认）

- 新增核心状态：
  - `mainAgentId`
  - `agentMetaById`（如 alias、isMain、lastActiveAt）
  - `viewPrefs.chat` / `viewPrefs.cmd`
- 派生集合：
  - `chatAgents = allThreads`
  - `cmdAgents = allThreads - mainAgent`
- 持久化：
  - `agent.main.id`
  - `agent.meta.v1`
  - `layout.chat.v1`
  - `layout.cmd.v1`

## 7. 事件与数据流（已确认）

- 全量事件统一进入同一 store 与时间线机制。
- 事件更新 `lastActiveAt`，用于 `CMD` 默认最近活跃子 Agent。
- 若当前 CMD 会话被改为主 Agent，则自动切换到最近活跃子 Agent。

## 8. 技术边界与性能约束（新增并确认）

- 禁止在前端使用浏览器原生 SSE 通道作为桌面主链路。
- 事件/状态同步统一通过 **Wails3 bridge + Go**。
- Vue/JS 仅负责 UI 渲染与交互状态，不承担系统能力或原生通道 fallback。
- bridge 不可用时只报错，不降级到 JS 原生系统能力。

## 9. 验收标准

- 两个入口仍存在，但核心实现只有一套。
- `Chat` 可定义主 Agent；`CMD` 自动隐藏主 Agent。
- `CMD` 默认混合视图，可切 A/B/C。
- `Chat`/`CMD` 分栏比例独立；`CMD` 子会话共享比例。
- 全部桌面事件同步路径走 Wails3/Go，前端无原生 SSE 直连主路径。
