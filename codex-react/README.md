# Codex.app React 前端 — 逆向提取项目

> 从 Codex.app v260206.1448 (build 565) 的 app.asar 中提取并重组

## 架构

```
src/
├── main/                                 # Electron Main 进程
│   ├── main.js                           # 入口: IPC路由/窗口管理/应用生命周期
│   └── AppServerConnection.js            # 与 Rust codex 的 JSON-RPC 通信
├── preload/
│   └── preload.js                        # IPC 桥接 (electronBridge → window.message)
└── renderer/                             # React 前端 (81 文件)
    ├── App.js                            # 根组件, Provider 树 (6 Context)
    ├── bridge.js                         # Electron IPC 包装层
    ├── contexts.js                       # React Context 定义 (6个)
    ├── routes.js                         # 路由定义 (17 条路由)
    ├── utils.js                          # 工具函数 + BatchQueue
    ├── logger.js                         # 日志工具
    ├── core/                             # 核心状态管理
    │   ├── ConversationManager.js        # ⭐ 对话状态管理 (840+ 行)
    │   ├── MessageDispatcher.js          # 全局消息分发 (50+ 类型)
    │   └── ConversationModels.js         # 数据模型 JSDoc (11 类型)
    ├── hooks/                            # React Hooks (12 个)
    │   ├── useConversation.js            # 对话订阅 + 状态获取
    │   ├── useConversationManager.js     # CM 实例获取
    │   ├── useAppQuery.js                # 封装 MCP JSON-RPC 查询
    │   ├── useConfig.js                  # 配置读写
    │   ├── useAuth.js / useAnalytics.js  # 认证 / 埋点
    │   ├── useIntl.js                    # 国际化 + jotai 简化
    │   ├── useNavigation.js              # 导航辅助
    │   ├── useRemoteTasks.js             # 远程任务查询
    │   ├── useToast.js                   # Toast 通知
    │   ├── useWindowType.js              # 窗口类型判断
    │   └── useWorktree.js               # Worktree 管理
    ├── components/                       # 共享 UI 组件 (21 个)
    │   ├── Composer.js                   # 消息输入框 (文本+附件+拖拽)
    │   ├── Sidebar.js                    # 全局侧边栏 (导航+对话列表)
    │   ├── ConversationPanel.js          # 对话面板
    │   ├── Toolbar.js                    # 各页面工具栏
    │   ├── MarkdownRenderer.js           # Markdown 渲染
    │   ├── DiffRenderer.js               # Diff 渲染
    │   ├── StreamingIndicator.js         # 流式输出指示器
    │   ├── Button.js / Checkbox.js       # 基础组件
    │   └── ... (Icons, LoginViews, etc.)
    ├── pages/                            # 页面组件 (19 个)
    │   ├── ChatPage.js                   # ⭐ 主对话页 (消息渲染/审批)
    │   ├── HomePage.js                   # 首页 (新建+历史)
    │   ├── SettingsPage.js               # 设置页 (左导航+右内容)
    │   ├── InboxAndSkillsPage.js         # 收件箱 + 技能管理
    │   ├── SelectWorkspacePage.js        # Workspace 选择
    │   ├── WorktreeInitPage.js           # Worktree 创建
    │   └── ... (Login, Welcome, Debug, etc.)
    ├── pages/settings/                   # 设置子页面 (8 组 × 2 文件)
    │   ├── agent-settings.js + AgentSettings.js
    │   ├── mcp-settings.js + McpSettings.js
    │   ├── git-settings.js + GitSettings.js
    │   └── ... (personalization, worktrees, etc.)
    └── styles/
        └── index.css                     # CSS Token 系统 + 工具类 (320 行)
```

## 数据流

```
React 组件 → bridge.js → preload.js → main.js → AppServerConnection → Rust codex
     ↑                                                                      ↓
     └── ConversationManager ← MessageDispatcher ← window.message ← preload ←┘
```

## 说明

- **所有代码从 minified Vite bundle 逆向提取**, 已还原可读变量名
- 每个文件头部注明了在原始 bundle 中的行号范围和混淆名
- Renderer 层使用 ESM 模块系统, Main/Preload 层使用 CJS
