// =============================================================================
// Codex.app — Worktree Hooks
// 从 WorktreeInitPage 中的 Hook 调用推导
//
// 功能: Worktree 生命周期管理
// =============================================================================

import { useState, useEffect, useCallback } from "react";
import { bridge } from "../bridge";

/**
 * useWorktreeManager — Worktree 管理操作
 * 混淆名: (WorktreeInitPage 中引用)
 *
 * @returns {{
 *   updatePendingWorktree: (id: string, updater: Function) => void,
 *   abortAndRemovePendingWorktree: (id: string) => void
 * }}
 */
export function useWorktreeManager() {
    const updatePendingWorktree = useCallback((id, updater) => {
        // 通过 IPC 更新 pending worktree 状态
        // 原始实现通过 Worker 消息通道
        bridge.sendWorkerMessage("worktree", {
            type: "update-pending-worktree",
            id,
        });
    }, []);

    const abortAndRemovePendingWorktree = useCallback((id) => {
        bridge.sendWorkerMessage("worktree", {
            type: "abort-and-remove-pending-worktree",
            id,
        });
    }, []);

    return { updatePendingWorktree, abortAndRemovePendingWorktree };
}

/**
 * usePendingWorktree — 订阅特定 pending worktree 的状态
 * 混淆名: (WorktreeInitPage 中引用)
 *
 * @param {string} pendingWorktreeId
 * @returns {PendingWorktree|null}
 *
 * PendingWorktree:
 *   id: string
 *   phase: "queued" | "creating" | "worktree-ready" | "conversation-starting" | "failed"
 *   prompt: string
 *   launchMode: "start-conversation" | "fork-conversation" | "create-stable-worktree"
 *   conversationId: string | null
 *   sourceConversationId: string | null
 *   sourceCollaborationMode: object | null
 *   sourceWorkspaceRoot: string
 *   startConversationParamsInput: object | null
 *   outputText: string
 *   errorMessage: string | null
 *   needsAttention: boolean
 */
export function usePendingWorktree(pendingWorktreeId) {
    const [worktree, setWorktree] = useState(null);

    useEffect(() => {
        if (!pendingWorktreeId) {
            setWorktree(null);
            return;
        }

        // 订阅 worktree Worker 消息
        const unsubscribe = bridge.subscribeToWorkerMessages("worktree", (msg) => {
            if (msg.type === "pending-worktree-updated" && msg.id === pendingWorktreeId) {
                setWorktree(msg.worktree);
            }
        });

        // 请求初始状态
        bridge.sendWorkerMessage("worktree", {
            type: "get-pending-worktree",
            id: pendingWorktreeId,
        });

        return unsubscribe;
    }, [pendingWorktreeId]);

    return worktree;
}
