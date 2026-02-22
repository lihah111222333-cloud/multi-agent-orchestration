# E2E 测试约定

- 优先使用 `data-testid` 定位，避免依赖图标和文案细节。
- 新增可测交互时，请同步补充稳定的 `data-testid`。
- 本目录用例默认依赖 `playwright.config.js` 自动启动本地 Vite 服务。

当前已使用的核心 test id：

- `app-shell`
- `page-chat` / `page-settings`
- `nav-chat` / `nav-settings`
- `launch-agent-button`
- `thread-archive-toggle`
- `thread-empty-state`
- `chat-empty-state`
- `composer-input`
- `composer-attach-button`
- `composer-compact-button`
- `composer-send-button`

页面级钩子（用于全页面巡检）：

- `data-page-agents`
- `data-page-dags`
- `tasks-page`
- `skills-page`
- `commands-page`
- `data-page-memory`
- `settings-page`

真实调用按钮钩子（当前覆盖设置页）：

- `settings-lsp-save-button`
