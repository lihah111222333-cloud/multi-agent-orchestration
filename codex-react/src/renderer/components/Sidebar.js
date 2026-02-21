// =============================================================================
// Codex.app — Sidebar 侧边栏
// 混淆名: (AuthenticatedLayout 内部)
// 提取自: index-formatted.js L~250000 附近
//
// 功能: 全局左侧导航栏
//   - 创建新对话 (+ 按钮)
//   - 导航: Home / Inbox / Skills / Settings
//   - 最近对话列表
//   - 可折叠
// =============================================================================

import { useNavigate, useLocation } from "react-router-dom";
import { PlusIcon, SearchIcon, SettingsIcon } from "./Icons";
import { useQuery } from "../hooks/useAppQuery";

/**
 * Sidebar — 全局侧边栏
 *
 * 功能:
 *   - "New conversation" 按钮
 *   - 最近对话列表
 *   - 底部: Settings / Inbox / Skills 快速入口
 */
export function Sidebar() {
    const navigate = useNavigate();
    const location = useLocation();

    const navItems = [
        { path: "/", label: "Home", emoji: "🏠" },
        { path: "/inbox", label: "Inbox", emoji: "📥" },
        { path: "/skills", label: "Skills", emoji: "🧰" },
    ];

    return (
        <aside className="sidebar flex flex-col w-60 border-r border-token-border bg-token-background-secondary h-full">
            {/* 顶部: 新建对话 + 搜索 */}
            <div className="flex items-center gap-2 p-3 border-b border-token-border draggable">
                <button
                    className="flex-1 flex items-center gap-2 px-3 py-1.5 text-sm text-token-foreground bg-token-background rounded-md hover:bg-token-foreground/5 transition-colors"
                    onClick={() => navigate("/")}
                >
                    <PlusIcon className="w-3.5 h-3.5" />
                    New conversation
                </button>
            </div>

            {/* 对话列表 */}
            <div className="flex-1 overflow-y-auto">
                <ThreadList />
            </div>

            {/* 底部导航 */}
            <div className="flex flex-col border-t border-token-border p-2">
                {navItems.map((item) => (
                    <button
                        key={item.path}
                        className={`flex items-center gap-2 px-3 py-2 text-sm rounded-md transition-colors
                            ${location.pathname === item.path
                                ? "bg-token-foreground/10 text-token-foreground"
                                : "text-token-description-foreground hover:text-token-foreground hover:bg-token-foreground/5"}`}
                        onClick={() => navigate(item.path)}
                    >
                        <span>{item.emoji}</span>
                        <span>{item.label}</span>
                    </button>
                ))}

                <button
                    className={`flex items-center gap-2 px-3 py-2 text-sm rounded-md transition-colors
                        ${location.pathname.startsWith("/settings")
                            ? "bg-token-foreground/10 text-token-foreground"
                            : "text-token-description-foreground hover:text-token-foreground hover:bg-token-foreground/5"}`}
                    onClick={() => navigate("/settings/agent-settings")}
                >
                    <SettingsIcon className="w-4 h-4" />
                    <span>Settings</span>
                </button>
            </div>
        </aside>
    );
}

/**
 * ThreadList — 最近对话列表
 * 混淆名: (HomePage/Sidebar 中引用)
 *
 * 显示 pinnedThreads 和 recentThreads
 */
export function ThreadList({ pinnedThreads: propPinned, recentThreads: propRecent }) {
    // 当 Sidebar 无参调用时, 自主通过 useQuery 获取数据
    const { data: threadsData } = useQuery("threads/list", {
        placeholderData: { threads: [] },
    });
    const allThreads = threadsData?.threads ?? [];
    const pinnedThreads = propPinned ?? allThreads.filter((t) => t.pinned);
    const recentThreads = propRecent ?? allThreads.filter((t) => !t.pinned);

    return (
        <div className="thread-list flex flex-col py-2">
            {/* Pinned */}
            {pinnedThreads?.length > 0 && (
                <div className="px-3 py-1">
                    <span className="text-xs font-medium text-token-description-foreground uppercase">Pinned</span>
                    {pinnedThreads.map((thread) => (
                        <ThreadItem key={thread.id} thread={thread} />
                    ))}
                </div>
            )}
            {/* Recent */}
            {recentThreads?.length > 0 && (
                <div className="px-3 py-1">
                    <span className="text-xs font-medium text-token-description-foreground uppercase">Recent</span>
                    {recentThreads.map((thread) => (
                        <ThreadItem key={thread.id} thread={thread} />
                    ))}
                </div>
            )}
            {!pinnedThreads?.length && !recentThreads?.length && (
                <div className="px-3 py-4 text-xs text-token-description-foreground text-center">
                    No conversations yet
                </div>
            )}
        </div>
    );
}

function ThreadItem({ thread }) {
    const navigate = useNavigate();
    return (
        <button
            className="w-full text-left px-2 py-1.5 text-sm text-token-foreground rounded hover:bg-token-foreground/5 truncate transition-colors"
            onClick={() => navigate(`/local/${thread.id}`)}
        >
            {thread.title || "Untitled"}
        </button>
    );
}

export default Sidebar;
