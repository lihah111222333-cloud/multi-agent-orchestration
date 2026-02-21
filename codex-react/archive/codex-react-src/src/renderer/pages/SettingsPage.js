// =============================================================================
// Codex.app — 设置页 (SettingsPage)
// 混淆名: Gwn (Layout) + Ywn (Detail)
// 路由: /settings/:slug
//
// 设置页采用左侧导航 + 右侧内容的布局
// 每个设置子页面是独立的 lazy-loaded chunk
// =============================================================================

// 设置类别列表 (从 settingsSlugs / WA 数组提取)
const SETTINGS_CATEGORIES = [
    {
        slug: "agent-settings",
        label: "Agent",
        description: "Model, approval policy, sandbox settings",
        // 加载: agent-settings-*.js chunk
    },
    {
        slug: "mcp-settings",
        label: "MCP Servers",
        description: "MCP server configuration and management",
        // 加载: mcp-settings-*.js chunk
    },
    {
        slug: "git-settings",
        label: "Git",
        description: "Git integration settings",
        // 加载: git-settings-*.js chunk
    },
    {
        slug: "personalization",
        label: "Personalization",
        description: "Theme, font, cursor style",
        // 加载: personalization-settings-*.js chunk
    },
    {
        slug: "local-environments",
        label: "Local Environments",
        description: "Local environment management",
        // 加载: local-environments-settings-page-*.js chunk
    },
    {
        slug: "worktrees",
        label: "Worktrees",
        description: "Git worktree management",
        // 加载: worktrees-settings-page-*.js chunk
    },
    {
        slug: "skills-settings",
        label: "Skills",
        description: "Skills configuration",
        // 加载: skills-settings-*.js chunk
    },
    {
        slug: "data-controls",
        label: "Data Controls",
        description: "Data sharing and privacy settings",
        // 加载: data-controls-*.js chunk
    },
];

/**
 * SettingsLayout — 设置页布局
 */
function SettingsLayout() {
    const navigate = useNavigate();
    const { slug } = useParams();

    return (
        <div className="settings-layout flex h-full">
            {/* 左侧导航 */}
            <nav className="settings-sidebar w-60 border-r">
                {SETTINGS_CATEGORIES.map((cat) => (
                    <button
                        key={cat.slug}
                        className={`settings-nav-item ${slug === cat.slug ? "active" : ""}`}
                        onClick={() => navigate(`/settings/${cat.slug}`)}
                    >
                        {cat.label}
                    </button>
                ))}
                <button onClick={() => navigate("/settings/open-source-licenses")}>
                    Open Source Licenses
                </button>
            </nav>

            {/* 右侧内容 (Outlet 渲染子路由) */}
            <div className="settings-content flex-1 overflow-y-auto p-6">
                <Outlet />
            </div>
        </div>
    );
}

/**
 * SettingsDetailPage — 设置详情页 (根据 slug 动态加载)
 */
function SettingsDetailPage({ slug }) {
    // React.lazy 动态加载对应 chunk
    const Component = React.lazy(() => import(`./settings/${slug}`));
    return (
        <React.Suspense fallback={<LoadingSpinner />}>
            <Component />
        </React.Suspense>
    );
}

module.exports = { SettingsLayout, SettingsDetailPage, SETTINGS_CATEGORIES };
