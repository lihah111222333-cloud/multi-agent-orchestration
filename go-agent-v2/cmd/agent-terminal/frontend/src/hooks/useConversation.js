// =============================================================================
// Codex.app — useConversation Hook
// 提取自: index-formatted.js
//
// 功能: 订阅并返回指定 conversationId 的实时状态
// =============================================================================

import { useState, useEffect } from "react";
import { useConversationManager } from "./useConversationManager";

/**
 * useConversation — 订阅对话状态
 *
 * @param {string|null} conversationId
 * @returns {ConversationState|null}
 *
 * 内部通过 ConversationManager.subscribeToConversation() 订阅变更
 * 每当 onNotification/startTurn 等操作修改对话状态时, 触发重渲染
 */
export function useConversation(conversationId) {
    const conversationManager = useConversationManager();
    const [state, setState] = useState(
        conversationId ? conversationManager.getConversation(conversationId) : null
    );

    useEffect(() => {
        if (!conversationId) {
            setState(null);
            return;
        }
        // 初始加载
        setState(conversationManager.getConversation(conversationId));

        // 订阅变更回调
        const unsubscribe = conversationManager.subscribeToConversation(conversationId, (newState) => {
            setState(newState);
        });

        return unsubscribe;
    }, [conversationId, conversationManager]);

    return state;
}

/**
 * useCurrentTurn — 获取当前 (最新) turn
 */
export function useCurrentTurn(conversationId) {
    const conversation = useConversation(conversationId);
    if (!conversation || conversation.turns.length === 0) return null;
    return conversation.turns[conversation.turns.length - 1];
}

/**
 * useStreamingState — 获取当前对话的流式状态
 */
export function useStreamingState(conversationId) {
    const conversationManager = useConversationManager();
    const [isStreaming, setIsStreaming] = useState(false);

    useEffect(() => {
        if (!conversationId) return;
        setIsStreaming(conversationManager.isStreaming(conversationId));
        const unsub = conversationManager.addStreamingCallback(conversationId, setIsStreaming);
        return unsub;
    }, [conversationId, conversationManager]);

    return isStreaming;
}

/**
 * useConversationManager — 从 Context 获取 ConversationManager 实例
 * 统一从 useConversationManager.js 重新导出, 避免重复定义
 */
export { useConversationManager } from "./useConversationManager";

/**
 * useApprovalRequests — 获取待处理的审批请求
 */
export function useApprovalRequests(conversationId) {
    const conversation = useConversation(conversationId);
    return conversation?.requests ?? [];
}

/**
 * useTokenUsage — 获取 token 使用量
 */
export function useTokenUsage(conversationId) {
    const conversation = useConversation(conversationId);
    return conversation?.latestTokenUsageInfo ?? null;
}
