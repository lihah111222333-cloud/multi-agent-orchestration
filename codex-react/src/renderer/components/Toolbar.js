// =============================================================================
// Codex.app — Toolbar 工具栏组件集合
// 从 ChatPage, HomePage, SkillsPage, RemoteTaskPage, DiffViewerPage 推导
//
// 功能: 各页面顶部工具栏
// =============================================================================

/**
 * Toolbar — 通用工具栏容器
 *
 * @param {Object} props
 * @param {boolean} props.electron - 是否 Electron 模式 (添加可拖拽区域)
 * @param {React.ReactNode} props.children
 */
export function Toolbar({ electron = false, children }) {
    return (
        <div className={`toolbar h-toolbar flex items-center px-4 border-b border-token-border ${electron ? "draggable" : ""}`}>
            {children}
        </div>
    );
}

/**
 * ToolbarTitle — 工具栏标题
 */
export function ToolbarTitle({ children }) {
    return (
        <span className="toolbar-title text-sm font-medium text-token-foreground truncate">
            {children}
        </span>
    );
}

// ===================== 专用工具栏 =====================

/**
 * ChatToolbar — 对话页工具栏
 * 混淆名: (ChatPage 中引用)
 *
 * 显示: 对话标题 / 模型名 / 工作目录 / 操作按钮
 */
export function ChatToolbar({ conversationId, title, model, cwd }) {
    return (
        <Toolbar electron>
            <div className="flex items-center gap-3 flex-1 min-w-0">
                <ToolbarTitle>{title || "New Conversation"}</ToolbarTitle>
                {model && (
                    <span className="text-xs text-token-description-foreground bg-token-background-secondary px-2 py-0.5 rounded">
                        {model}
                    </span>
                )}
            </div>
            <div className="flex items-center gap-1 text-xs text-token-description-foreground">
                {cwd && <span className="truncate max-w-[200px] font-mono">{cwd}</span>}
            </div>
        </Toolbar>
    );
}

/**
 * HomeToolbar — 首页工具栏
 * 混淆名: (HomePage 中引用)
 */
export function HomeToolbar() {
    return (
        <Toolbar electron>
            <ToolbarTitle>Codex</ToolbarTitle>
        </Toolbar>
    );
}

/**
 * InboxHeader — 收件箱标题栏
 * 混淆名: (InboxPage 中引用)
 */
export function InboxHeader() {
    return (
        <Toolbar>
            <ToolbarTitle>Inbox</ToolbarTitle>
        </Toolbar>
    );
}

/**
 * SkillsToolbar — 技能页工具栏
 * 混淆名: (SkillsPage 中引用)
 */
export function SkillsToolbar() {
    return (
        <Toolbar electron>
            <ToolbarTitle>Skills</ToolbarTitle>
        </Toolbar>
    );
}

/**
 * SkillsFilterBar — 技能搜索/过滤栏
 * 混淆名: (SkillsPage 中引用)
 */
export function SkillsFilterBar() {
    return (
        <div className="skills-filter-bar px-4 py-2 border-b border-token-border">
            <input
                type="text"
                placeholder="Search skills..."
                className="w-full bg-token-background-secondary rounded-md px-3 py-1.5 text-sm text-token-foreground placeholder-token-input-placeholder-foreground outline-none"
            />
        </div>
    );
}

/**
 * RemoteTaskToolbar — 远程任务工具栏
 * 混淆名: (RemoteTaskPage 中引用)
 */
export function RemoteTaskToolbar({ taskId, title, status, onForkLocally }) {
    return (
        <Toolbar electron>
            <div className="flex items-center gap-3 flex-1 min-w-0">
                <ToolbarTitle>{title || "Remote Task"}</ToolbarTitle>
                <span className={`text-xs px-2 py-0.5 rounded
                    ${status === "running" ? "bg-yellow-100 text-yellow-700" :
                        status === "completed" ? "bg-green-100 text-green-700" :
                            "bg-red-100 text-red-700"}`}>
                    {status}
                </span>
            </div>
            {onForkLocally && (
                <button
                    onClick={onForkLocally}
                    className="text-xs text-token-primary hover:underline"
                >
                    Fork locally
                </button>
            )}
        </Toolbar>
    );
}

/**
 * DiffToolbar — Diff 查看器工具栏
 * 混淆名: (DiffViewerPage 中引用)
 */
export function DiffToolbar({ filePath }) {
    return (
        <Toolbar>
            <ToolbarTitle>{filePath}</ToolbarTitle>
        </Toolbar>
    );
}

/**
 * AnnouncementToolbar — 公告页工具栏
 * 混淆名: (AnnouncementPage 中引用)
 */
export function AnnouncementToolbar() {
    return (
        <Toolbar electron>
            <ToolbarTitle>Announcements</ToolbarTitle>
        </Toolbar>
    );
}

export default Toolbar;
