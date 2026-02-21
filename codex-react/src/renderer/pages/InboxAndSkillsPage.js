// =============================================================================
// Codex.app — 收件箱页面 (InboxPage) + 技能页面 (SkillsPage)
// =============================================================================

import { useParams, Outlet } from "react-router-dom";
import { useQuery } from "../hooks/useAppQuery";

// ======================== InboxPage ========================
// 混淆名: Myn (Layout) + L7e (Item)
// 路由: /inbox, /inbox/:itemId
//
// 功能: 显示自动化任务结果列表, 含:
//   - 自动化任务运行状态
//   - 创建新自动化 (automationMode=create)
//   - 任务详情查看

export function InboxPage() {
    // 查询收件箱列表
    const { data: items } = useQuery("inbox-items", {
        placeholderData: [],
    });
    const { data: pendingRuns } = useQuery("pending-automation-runs", {
        placeholderData: [],
    });

    return (
        <div className="inbox-page h-full flex">
            {/* 左侧列表 */}
            <aside className="inbox-sidebar w-80 border-r border-token-border overflow-y-auto">
                <InboxHeader />
                {/* 待处理的自动化运行 */}
                <PendingRunsList runs={pendingRuns} />
                {/* 历史任务列表 */}
                <InboxItemsList items={items} />
            </aside>

            {/* 右侧详情 (Outlet 渲染子路由) */}
            <main className="inbox-content flex-1">
                <Outlet />
            </main>
        </div>
    );
}

function InboxHeader() {
    return (
        <div className="flex items-center justify-between p-4 border-b border-token-border">
            <h2 className="text-lg font-semibold text-token-foreground">Inbox</h2>
        </div>
    );
}

function PendingRunsList({ runs = [] }) {
    if (runs.length === 0) return null;
    return (
        <div className="px-3 py-2">
            <span className="text-xs font-medium text-token-description-foreground uppercase">Pending</span>
            {runs.map((run, i) => (
                <div key={run.id ?? i} className="px-2 py-1.5 text-sm text-token-foreground rounded hover:bg-token-foreground/5">
                    {run.title ?? "Running..."}
                </div>
            ))}
        </div>
    );
}

function InboxItemsList({ items = [] }) {
    if (items.length === 0) {
        return (
            <div className="px-3 py-8 text-center text-xs text-token-description-foreground">
                No inbox items yet
            </div>
        );
    }
    return (
        <div className="px-3 py-2">
            {items.map((item, i) => (
                <div key={item.id ?? i} className="px-2 py-1.5 text-sm text-token-foreground rounded hover:bg-token-foreground/5 truncate">
                    {item.title ?? "Untitled task"}
                </div>
            ))}
        </div>
    );
}

