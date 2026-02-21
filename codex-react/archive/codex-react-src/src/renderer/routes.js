// =============================================================================
// Codex.app — React 路由定义
// 提取自: index-formatted.js L247700-247787
// 组件名为 Vite 混淆后名称, 注释标注了实际功能
// =============================================================================

import { Route } from "react-router-dom";

// 页面组件映射 (混淆名 → 实际功能)
// CIt  = DebugPage
// lxn  = RootLayout (顶层布局, 包含侧边栏等)
// Axn  = LoginPage
// qxn  = WelcomePage
// Lxn  = SelectWorkspacePage
// gan  = DiffViewerPage
// nxn  = PlanSummaryPage
// Can  = FilePreviewPage
// cIt  = AuthGuardLayout (需要登录的布局)
// y7n  = HomePage (新建对话/选择历史)
// e_n  = FirstRunPage
// Kbn  = ChatPage (主对话页面) ⭐ 核心
// W_n  = ThreadOverlayPage (浮动窗口)
// Myn  = InboxPage (收件箱)
// L7e  = InboxItemPage
// q_n  = WorktreeInitPage
// n7t  = AnnouncementPage
// zwn  = RemoteTaskPage
// Gwn  = SettingsLayout
// Ywn  = SettingsDetailPage
// R_n  = SkillsPage
// Hwn  = OpenSourceLicensesPage

export const routes = (
    <>
        {/* 调试页 (独立, 不需要认证) */}
        <Route path="/debug" element={<DebugPage />} />

        {/* 根布局 */}
        <Route element={<RootLayout />}>

            {/* 无需认证的页面 */}
            <Route path="/login" element={<LoginPage />} />
            <Route path="/welcome" element={<WelcomePage />} />
            <Route path="/select-workspace" element={<SelectWorkspacePage />} />
            <Route path="/diff" element={<DiffViewerPage />} />
            <Route path="/plan-summary" element={<PlanSummaryPage />} />
            <Route path="/file-preview" element={<FilePreviewPage />} />

            {/* 需要认证的页面 (AuthGuardLayout) */}
            <Route element={<AuthGuardLayout />}>

                {/* 首页 */}
                <Route path="/" element={<HomePage />} />

                {/* 首次运行引导 */}
                <Route path="/first-run" element={<FirstRunPage />} />

                {/* ⭐ 主对话页 — AI 聊天界面 */}
                <Route path="/local/:conversationId" element={<ChatPage />} />

                {/* 浮动窗口对话 */}
                <Route path="/thread-overlay/:conversationId" element={<ThreadOverlayPage />} />

                {/* 收件箱 (含子路由: 任务列表/详情) */}
                <Route path="/inbox" element={<InboxPage />}>
                    <Route index element={<InboxItemPage />} />
                    <Route path=":itemId" element={<InboxItemPage />} />
                </Route>

                {/* Worktree 创建 */}
                <Route path="/worktree-init-v2/:pendingWorktreeId" element={<WorktreeInitPage />} />

                {/* 公告 */}
                <Route path="/announcement" element={<AnnouncementPage />} />

                {/* 远程任务详情 */}
                <Route path="/remote/:taskId" element={<RemoteTaskPage />} />

                {/* 设置 (含子路由) */}
                <Route path="/settings" element={<SettingsLayout />}>
                    <Route index element={<Navigate to={`/settings/${defaultSettingsSlug}`} replace />} />
                    {settingsSlugs.map(item =>
                        <Route key={item.slug} path={item.slug} element={<SettingsDetailPage slug={item.slug} />} />
                    )}
                    <Route path="open-source-licenses" element={<OpenSourceLicensesPage />} />
                    <Route path="*" element={<Navigate to={`/settings/${defaultSettingsSlug}`} replace />} />
                </Route>

                {/* 技能管理 */}
                <Route path="/skills" element={<SkillsPage />} />

            </Route>
        </Route>
    </>
);

// 设置页子路由 slugs (从 lazy-loaded chunks 提取):
// - agent-settings     → Agent 设置 (模型/审批策略)
// - mcp-settings       → MCP 服务器配置
// - git-settings       → Git 设置
// - personalization    → 个性化 (主题/字体)
// - local-environments → 本地环境管理
// - worktrees          → Worktree 管理
// - data-controls      → 数据控制
// - skills-settings    → 技能设置
