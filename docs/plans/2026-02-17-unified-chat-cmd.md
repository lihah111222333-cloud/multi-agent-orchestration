# Unified Chat/CMD 实现计划

> **给 Claude:** 必须使用 @执行计划 逐任务实现此计划。

**目标:** 保留 `Chat`/`CMD` 两个入口，同时合并为一套统一页面与状态逻辑，实现主/子 Agent 分工、模式切换与独立布局偏好。

**架构:** 引入 `UnifiedChatPage(mode)` 作为唯一核心页面，`Chat`/`CMD` 仅作为模式入口。线程筛选、主 Agent 规则、CMD 视图模式与分栏比例统一下沉到 `threads store + 纯函数视图模型`。事件流继续由 Go/Wails3 提供，前端只做 UI 状态消费与渲染。

**技术栈:** Vue3（ESM 无构建链）、Wails3 bridge（`/wails/runtime.js` + `Call.ByID` + `Events.On`）、Go app-server JSON-RPC、Node 内置 `node --test`（新增最小纯函数测试）。

---

### 任务 1: 建立线程视图模型与最小测试基线

**文件:**
- 创建: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/thread-view.model.js`
- 创建: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`

**步骤 1: 写失败的测试（主/子筛选、主Agent优先规则、CMD可见集）**

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { deriveChatAgents, deriveCmdAgents, resolveMainAgent } from '../thread-view.model.js';

test('cmd agents exclude main agent', () => {
  const threads = [{ id: 'a' }, { id: 'b' }];
  const main = resolveMainAgent({ mainAgentId: 'a', threads, meta: {} });
  assert.equal(main, 'a');
  assert.deepEqual(deriveCmdAgents({ threads, mainAgentId: main }).map(t => t.id), ['b']);
});
```

**步骤 2: 运行测试确认失败**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: FAIL（缺少导出函数或行为不匹配）

**步骤 3: 写最小实现**

```js
export function resolveMainAgent({ mainAgentId, threads, meta }) { /* 开关优先，名称兜底 */ }
export function deriveChatAgents({ threads }) { return threads; }
export function deriveCmdAgents({ threads, mainAgentId }) { return threads.filter(t => t.id !== mainAgentId); }
```

**步骤 4: 运行测试确认通过**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: PASS

**步骤 5: 提交**

```bash
git add go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/thread-view.model.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs
git commit -m "feat(ui): add thread view model and tests for chat/cmd split"
```

### 任务 2: 引入统一页面核心（UnifiedChatPage）

**文件:**
- 创建: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/UnifiedChatPage.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/ChatPage.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/TerminalPage.js`

**步骤 1: 写失败测试（模式映射与默认视图规则）**

在 `thread-view.model.test.mjs` 增加断言：

```js
test('default layouts by mode', () => {
  assert.equal(defaultLayoutForMode('chat'), 'chat_focus');
  assert.equal(defaultLayoutForMode('cmd'), 'cmd_mix');
});
```

**步骤 2: 运行测试确认失败**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: FAIL（`defaultLayoutForMode` 未实现）

**步骤 3: 最小实现 + 页面骨架**

- `UnifiedChatPage` 接收 `mode='chat'|'cmd'`
- `ChatPage`/`TerminalPage` 变为薄包装：

```js
template: `<UnifiedChatPage mode="chat" :project-store="projectStore" :thread-store="threadStore" />`
```

```js
template: `<UnifiedChatPage mode="cmd" :project-store="projectStore" :thread-store="threadStore" />`
```

**步骤 4: 运行测试确认通过**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: PASS

**步骤 5: 提交**

```bash
git add go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/UnifiedChatPage.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/ChatPage.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/TerminalPage.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/thread-view.model.js
git commit -m "refactor(ui): route chat/cmd through unified page"
```

### 任务 3: 扩展 Store（主Agent、模式偏好、比例持久化）

**文件:**
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/threads.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/thread-view.model.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`

**步骤 1: 写失败测试（主Agent切换、CMD自动过滤、比例独立）**

```js
test('chat/cmd split ratio stored separately', () => {
  const prefs = { chat: { splitRatio: 0.7 }, cmd: { splitRatio: 0.42 } };
  assert.equal(prefs.chat.splitRatio, 0.7);
  assert.equal(prefs.cmd.splitRatio, 0.42);
});
```

**步骤 2: 运行测试确认失败**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: FAIL（对应函数/常量缺失）

**步骤 3: 写最小实现**

- `threads.js` 新增：
  - `mainAgentId`
  - `agentMetaById`
  - `viewPrefs.chat` / `viewPrefs.cmd`
  - `setMainAgent()`, `setLayoutMode()`, `setSplitRatio()`
- 本地持久化键：
  - `agent.main.id`
  - `agent.meta.v1`
  - `layout.chat.v1`
  - `layout.cmd.v1`

**步骤 4: 运行测试确认通过**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: PASS

**步骤 5: 提交**

```bash
git add go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/threads.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/thread-view.model.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs
git commit -m "feat(store): add main-agent and mode-specific layout preferences"
```

### 任务 4: 实现 CMD 三视图与右侧卡片区

**文件:**
- 创建: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/AgentCardRail.js`
- 创建: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/CmdOverviewPanel.js`
- 创建: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/LayoutSwitcher.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/UnifiedChatPage.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/ChatTimeline.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/ComposerBar.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/styles.css`

