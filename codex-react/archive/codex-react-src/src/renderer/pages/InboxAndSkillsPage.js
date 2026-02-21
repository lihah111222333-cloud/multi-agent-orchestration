// =============================================================================
// Codex.app — 收件箱页面 (InboxPage) + 技能页面 (SkillsPage)
// =============================================================================

// ======================== InboxPage ========================
// 混淆名: Myn (Layout) + L7e (Item)
// 路由: /inbox, /inbox/:itemId
//
// 功能: 显示自动化任务结果列表, 含:
//   - 自动化任务运行状态
//   - 创建新自动化 (automationMode=create)
//   - 任务详情查看

function InboxPage() {
    // 查询收件箱列表
    // const { data: items } = useQuery("inbox-items", { limit: 200 });
    // const { data: pendingRuns } = useQuery("pending-automation-runs");
    // const { data: automations } = useQuery("list-automations");

    return (
        <div className="inbox-page h-full flex">
            {/* 左侧列表 */}
            <aside className="inbox-sidebar w-80 border-r overflow-y-auto">
                <InboxHeader />
                {/* 待处理的自动化运行 */}
                <PendingRunsList />
                {/* 历史任务列表 */}
                <InboxItemsList />
            </aside>

            {/* 右侧详情 (Outlet 渲染子路由) */}
            <main className="inbox-content flex-1">
                <Outlet />
            </main>
        </div>
    );
}

function InboxItemDetail() {
    // 根据 :itemId 显示任务详情
    // 含: 用户消息, AI 回复, 代码变更, 操作按钮
    const { itemId } = useParams();
    return (
        <div className="inbox-item-detail p-4">
            {/* 任务摘要 */}
            {/* 用户/AI 消息预览 */}
            {/* 操作: 打开/归档/继续 */}
        </div>
    );
}

// ======================== SkillsPage ========================
// 混淆名: R_n
// 路由: /skills
//
// 功能: 技能 (Skills) 管理页面
//   - 浏览可用技能
//   - 安装/卸载技能
//   - 技能搜索和过滤
//   - 按 scope 分组 (global/workspace)

function SkillsPage() {
    // const { data: skills } = useQuery("skills/list");
    // const installedSkillIds = new Set(...);

    return (
        <div className="skills-page h-full flex flex-col">
            <SkillsToolbar />

            {/* 搜索和过滤 */}
            <SkillsFilterBar />

            {/* 技能网格 */}
            <div className="skills-grid grid gap-4 p-4">
                {/* skills.map(skill => <SkillCard skill={skill} />) */}
            </div>
        </div>
    );
}

function SkillCard({ skill, canInstall, isInstalled, isInstalling, onInstall }) {
    return (
        <div className="skill-card border rounded-lg p-4">
            <h3>{skill.name}</h3>
            <p className="text-sm text-muted">{skill.description}</p>
            <div className="flex gap-2 mt-2">
                {isInstalled ? (
                    <span className="badge">Installed</span>
                ) : (
                    <button onClick={() => onInstall(skill.id)} disabled={isInstalling}>
                        {isInstalling ? "Installing..." : "Install"}
                    </button>
                )}
            </div>
        </div>
    );
}

module.exports = { InboxPage, InboxItemDetail, SkillsPage, SkillCard };
