# 前端架构文档

## 1. 技术栈

| 分类 | 选型 | 说明 |
| :--- | :--- | :--- |
| 框架 | Vue 3 (ESM 浏览器版) | 直接引入 `vue.esm-browser.prod.js`，无编译时框架依赖 |
| 构建 | Vite 6 | 开发服务器 + 生产构建，输出到 `dist/` |
| 桌面集成 | Wails v3 Runtime | Go ↔ JS 双向桥接 |
| 图表 | ECharts | 运行时按需加载 |
| E2E 测试 | Playwright | Chromium 自动化测试 |
| 样式 | 原生 CSS (113KB + 32KB) | `styles.css` + `codex-components.css` |

> **特点**: 零 npm 运行时依赖 (`dependencies: {}`)，仅 Vite 和 Playwright 为开发依赖。Vue 通过 ESM 直接引入，无需 vue-loader 或 SFC 编译。

---

## 2. 目录结构

```
cmd/agent-terminal/frontend/
├── index.html                 # 入口 HTML (挂载 #app)
├── package.json               # 零运行时依赖
├── vite.config.js             # Vite 配置 (Wails 外部化)
├── lib/
│   └── vue.esm-browser.prod.js  # Vue 3 ESM
├── vue-app/
│   ├── main.js                # 应用引导 (createApp → mount)
│   ├── app.js                 # AppRoot 组件 (路由/页面切换)
│   ├── styles.css             # 全局样式 (113KB)
│   ├── codex-components.css   # 组件样式 (32KB)
│   ├── components/            # 可复用组件 (9 个)
│   ├── pages/                 # 页面组件 (6 个)
│   ├── services/              # 服务层 (5 个)
│   ├── stores/                # 状态管理 (5 个)
│   └── utils/                 # 工具函数
├── assets/                    # 静态资源
└── wails/                     # Wails 自动生成的 Go 绑定
```

---

## 3. 页面路由

使用 Vue 条件渲染实现 SPA 路由 (无 vue-router)：

| 页面 | 组件 | 文件大小 | 说明 |
| :--- | :--- | :--- | :--- |
| **chat** | `UnifiedChatPage` | 110KB | 主对话界面 (核心页面) |
| **skills** | `SkillsPage` | 29KB | 技能浏览/匹配/详情 |
| **settings** | `SettingsPage` | 18KB | 模型/配置/构建信息 |
| **commands** | `CommandsPage` | 4KB | 命令卡管理 |
| **agents** / **memory** | `DataPage` | 2KB | 通用数据表格 |
| **tasks** | `TasksPage` | 2KB | 任务列表 |

导航通过 `SidebarNav` 组件控制：`chat → agents → tasks → skills → commands → memory → settings`

---

## 4. 组件清单

| 组件 | 文件大小 | 职责 |
| :--- | :--- | :--- |
| `ChatTimeline` | 43KB | 对话时间线渲染 (消息/推理/命令/diff) |
| `ComposerBar` | 20KB | 输入区 (文本/图片/附件/发送) |
| `DiffPanel` | 18KB | 代码 diff 可视化 |
| `JsonRenderWidgets` | 20KB | JSON 数据渲染组件集 |
| `ActivityPanel` | 14KB | Agent 活动面板 |
| `ProjectModal` | 2KB | 项目选择弹窗 |
| `ProjectSelect` | 2KB | 项目下拉选择器 |
| `JsonRenderer` | 1KB | JSON 渲染入口 |
| `SidebarNav` | 1KB | 侧边导航栏 |

---

## 5. 状态管理

使用 Vue 3 `reactive()` 手动管理全局状态 (无 Pinia/Vuex)：

| Store | 文件大小 | 职责 |
| :--- | :--- | :--- |
| `threads.js` | 49KB | **核心 Store** — 线程列表/状态/时间线/Token/偏好/布局 |
| `composer.js` | 7KB | 输入框状态 (文本/附件/图片/历史) |
| `projects.js` | 5KB | 项目列表/选中项/工作目录 |
| `thread-view.model.js` | 1KB | 线程视图模型 |
| `thread-state-whitelist.js` | 2KB | 状态字段白名单 (防止非法字段注入) |

