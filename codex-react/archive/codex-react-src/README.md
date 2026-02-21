# Codex.app React 前端 — 逆向提取项目

> 从 Codex.app v260206.1448 (build 565) 的 app.asar 中提取并重组

## 项目结构

```
codex-react-src/
├── package.json                          # 原始依赖声明
├── public/
│   └── index.html                        # SPA 入口
├── src/
│   ├── main/                             # Electron Main 进程
│   │   └── AppServerConnection.js        # 与 Rust codex 通信核心
│   ├── preload/
│   │   └── preload.js                    # IPC 桥接层
│   └── renderer/                         # React 前端
│       ├── routes.js                     # 路由定义
│       ├── core/
│       │   ├── ConversationManager.js    # 对话状态管理 (核心)
│       │   ├── MessageDispatcher.js      # 全局消息分发
│       │   └── ConversationModels.js     # 数据模型定义
│       ├── pages/
│       │   ├── HomePage.js               # 首页
│       │   ├── ChatPage.js               # 主对话页面
│       │   ├── InboxPage.js              # 收件箱
│       │   ├── SettingsPage.js           # 设置页
│       │   └── SkillsPage.js             # 技能页
│       └── hooks/
│           └── useConversation.js        # 对话 Hook
└── full-bundles/                         # 完整 bundle (参考用)
    ├── index-formatted.js                # 格式化的 React bundle (254K行)
    └── main-bundle.js                    # Electron main 进程 bundle
```

## 架构说明

- **所有代码从 minified Vite bundle 逆向提取**, 变量名为混淆后名称
- 每个文件头部注明了在原始 bundle 中的行号范围
- `full-bundles/` 包含完整格式化 bundle 供交叉参考
