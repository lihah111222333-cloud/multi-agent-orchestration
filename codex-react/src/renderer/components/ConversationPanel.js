// =============================================================================
// Codex.app — ConversationPanel 对话面板 (共享组件)
// 混淆名: rO
// 提取自: index-formatted.js L~200000
//
// 功能: 对话渲染容器 (被 ChatPage, ThreadOverlayPage, RemoteTaskPage 共享)
//   - 轮次列表渲染
//   - 流式文本追加
//   - 审批弹窗
//   - 自动滚动
//   - 可配置: 轻量模式 / 完整模式
// =============================================================================

import { useRef, useEffect } from "react";
import { useConversation } from "../hooks/useConversation";
import { MarkdownRenderer } from "./MarkdownRenderer";
import { StreamingIndicator, TurnError, ContextCompaction } from "./StreamingIndicator";
import { Composer } from "./Composer";
import { bridge } from "../bridge";

/**
 * ConversationPanel — 可复用对话面板
 * 混淆名: rO
 *
 * @param {Object} props
 * @param {string} props.conversationId
 * @param {React.ReactNode} props.header - 顶部区域
 * @param {boolean} props.shouldResume - 是否恢复历史对话
 * @param {boolean} props.allowMissingConversation
 * @param {boolean} props.showExternalFooter - 显示底部 Composer
 * @param {boolean} props.isLightweight - 轻量模式 (ThreadOverlay)
 * @param {string} props.className
 */
export function ConversationPanel({
    conversationId,
    header,
    shouldResume = false,
    allowMissingConversation = false,
    showExternalFooter = true,
    isLightweight = false,
    className = "",
}) {
    const conversation = useConversation(conversationId);
    const scrollRef = useRef(null);

    // 自动滚动到底部
    useEffect(() => {
        const el = scrollRef.current;
        if (el) {
            el.scrollTop = el.scrollHeight;
        }
    }, [conversation?.turns]);

    if (!conversation && !allowMissingConversation) {
        return (
            <div className="flex h-full items-center justify-center text-token-description-foreground text-sm">
                Conversation not found
            </div>
        );
    }

    const turns = conversation?.turns ?? [];
    const currentTurn = turns[turns.length - 1];
    const isStreaming = currentTurn?.status === "in_progress";

    return (
        <div className={`conversation-panel flex flex-col h-full ${className}`}>
            {header}

            {/* 对话内容区 */}
            <div ref={scrollRef} className="flex-1 overflow-y-auto">
                <div className={`mx-auto ${isLightweight ? "max-w-full px-3" : "max-w-[800px] px-4"} py-4`}>
                    {turns.map((turn, idx) => (
                        <TurnRenderer key={turn.turnId || idx} turn={turn} isLightweight={isLightweight} />
                    ))}

                    {isStreaming && <StreamingIndicator />}
                </div>
            </div>

            {/* 底部 Composer */}
            {showExternalFooter && !isLightweight && (
                <Composer conversationId={conversationId} onSend={() => { }} />
            )}
        </div>
    );
}

// ===================== TurnRenderer =====================
/**
 * TurnRenderer — 渲染单个 Turn
 * 混淆名: b9
 */
function TurnRenderer({ turn, isLightweight }) {
    if (!turn) return null;

    return (
        <div className="turn-container py-4 border-b border-token-border/50 last:border-b-0">
            {(turn.items ?? []).map((item, idx) => (
                <TurnItemRenderer key={item.id || idx} item={item} isLightweight={isLightweight} />
            ))}

            {turn.error && <TurnError error={turn.error} />}
        </div>
    );
}

// ===================== TurnItemRenderer =====================
/**
 * TurnItemRenderer — 渲染单个 TurnItem
 *
 * TurnItem 类型:
 *   - user_message: 用户消息
 *   - assistant_message: AI 回复
 *   - tool_call: 工具调用
 *   - tool_result: 工具结果
 *   - file_change: 文件修改
 *   - terminal_command: 终端命令
 *   - terminal_output: 终端输出
 *   - approval_request: 审批请求
 *   - context_compaction: 上下文压缩
 *   - system_message: 系统消息
 */
