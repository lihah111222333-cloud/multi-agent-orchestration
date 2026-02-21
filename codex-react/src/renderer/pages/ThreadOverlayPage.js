// =============================================================================
// Codex.app — 浮动窗口对话页 (ThreadOverlayPage)
// 混淆名: FAn
// 路由: /thread-overlay/:conversationId
// 提取自: index-formatted.js L330926
//
// 功能: 在独立浮动窗口中显示对话
//   - 精简版的 ChatPage, 用于 "Always on top" 模式
//   - 包含 ConversationPanel (对话渲染)
//   - 支持 pin/unpin 浮动状态
//   - 轻量级模式 (isLightweight = true)
// =============================================================================

import { useParams } from "react-router-dom";
import { Navigate } from "react-router-dom";
import { useConversation } from "../hooks/useConversation";
import { ConversationPanel } from "../components/ConversationPanel";
import { PinIcon } from "../components/Icons";
import { bridge } from "../bridge";

/**
 * ThreadOverlayPage — 浮动窗口对话
 *
 * ConversationPanel 的轻量化包装:
 *   - 头部显示对话标题 + pin/unpin 按钮
 *   - 使用 ConversationPanel 渲染对话 (isLightweight=true, showExternalFooter=false)
 *   - 通过 Electron IPC 控制窗口 always-on-top 状态
 */
function ThreadOverlayPage() {
    const { conversationId } = useParams();
    const conversation = useConversation(conversationId);

    // 切换浮动状态
    const handleToggleFloat = (currentFloat) => {
        const shouldFloat = !currentFloat;
        bridge.dispatchMessage("thread-overlay-set-always-on-top", { shouldFloat });
        return shouldFloat;
    };

    if (!conversation) {
        return <Navigate to="/" replace />;
    }

    // 头部: 对话标题 + pin 按钮
    const header = (
        <div className="flex items-center justify-between p-2">
            <span className="text-sm font-medium truncate">
                {conversation.title || "Thread"}
            </span>
            <button
                onClick={() => handleToggleFloat(false)}
                className="p-1 rounded hover:bg-token-foreground/10"
            >
                <PinIcon className="icon-xs" />
            </button>
        </div>
    );

    return (
        <div className="h-full flex flex-col">
            <ConversationPanel
                header={header}
                className="h-full [--padding-panel:calc(var(--padding-panel-base)/2)]"
                conversationId={conversationId}
                shouldResume={false}
                allowMissingConversation={true}
                showExternalFooter={false}
                isLightweight={true}
            />
        </div>
    );
}

export { ThreadOverlayPage };
