# Codex.app 原始文件映射

> 提取自: `/Users/mima0000/Downloads/Codex.app` → `app.asar` 解包

## 原始 Bundle 文件

| 原始文件 (Codex.app 内) | 大小 | 说明 |
|---|---|---|
| `Contents/Resources/app.asar` | — | Electron 应用包 (解包后即为下方文件) |
| `.vite/build/main.js` | 120B | Main 进程入口 (仅 require main-B6C8fi5S.js) |
| `.vite/build/main-B6C8fi5S.js` | 1.5MB | **Main 进程主 bundle** |
| `.vite/build/preload.js` | 1.5KB | **Preload 桥接脚本** |
| `.vite/build/worker.js` | — | Worker 入口 |
| `webview/index.html` | 1KB | React SPA 入口 HTML |
| `webview/assets/index-3Lu2GYf3.js` | 6.4MB | **React 前端主 bundle** (254K行格式化) |
| `webview/assets/index-BVu5GRFr.css` | — | 样式表 |
| `Contents/Resources/codex` | 55MB | **Rust codex binary** (app-server 后端) |

## 提取文件 → 原始位置映射

### Main 进程 (`src/main/`)

| 提取文件 | 原始文件 | 行号范围 | 原始类/函数名 |
|---|---|---|---|
| [main.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/main/main.js) | `main-B6C8fi5S.js` | 全局入口段 (文件末尾 ~1000行) | `ape` (HostContextManager), `Xpe` (WindowManager), `Vt`, `Nc` |
| [AppServerConnection.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/main/AppServerConnection.js) | `main-B6C8fi5S.js` | L532 (class `rue`) | `rue`, `Pce` (StdioTransport), `jce` (WebSocketTransport) |

### Preload (`src/preload/`)

| 提取文件 | 原始文件 | 行号范围 | 说明 |
|---|---|---|---|
| [preload.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/preload/preload.js) | `preload.js` | 全部 (3行 minified) | 1:1 反混淆 |

### React 前端 (`src/renderer/`)

| 提取文件 | 原始文件 | 行号范围 | 原始类/函数名 |
|---|---|---|---|
| [routes.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/renderer/routes.js) | `index-formatted.js` | L247700-247787 | `CVe`, `n1` (Route) |
| [ConversationManager.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/renderer/core/ConversationManager.js) | `index-formatted.js` | L35700-37500 | 匿名 class (含 `startConversation`, `startTurn`, `onNotification`, `onRequest`) |
| [MessageDispatcher.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/renderer/core/MessageDispatcher.js) | `index-formatted.js` | L251400-251715 | `window.addEventListener("message")` 回调 |
| [ConversationModels.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/renderer/core/ConversationModels.js) | `index-formatted.js` | 多处推导 | 从 `setConversation()` / `upsertItem()` 等调用推导 |
| [ChatPage.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/renderer/pages/ChatPage.js) | `index-formatted.js` | `Kbn` (L~200000+), `rO`, `b9` | 路由 `/local/:conversationId` |
| [HomePage.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/renderer/pages/HomePage.js) | `index-formatted.js` | `y7n` (L247727) | 路由 `/` |
| [SettingsPage.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/renderer/pages/SettingsPage.js) | `index-formatted.js` | `Gwn` + `Ywn` (L247758) | 路由 `/settings` + lazy chunks |
| [InboxAndSkillsPage.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/renderer/pages/InboxAndSkillsPage.js) | `index-formatted.js` | `Myn`/`L7e` (L247739), `R_n` (L247782) | 路由 `/inbox`, `/skills` |
| [useConversation.js](file:///Users/mima0000/.gemini/antigravity/playground/nodal-quasar/codex-react-src/src/renderer/hooks/useConversation.js) | `index-formatted.js` | 多处引用 | `useConversation`, `useCurrentTurn` 等 |

## 页面组件 → 混淆名映射

| 页面 | 路由 | 混淆名 | 格式化 bundle 行号 |
|---|---|---|---|
| DebugPage | `/debug` | `CIt` | L247702 |
| RootLayout | (layout) | `lxn` | L247705 |
| LoginPage | `/login` | `Axn` | L247707 |
| WelcomePage | `/welcome` | `qxn` | L247710 |
| SelectWorkspacePage | `/select-workspace` | `Lxn` | L247713 |
| DiffViewerPage | `/diff` | `gan` | L247716 |
| PlanSummaryPage | `/plan-summary` | `nxn` | L247719 |
| FilePreviewPage | `/file-preview` | `Can` | L247722 |
| AuthGuardLayout | (layout) | `cIt` | L247725 |
| **HomePage** | `/` | `y7n` | L247727 |
| FirstRunPage | `/first-run` | `e_n` | L247730 |
| **ChatPage** | `/local/:id` | `Kbn` | L247733 |
| ThreadOverlay | `/thread-overlay/:id` | `W_n` | L247736 |
| **InboxPage** | `/inbox` | `Myn` | L247739 |
| WorktreeInitPage | `/worktree-init-v2/:id` | `q_n` | L247749 |
| AnnouncementPage | `/announcement` | `n7t` | L247752 |
| RemoteTaskPage | `/remote/:id` | `zwn` | L247755 |
| **SettingsLayout** | `/settings` | `Gwn` | L247758 |
| **SkillsPage** | `/skills` | `R_n` | L247782 |

## 设置子页面 → Lazy Chunk

| 设置 slug | Chunk 文件名 |
|---|---|
| agent-settings | `agent-settings-*.js` |
| mcp-settings | `mcp-settings-*.js` |
| git-settings | `git-settings-*.js` |
| personalization | `personalization-settings-*.js` |
| local-environments | `local-environments-settings-page-*.js` |
| worktrees | `worktrees-settings-page-*.js` |
| skills-settings | `skills-settings-*.js` |
| data-controls | `data-controls-*.js` |

## 格式化 bundle 参考

完整格式化后的 React bundle 在:
`codex-app-extracted/webview/assets/index-formatted.js` (254,330 行)

可通过行号直接在原始 bundle 中查阅任何提取代码的上下文。