export function InboxItemDetail() {
    // 根据 :itemId 显示任务详情
    // 含: 用户消息, AI 回复, 代码变更, 操作按钮
    const { itemId } = useParams();

    const { data: itemDetail, isLoading } = useQuery("inbox-items/get", {
        params: { itemId },
        queryConfig: { enabled: !!itemId },
    });

    if (!itemId) {
        return (
            <div className="inbox-item-detail p-4 flex items-center justify-center h-full">
                <div className="text-sm text-token-description-foreground text-center py-8">
                    Select an item to view details
                </div>
            </div>
        );
    }

    if (isLoading) {
        return (
            <div className="inbox-item-detail p-4 flex items-center justify-center h-full">
                <div className="text-sm text-token-description-foreground">Loading...</div>
            </div>
        );
    }

    const item = itemDetail ?? {};

    return (
        <div className="inbox-item-detail p-6 overflow-y-auto">
            {/* 标题和状态 */}
            <div className="flex items-start justify-between mb-4">
                <h2 className="text-lg font-semibold text-token-foreground">
                    {item.title ?? "Untitled Task"}
                </h2>
                <span className={`px-2 py-0.5 text-xs rounded ${item.status === "completed" ? "bg-green-100 text-green-600" :
                        item.status === "running" ? "bg-blue-100 text-blue-600" :
                            item.status === "failed" ? "bg-red-100 text-red-600" :
                                "bg-gray-100 text-gray-600"
                    }`}>
                    {item.status ?? "unknown"}
                </span>
            </div>

            {/* 元数据 */}
            {item.createdAt && (
                <div className="text-xs text-token-description-foreground mb-4">
                    Created: {new Date(item.createdAt).toLocaleString()}
                    {item.completedAt && (<> · Completed: {new Date(item.completedAt).toLocaleString()}</>)}
                </div>
            )}

            {/* 描述/内容 */}
            {item.description && (
                <div className="text-sm text-token-foreground mb-4 whitespace-pre-wrap">
                    {item.description}
                </div>
            )}

            {/* 消息列表 (用户消息 + AI 回复) */}
            {item.messages && item.messages.length > 0 && (
                <div className="flex flex-col gap-3 mb-4">
                    {item.messages.map((msg, i) => (
                        <div key={msg.id ?? i} className={`p-3 rounded-lg text-sm ${msg.role === "user"
                                ? "bg-token-foreground/5 text-token-foreground"
                                : "bg-token-primary/10 text-token-foreground"
                            }`}>
                            <div className="text-xs font-medium text-token-description-foreground mb-1">
                                {msg.role === "user" ? "You" : "Codex"}
                            </div>
                            <div className="whitespace-pre-wrap">{msg.text ?? msg.content}</div>
                        </div>
                    ))}
                </div>
            )}

            {/* 代码变更 */}
            {item.changes && item.changes.length > 0 && (
                <div className="mb-4">
                    <h3 className="text-sm font-medium text-token-foreground mb-2">Changes</h3>
                    {item.changes.map((change, i) => (
                        <div key={i} className="flex items-center gap-2 px-2 py-1 text-xs text-token-foreground">
                            <span className={`px-1.5 py-0.5 rounded ${change.action === "create" ? "bg-green-100 text-green-700" :
                                    change.action === "delete" ? "bg-red-100 text-red-700" :
                                        "bg-yellow-100 text-yellow-700"
                                }`}>{change.action}</span>
                            <span className="font-mono truncate">{change.path}</span>
                        </div>
                    ))}
                </div>
            )}

            {/* 操作按钮 */}
            {item.conversationId && (
                <div className="flex gap-2 mt-4">
                    <a
                        href={`/local/${item.conversationId}`}
                        className="px-3 py-1.5 text-xs rounded-md bg-token-primary text-white hover:bg-token-primary-hover transition-colors"
                    >
                        Open Conversation
                    </a>
                </div>
            )}
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

export function SkillsPage() {
    const { data: skillsData } = useQuery("skills/list", {
        placeholderData: { data: [] },
    });
    const skills = skillsData?.data ?? [];

    return (
        <div className="skills-page h-full flex flex-col">
            <div className="flex items-center justify-between p-4 border-b border-token-border">
                <h2 className="text-lg font-semibold text-token-foreground">Skills</h2>
            </div>

            {/* 技能网格 */}
            <div className="skills-grid grid gap-4 p-4" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))" }}>
                {skills.length === 0 ? (
                    <div className="text-sm text-token-description-foreground text-center py-8 col-span-full">
                        No skills available
                    </div>
                ) : (
                    skills.map((skill) => (
                        <SkillCard key={skill.id ?? skill.name} skill={skill} />
                    ))
                )}
            </div>
        </div>
    );
}

export function SkillCard({ skill, canInstall, isInstalled, isInstalling, onInstall }) {
    return (
        <div className="skill-card border border-token-border rounded-lg p-4">
            <h3 className="text-sm font-medium text-token-foreground">{skill.name}</h3>
            <p className="text-xs text-token-description-foreground mt-1">{skill.description}</p>
            <div className="flex gap-2 mt-3">
                {isInstalled ? (
                    <span className="px-2 py-0.5 text-xs rounded bg-green-100 text-green-600">Installed</span>
                ) : (
                    <button
                        className="px-3 py-1 text-xs rounded-md bg-token-primary text-white hover:bg-token-primary-hover transition-colors"
                        onClick={() => onInstall?.(skill.id)}
                        disabled={isInstalling}
                    >
                        {isInstalling ? "Installing..." : "Install"}
                    </button>
                )}
            </div>
        </div>
    );
}
