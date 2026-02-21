// =============================================================================
// Codex.app — useConversationManager Hook
// 从 WorktreeInitPage 的 useConversationManager() 调用推导
//
// 功能: 获取 ConversationManager 实例
// =============================================================================

import { useContext } from "react";
import { ConversationManagerContext } from "../contexts";

/**
 * useConversationManager — 获取 ConversationManager 实例
 * 混淆名: (WorktreeInitPage 中引用的 mcpManager)
 *
 * 与 useConversation 的区别:
 *   - useConversation(id) → 获取单个对话状态 (只读)
 *   - useConversationManager() → 获取 ConversationManager 实例 (可调用方法)
 *
 * @returns {ConversationManager}
 */
export function useConversationManager() {
    const cm = useContext(ConversationManagerContext);
    if (!cm) {
        throw new Error("useConversationManager must be used within ConversationManagerContext.Provider");
    }
    return cm;
}