function TurnItemRenderer({ item, isLightweight }) {
    switch (item.type) {
        case "user_message":
            return <UserMessage message={item.text} />;
        case "assistant_message":
            return (
                <div className="assistant-message py-2">
                    <MarkdownRenderer>{item.text}</MarkdownRenderer>
                </div>
            );
        case "tool_call":
            return (
                <div className="tool-call py-1 text-xs text-token-description-foreground">
                    <span className="font-mono bg-token-background-secondary px-1.5 py-0.5 rounded">
                        {item.name}({item.arguments ? "..." : ""})
                    </span>
                </div>
            );
        case "tool_result":
            return (
                <div className="tool-result py-1 text-xs">
                    <ToolResultRenderer result={item} />
                </div>
            );
        case "file_change":
            return <FileChangeRenderer change={item} isLightweight={isLightweight} />;
        case "terminal_command":
            return (
                <div className="terminal-command py-1 font-mono text-sm bg-token-editor-background rounded px-3 py-2 my-1">
                    <span className="text-token-primary">$ </span>
                    <span className="text-token-foreground">{item.command}</span>
                </div>
            );
        case "terminal_output":
            return (
                <div className="terminal-output py-1 font-mono text-xs bg-token-editor-background rounded px-3 py-2 my-1 text-token-description-foreground max-h-[200px] overflow-auto whitespace-pre">
                    {item.output}
                </div>
            );
        case "approval_request":
            return <ApprovalRequestRenderer request={item} />;
        case "context_compaction":
            return <ContextCompaction completed={item.completed} />;
        case "system_message":
            return (
                <div className="system-message py-1 text-xs text-token-description-foreground italic text-center">
                    {item.text}
                </div>
            );
        default:
            return null;
    }
}

// ===================== 子组件 =====================

/**
 * UserMessage — 用户消息气泡
 * 混淆名: (WorktreeInitPage, ConversationPanel 中引用)
 */
export function UserMessage({ message, alwaysShowActions = false }) {
    return (
        <div className="user-message flex justify-end py-2">
            <div className="bg-token-primary/10 text-token-foreground rounded-2xl rounded-br-sm px-4 py-2.5 max-w-[85%]">
                <span className="text-sm whitespace-pre-wrap">{message}</span>
            </div>
        </div>
    );
}

function ToolResultRenderer({ result }) {
    if (result.error) {
        return <span className="text-token-error-foreground">Error: {result.error}</span>;
    }
    return (
        <div className="text-token-description-foreground bg-token-background-secondary rounded px-2 py-1 text-xs my-1 max-h-[100px] overflow-auto">
            {typeof result.output === "string" ? result.output : JSON.stringify(result.output, null, 2)}
        </div>
    );
}

function FileChangeRenderer({ change, isLightweight }) {
    return (
        <div className="file-change flex items-center gap-2 py-1 text-sm">
            <span className={`px-1.5 py-0.5 rounded text-xs font-medium
                ${change.action === "create" ? "bg-green-100 text-green-700" :
                    change.action === "delete" ? "bg-red-100 text-red-700" :
                        "bg-yellow-100 text-yellow-700"}`}>
                {change.action}
            </span>
            <span className="font-mono text-token-foreground truncate">{change.path}</span>
        </div>
    );
}

function ApprovalRequestRenderer({ request }) {
    const handleApprove = () => {
        bridge.sendResponse({
            id: request.requestId,
            result: { decision: "approve" },
        });
    };
    const handleDeny = () => {
        bridge.sendResponse({
            id: request.requestId,
            result: { decision: "deny" },
        });
    };

    return (
        <div className="approval-request border border-yellow-300 bg-yellow-50 rounded-lg p-3 my-2">
            <div className="text-sm font-medium text-yellow-800">
                Approval Required
            </div>
            <div className="text-xs text-yellow-700 mt-1">
                {request.description || `${request.toolName}: ${request.command || "action"}`}
            </div>
            <div className="flex gap-2 mt-2">
                <button
                    className="px-3 py-1 text-xs bg-green-500 text-white rounded hover:bg-green-600"
                    onClick={handleApprove}
                >
                    Approve
                </button>
                <button
                    className="px-3 py-1 text-xs bg-red-500 text-white rounded hover:bg-red-600"
                    onClick={handleDeny}
                >
                    Deny
                </button>
            </div>
        </div>
    );
}

export default ConversationPanel;
