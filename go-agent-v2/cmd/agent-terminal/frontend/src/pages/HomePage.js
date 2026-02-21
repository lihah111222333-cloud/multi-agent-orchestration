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
import { HomeToolbar } from "../components/Toolbar";

/**
 * HomePage — 首页
 *
 * 功能:
 *   - 新建对话 (通过 Composer)
 *   - 居中欢迎视图
 */
export function HomePage() {
    const navigate = useNavigate();
    const conversationManager = useConversationManager();

    // 新建对话
    const handleNewConversation = async (input, options = {}) => {
        try {
            console.log("[HomePage] startConversation: begin", input);
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
            console.log("[HomePage] startConversation: done, navigating to", conversationId);
            navigate(`/local/${conversationId}`);
        } catch (err) {
            console.error("[HomePage] startConversation: FAILED", err);
        }
    };

    return (
        <div className="h-full flex flex-col">
            <HomeToolbar />

            {/* 居中欢迎内容 */}
            <div className="flex-1 flex items-center justify-center">
                <div className="text-center max-w-md px-6">
                    <h1 className="text-2xl font-semibold text-token-foreground mb-2">
                        What can I help you with?
                    </h1>
                    <p className="text-sm text-token-description-foreground">
                        Ask Codex to write code, answer questions, or help with tasks.
                    </p>
                </div>
            </div>

            {/* 底部 Composer (新建对话) */}
            <Composer
                onSend={(input, attachments) => handleNewConversation(input, { attachments })}
                placeholder="How can Codex help?"
            />
        </div>
    );
}
