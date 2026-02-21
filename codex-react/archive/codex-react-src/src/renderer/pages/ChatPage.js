// =============================================================================
// Codex.app — 主聊天页面 (ChatPage)
// 混淆名: Kbn
// 路由: /local/:conversationId
// 提取自: index-formatted.js (多处引用)
//
// 这是 Codex.app 的核心页面, 包含:
//   - 对话消息列表渲染
//   - 输入框 (Composer)
//   - 审批弹窗
//   - Diff 面板
//   - 终端面板
// =============================================================================

/**
 * ChatPage — 主对话界面
 *
 * 路由参数: { conversationId: string }
 *
 * 核心组件结构:
 *
 *   ChatPage
 *   ├── ChatToolbar (顶部工具栏: 标题/模型/操作)
 *   ├── ConversationPanel (对话面板, 含包裹组件 rO)
 *   │   ├── TurnList (轮次列表)
 *   │   │   ├── UserMessage (用户消息, 混淆名 b9)
 *   │   │   │   └── InputItemRenderer (text/image/skill/mention)
 *   │   │   ├── AgentMessage (AI 回复)
 *   │   │   │   └── MarkdownRenderer (QZ - Markdown 渲染)
 *   │   │   ├── ReasoningBlock (推理/Thinking)
 *   │   │   ├── CommandExecution (命令执行)
 *   │   │   │   └── OutputViewer (输出查看器)
 *   │   │   ├── FileChange (文件修改)
 *   │   │   │   └── DiffViewer (Diff 查看器)
 *   │   │   ├── McpToolCall (MCP 工具调用)
 *   │   │   ├── PlanBlock (Plan 展示)
 *   │   │   ├── TodoList (Todo 列表)
 *   │   │   └── ErrorBlock (错误信息)
 *   │   └── ApprovalRequest (审批请求弹窗)
 *   │       ├── CommandApproval (命令审批)
 *   │       └── FileChangeApproval (文件修改审批)
 *   ├── Composer (输入框)
 *   │   ├── TextInput (文本输入)
 *   │   ├── AttachmentBar (附件栏: 图片/技能/Mention)
 *   │   ├── ModelSelector (模型选择器)
 *   │   └── SendButton (发送按钮)
 *   ├── DiffPanel (Diff 侧面板)
 *   └── TerminalPanel (终端面板)
 *
 * 关键 Hooks:
 *   useConversation(conversationId) → 订阅对话状态变化
 *   useCurrentTurn()               → 获取当前 turn
 *   useStreamingState()            → 获取流式输出状态
 */

function ChatPage() {
    const { conversationId } = useParams();
    const conversation = useConversation(conversationId);
    const navigate = useNavigate();

    // 如果对话不存在, 尝试恢复或重定向
    if (!conversation) {
        return <LoadingOrRedirect conversationId={conversationId} />;
    }

    return (
        <div className="h-full flex flex-col">
            {/* 顶部工具栏 */}
            <ChatToolbar
                conversationId={conversationId}
                title={conversation.title}
                model={conversation.latestModel}
                cwd={conversation.cwd}
            />

            {/* 主对话区域 */}
            <ConversationPanel
                conversationId={conversationId}
                shouldResume={conversation.resumeState === "needs_resume"}
            />

            {/* 底部输入框 */}
            <Composer
                conversationId={conversationId}
                onSend={(input, attachments) => {
                    conversationManager.startTurn(conversationId, {
                        input,
                        attachments,
                    });
                }}
            />
        </div>
    );
}

// =============================================================================
// ConversationPanel (混淆名: rO)
// 包裹对话渲染, 管理滚动/虚拟列表
// =============================================================================

function ConversationPanel({
    conversationId,
    header,
    shouldResume,
    allowMissingConversation = false,
    showExternalFooter = true,
    isLightweight = false,
}) {
    const conversation = useConversation(conversationId);

    return (
        <div className="flex-1 overflow-y-auto">
            {header}

            {/* 渲染每个 Turn */}
            {conversation?.turns.map((turn, index) => (
                <TurnRenderer
                    key={turn.turnId ?? index}
                    turn={turn}
                    conversationId={conversationId}
                    isLatest={index === conversation.turns.length - 1}
                />
            ))}

            {/* 审批请求 */}
            {conversation?.requests.map((req) => (
                <ApprovalRequestRenderer key={req.id} request={req} conversationId={conversationId} />
            ))}
        </div>
    );
}

