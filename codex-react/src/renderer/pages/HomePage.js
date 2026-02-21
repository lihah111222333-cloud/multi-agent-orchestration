// =============================================================================
// Codex.app — 首页 (HomePage)
// 混淆名: y7n
// 路由: /
//
// 功能: 新建对话入口 + 历史对话列表
// =============================================================================

import { useNavigate } from "react-router-dom";
import { useConversationManager } from "../hooks/useConversation";
import { useQuery } from "../hooks/useAppQuery";
import { Composer } from "../components/Composer";
import { Toolbar } from "../components/Toolbar";
import { Sidebar } from "../components/Sidebar";

/**
 * HomePage — 首页
 *
 * 功能:
 *   - 新建对话 (通过 Composer)
 *   - 对话历史列表 (pinned + recent)
 */
export function HomePage() {
    const navigate = useNavigate();
    const conversationManager = useConversationManager();

    // 查询对话列表
    const { data: threadsData } = useQuery("threads/list", {
        placeholderData: { threads: [] },
    });

    const allThreads = threadsData?.threads ?? [];
    const pinnedThreads = allThreads.filter((t) => t.pinned);
    const recentThreads = allThreads.filter((t) => !t.pinned);

    // 新建对话
    const handleNewConversation = async (input, options = {}) => {
        const conversationId = await conversationManager.startConversation({
            input,
            workspaceRoots: options.workspaceRoots ?? [],
            cwd: options.cwd,
            collaborationMode: options.collaborationMode ?? null,
            permissions: options.permissions ?? {
                approvalPolicy: "on-failure",
                sandboxPolicy: null,
            },
            attachments: options.attachments ?? [],
        });
        navigate(`/local/${conversationId}`);
    };

    return (
        <div className="h-full flex flex-col">
            <Toolbar title="Home" />

            {/* 对话历史列表 */}
            <div className="flex-1 overflow-y-auto">
                <ThreadList pinnedThreads={pinnedThreads} recentThreads={recentThreads} />
            </div>

            {/* 底部 Composer (新建对话) */}
            <Composer
                onSend={(input, attachments) => handleNewConversation(input, { attachments })}
                placeholder="How can Codex help?"
            />
        </div>
    );
}

// =============================================================================
// ThreadList —  对话历史列表
// 展示 pinned threads + recent threads
// =============================================================================

function ThreadList({ pinnedThreads = [], recentThreads = [] }) {
    const navigate = useNavigate();

    return (
        <div className="thread-list flex flex-col py-2 px-4">
            {/* Pinned threads */}
            {pinnedThreads.length > 0 && (
                <section className="mb-4">
                    <h3 className="text-xs font-medium text-token-description-foreground uppercase mb-2">Pinned</h3>
                    {pinnedThreads.map((t) => (
                        <button
                            key={t.id}
                            className="w-full text-left px-3 py-2 text-sm text-token-foreground rounded-md hover:bg-token-foreground/5 truncate transition-colors"
                            onClick={() => navigate(`/local/${t.id}`)}
                        >
                            {t.title || "Untitled"}
                        </button>
                    ))}
                </section>
            )}

            {/* Recent threads */}
            {recentThreads.length > 0 && (
                <section>
                    <h3 className="text-xs font-medium text-token-description-foreground uppercase mb-2">Recent</h3>
                    {recentThreads.map((t) => (
                        <button
                            key={t.id}
                            className="w-full text-left px-3 py-2 text-sm text-token-foreground rounded-md hover:bg-token-foreground/5 truncate transition-colors"
                            onClick={() => navigate(`/local/${t.id}`)}
                        >
                            {t.title || "Untitled"}
                        </button>
                    ))}
                </section>
            )}

            {pinnedThreads.length === 0 && recentThreads.length === 0 && (
                <div className="text-center py-8 text-sm text-token-description-foreground">
                    No conversations yet. Start a new one below.
                </div>
            )}
        </div>
    );
}
