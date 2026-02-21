// =============================================================================
// Codex.app — 主聊天页面 (ChatPage)
// 混淆名: Kbn
// 路由: /local/:conversationId
// 提取自: index-formatted.js (多处引用)
//
// 这是 Codex.app 的核心页面, 包含:
//   - 对话消息列表渲染 (委托给 ConversationPanel)
//   - 输入框 (Composer)
//   - 审批弹窗
//   - Diff 面板
//   - 终端面板
// =============================================================================

import { useEffect } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { useConversation, useConversationManager } from "../hooks/useConversation";
import { ChatToolbar } from "../components/Toolbar";
import { Composer } from "../components/Composer";
import { ConversationPanel } from "../components/ConversationPanel";
import { LoadingSpinner } from "../components/LoadingSpinner";

/**
 * ChatPage — 主对话界面
 *
 * 路由参数: { conversationId: string }
 *
 * 核心组件结构:
 *
 *   ChatPage
 *   ├── ChatToolbar (顶部工具栏: 标题/模型/操作)
 *   ├── ConversationPanel (对话面板 — 共享组件)
 *   │   ├── TurnRenderer (轮次列表)
 *   │   │   ├── UserMessage (用户消息)
 *   │   │   ├── AgentMessage (AI 回复)
 *   │   │   ├── ReasoningBlock (推理/Thinking)
 *   │   │   ├── CommandExecution (命令执行)
 *   │   │   ├── FileChange (文件修改)
 *   │   │   ├── McpToolCall (MCP 工具调用)
 *   │   │   ├── PlanBlock / TodoList
 *   │   │   └── ErrorBlock (错误信息)
 *   │   └── ApprovalRequestRenderer (审批请求弹窗)
 *   └── Composer (输入框)
 *       ├── TextInput (文本输入)
 *       ├── AttachmentBar (附件栏)
 *       └── SendButton (发送按钮)
 *
 * 关键 Hooks:
 *   useConversation(conversationId) → 订阅对话状态变化
 *   useCurrentTurn()               → 获取当前 turn
 *   useStreamingState()            → 获取流式输出状态
 */

export function ChatPage() {
    const { conversationId } = useParams();
    const conversation = useConversation(conversationId);
    const conversationManager = useConversationManager();

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

            {/* 主对话区域 — 使用共享 ConversationPanel */}
            <ConversationPanel
                conversationId={conversationId}
                shouldResume={conversation.resumeState === "needs_resume"}
                showExternalFooter={false}
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
// LoadingOrRedirect — 对话不存在时的加载/重定向
// =============================================================================

function LoadingOrRedirect({ conversationId }) {
    const conversationManager = useConversationManager();
    const navigate = useNavigate();

    useEffect(() => {
        if (!conversationId) {
            navigate("/", { replace: true });
            return;
        }
        // 尝试恢复对话
        conversationManager.resumeConversation(conversationId).catch(() => {
            // thread/resume 失败 (重启后线程丢失),
            // 创建空的对话状态, 让页面保持可用 (用户可发新消息)
            console.warn("[ChatPage] thread/resume failed for", conversationId, "— creating empty state");
            conversationManager.setConversation({
                id: conversationId,
                turns: [],
                requests: [],
                createdAt: Date.now(),
                updatedAt: Date.now(),
                title: null,
                latestModel: null,
                hasUnreadTurn: false,
                rolloutPath: "",
                cwd: null,
                resumeState: "expired",
                latestTokenUsageInfo: null,
            });
        });
    }, [conversationId, conversationManager, navigate]);

    return (
        <div className="h-full flex items-center justify-center">
            <LoadingSpinner />
        </div>
    );
}