**步骤 1: 写失败测试（CMD 默认 C 模式 + 可切 A/B/C）**

在 `thread-view.model.test.mjs` 增加：

```js
test('cmd defaults to mixed layout and can switch', () => {
  assert.equal(normalizeCmdLayout(undefined), 'mix');
  assert.equal(normalizeCmdLayout('overview'), 'overview');
  assert.equal(normalizeCmdLayout('chat'), 'chat');
});
```

**步骤 2: 运行测试确认失败**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: FAIL

**步骤 3: 写最小实现**

- 右侧固定宽度卡片区（可滚动）
- CMD 默认 `mix`（总览 + 最近活跃子Agent会话）
- CMD 可切 `overview` / `chat` / `mix`
- 点击卡片切换当前子Agent会话，显示完整聊天能力（与 Chat 对齐）

**步骤 4: 运行测试确认通过**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: PASS

**步骤 5: 手动验证**

运行: `go run ./cmd/agent-terminal/main.go`  
预期:
- `Chat` 可设主 Agent
- `CMD` 不显示主 Agent
- `CMD` 三模式切换可用
- 右侧卡片区固定宽度、可滚动

**步骤 6: 提交**

```bash
git add go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/AgentCardRail.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/CmdOverviewPanel.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/LayoutSwitcher.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/UnifiedChatPage.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/ChatTimeline.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/components/ComposerBar.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/styles.css \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/thread-view.model.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs
git commit -m "feat(ui): add cmd mixed view and right-side agent rail"
```

### 任务 5: 强化 Wails3 边界并清理冗余终端逻辑

**文件:**
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/services/api.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/app.js`
- 修改: `go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/TerminalPage.js`
- 修改: `go-agent-v2/cmd/agent-terminal/AGENTS.md`

**步骤 1: 写失败测试（API 层不允许原生 SSE 直连）**

在 `thread-view.model.test.mjs` 增加纯函数约束断言（例如 API 配置标志）：

```js
test('desktop events must use wails bridge', () => {
  assert.equal(EVENT_CHANNEL, 'wails');
});
```

**步骤 2: 运行测试确认失败**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: FAIL

**步骤 3: 写最小实现**

- `api.js` 明确 bridge-only 事件订阅路径
- 文档注释明确：禁止前端原生 SSE 作为桌面主链路
- `TerminalPage` 仅保留 CMD 入口壳，不再保留独立业务实现

**步骤 4: 运行测试与手动验证**

运行: `node --test go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs`  
预期: PASS  

运行: `go run ./cmd/agent-terminal/main.go`  
预期: `Chat/CMD` 正常收事件，行为一致，无前端原生 SSE 回退

**步骤 5: 提交**

```bash
git add go-agent-v2/cmd/agent-terminal/frontend/vue-app/services/api.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/app.js \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/pages/TerminalPage.js \
  go-agent-v2/cmd/agent-terminal/AGENTS.md \
  go-agent-v2/cmd/agent-terminal/frontend/vue-app/stores/__tests__/thread-view.model.test.mjs
git commit -m "chore(architecture): enforce wails bridge-only event flow"
```

### 任务 6: 最终回归与交付检查

**文件:**
- 修改: `docs/plans/2026-02-17-unified-chat-cmd-design.md`（如实现细节有偏差，回填）
- 修改: `docs/plans/2026-02-17-unified-chat-cmd.md`（标注执行结果）

**步骤 1: 回归命令**

运行: `go test ./cmd/agent-terminal/... ./internal/apiserver/...`  
预期: PASS（或 `[no test files]` + 无失败）

**步骤 2: 手动验收清单**

- `Chat` 可设主 Agent
- `CMD` 自动隐藏主 Agent
- `CMD` 默认 `mix`，可切 `overview` / `chat`
- `Chat` 与 `CMD` 比例独立持久化
- `CMD` 子会话共享比例
- Wails3 bridge-only 事件链路满足约束

**步骤 3: 交付提交**

```bash
git add docs/plans/2026-02-17-unified-chat-cmd-design.md docs/plans/2026-02-17-unified-chat-cmd.md
git commit -m "docs(plan): finalize unified chat/cmd implementation and validation checklist"
```