### threads.js 状态分层

```
state (UI 本地)          ← 页面/主题/活跃线程 等持久偏好
  ↕ 双向同步 (persistPreferenceAndSync)
runtimeRootState (运行时) ← threads/statuses/timelines/tokens 等后端推送
```

---

## 6. 服务层

| 服务 | 文件大小 | 职责 |
| :--- | :--- | :--- |
| `api.js` | 12KB | **Wails 桥接层** — `callAPI()`/`onAgentEvent()`/`onBridgeEvent()` |
| `log.js` | 4KB | 前端日志 (采样/节流) |
| `json-render-engine.js` | 4KB | JSON → 可视化渲染引擎 |
| `diff.js` | 3KB | Diff 文本解析 |
| `status.js` | 0.4KB | Agent 状态常量 |

---

## 7. Wails 桥接架构

前端 **禁止** 直接访问系统能力，所有系统操作通过 Wails Go 桥接：

```
┌─────────────────────────────────────────────┐
│              Vue 前端 (WebView)              │
│                                             │
│  api.js:                                     │
│    callAPI(method, params)                   │
│    → window.go.main.App.CallAPI(method, ...) │
│                                             │
│    onAgentEvent(callback)                    │
│    → Wails Events.On("agent-event", ...)     │
│                                             │
│    selectProjectDir()                        │
│    → window.go.main.App.SelectProjectDir()   │
├─────────────────────────────────────────────┤
│              Wails v3 Bridge                 │
├─────────────────────────────────────────────┤
│         Go (app.go / main.go)               │
│                                             │
│  App.CallAPI(method, params)                │
│    → apiserver.Server 方法分发               │
│    → 返回结果给前端                           │
│                                             │
│  App.handleBridgeNotification(method, p)    │
│    → Wails Events.Emit("bridge-event", p)    │
│    → Vue onBridgeEvent 接收                  │
└─────────────────────────────────────────────┘
```

### 桥接方法

| Go 方法 | JS 调用 | 说明 |
| :--- | :--- | :--- |
| `App.CallAPI()` | `callAPI(method, params)` | 通用 RPC 桥 (覆盖全部 45+ 后端方法) |
| `App.SelectProjectDir()` | `selectProjectDir()` | 原生目录选择对话框 |
| `App.SelectProjectDirs()` | `selectProjectDirs()` | 原生多选目录 |
| `App.SelectFiles()` | `selectFiles()` | 原生文件选择 |
| `App.SaveClipboardImage()` | `saveClipboardImage(base64)` | 剪贴板图片保存 |
| `App.CopyText()` | `copyTextToClipboard(text)` | 系统剪贴板写入 |

### 事件通道

| 通道 | 方向 | 说明 |
| :--- | :--- | :--- |
| `agent-event` | Go → JS | Agent 事件 (来自 CLI 子进程) |
| `bridge-event` | Go → JS | apiserver 通知 (UI 状态/审批等) |
| `app-will-quit` | Go → JS | 应用即将退出 |
| `files-dropped` | Wails → JS | 文件拖放 |

---

## 8. 工具函数

| 模块 | 说明 |
| :--- | :--- |
| `assistant-markdown.js` (33KB) | Markdown 渲染 (代码高亮/Mermaid/LaTeX/表格/diff) |

---

## 9. 开发与测试

### 本地开发

```bash
cd cmd/agent-terminal/frontend
npm install        # 首次安装 (仅 devDependencies)
npm run dev        # Vite 开发服务器 (localhost:5173)
npm run build      # 生产构建 → dist/
```

### E2E 测试 (Playwright)

```bash
npm run test:e2e:install  # 安装 Chromium
npm run test:e2e          # 无头运行
npm run test:e2e:headed   # 有头运行
npm run test:e2e:ui       # Playwright UI
```

### 生产构建

Vite 构建输出到 `dist/`，Go 通过 `embed.FS` 打包进二进制文件 → 单文件分发。