// =============================================================================
// TurnRenderer — 渲染单个对话轮次
// =============================================================================

function TurnRenderer({ turn, conversationId, isLatest }) {
    return (
        <div className="turn-container">
            {/* 用户消息 */}
            {turn.params?.input?.length > 0 && (
                <UserMessage content={turn.params.input} />
            )}

            {/* 轮次中的所有 Item */}
            {turn.items.map((item) => {
                switch (item.type) {
                    case "agentMessage":
                        return <AgentMessage key={item.id} text={item.text} />;
                    case "commandExecution":
                        return <CommandExecution key={item.id} item={item} />;
                    case "fileChange":
                        return <FileChange key={item.id} item={item} />;
                    case "reasoning":
                        return <ReasoningBlock key={item.id} item={item} />;
                    case "plan":
                        return <PlanBlock key={item.id} text={item.text} />;
                    case "planImplementation":
                        return <PlanImplementation key={item.id} item={item} />;
                    case "todo-list":
                        return <TodoList key={item.id} plan={item.plan} />;
                    case "mcpToolCall":
                        return <McpToolCall key={item.id} item={item} />;
                    case "error":
                        return <ErrorBlock key={item.id} message={item.message} willRetry={item.willRetry} />;
                    case "contextCompaction":
                        return <ContextCompaction key={item.id} completed={item.completed} />;
                    default:
                        return null;
                }
            })}

            {/* 轮次状态 */}
            {isLatest && turn.status === "inProgress" && <StreamingIndicator />}
            {turn.error && <TurnError error={turn.error} />}
        </div>
    );
}

// =============================================================================
// UserMessage (混淆名: b9)
// =============================================================================

function UserMessage({ content, alwaysShowActions = false }) {
    return (
        <div className="user-message">
            {content.map((item, i) => {
                switch (item.type) {
                    case "text":
                        return <p key={i}>{item.text}</p>;
                    case "image":
                        return <img key={i} src={item.url} alt="User uploaded" />;
                    case "localImage":
                        return <img key={i} src={`file://${item.path}`} alt="Local image" />;
                    case "skill":
                        return <SkillBadge key={i} name={item.name} path={item.path} />;
                    case "mention":
                        return <MentionBadge key={i} name={item.name} path={item.path} />;
                    default:
                        return null;
                }
            })}
        </div>
    );
}

// =============================================================================
// AgentMessage — AI 回复 (Markdown 渲染)
// =============================================================================

function AgentMessage({ text }) {
    return (
        <div className="agent-message">
            <MarkdownRenderer>{text}</MarkdownRenderer>
        </div>
    );
}

// =============================================================================
// ApprovalRequest — 审批弹窗
// =============================================================================

function ApprovalRequestRenderer({ request, conversationId }) {
    const handleDecision = (decision) => {
        // decision: "accept" | "acceptForSession" | "decline"
        if (request.method === "item/commandExecution/requestApproval") {
            conversationManager.replyWithCommandExecutionApprovalDecision(conversationId, request.id, decision);
        } else if (request.method === "item/fileChange/requestApproval") {
            conversationManager.replyWithFileChangeApprovalDecision(conversationId, request.id, decision);
        }
    };

    return (
        <div className="approval-request">
            <p>{request.params.reason ?? "Agent requests approval"}</p>
            <div className="flex gap-2">
                <button onClick={() => handleDecision("accept")}>Approve</button>
                <button onClick={() => handleDecision("acceptForSession")}>Approve for session</button>
                <button onClick={() => handleDecision("decline")}>Decline</button>
            </div>
        </div>
    );
}

module.exports = {
    ChatPage,
    ConversationPanel,
    TurnRenderer,
    UserMessage,
    AgentMessage,
    ApprovalRequestRenderer,
};
