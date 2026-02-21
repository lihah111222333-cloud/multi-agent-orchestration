// =============================================================================
// Codex.app — 首页 (HomePage)
// 混淆名: y7n
// 路由: /
//
// 功能: 新建对话入口 + 历史对话列表
// =============================================================================

function HomePage() {
    const navigate = useNavigate();
    const conversationManager = useConversationManager();

    // 新建对话
    const handleNewConversation = async (input, options = {}) => {
        const conversationId = await conversationManager.startConversation({
            input,
            workspaceRoots: options.workspaceRoots ?? [],
            cwd: options.cwd,
            collaborationMode: options.collaborationMode ?? null,
            attachments: options.attachments ?? [],
        });
        navigate(`/local/${conversationId}`);
    };

    return (
        <div className="h-full flex flex-col">
            <HomeToolbar />

            {/* 对话历史列表 */}
            <ThreadList />

            {/* 底部 Composer (新建对话) */}
            <Composer
                onSend={(input, attachments) => handleNewConversation(input, { attachments })}
                placeholder="How can Codex help?"
            />
        </div>
    );
}

// =============================================================================
// ThreadList — 对话历史列表
// 展示 pinned threads + recent threads
// =============================================================================

function ThreadList() {
    // 从 global state 获取 thread titles 和 pin 状态
    // 按 updatedAt 排序, pinned 置顶
    return (
        <div className="thread-list">
            {/* Pinned threads */}
            <section>
                <h3>Pinned</h3>
                {pinnedThreads.map((t) => (
                    <ThreadItem key={t.id} thread={t} />
                ))}
            </section>

            {/* Recent threads */}
            <section>
                <h3>Recent</h3>
                {recentThreads.map((t) => (
                    <ThreadItem key={t.id} thread={t} />
                ))}
            </section>
        </div>
    );
}

module.exports = { HomePage };
