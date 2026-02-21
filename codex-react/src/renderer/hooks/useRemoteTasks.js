// =============================================================================
// Codex.app — Remote Tasks Hooks
// 从 SelectWorkspacePage, RemoteTaskPage 中的 Hook 调用推导
//
// 功能: 远程 (云端) 任务查询
// =============================================================================

import { useQuery } from "./useAppQuery";

/**
 * useRemoteTasks — 获取远程任务列表
 * 混淆名: eN (SelectWorkspacePage 中引用)
 *
 * @returns {UseQueryResult<RemoteTask[]>}
 *
 * RemoteTask:
 *   id: string
 *   title: string
 *   status: "running" | "completed" | "failed"
 *   conversationId: string
 *   workspacePath: string
 *   cwd: string
 *   createdAt: number
 */
export function useRemoteTasks() {
    return useQuery("tasks/list", {
        placeholderData: [],
    });
}

/**
 * useRemoteTask — 获取单个远程任务详情
 * 混淆名: (RemoteTaskPage 中引用)
 *
 * @param {string} taskId
 * @returns {UseQueryResult<RemoteTask>}
 */
export function useRemoteTask(taskId) {
    return useQuery("tasks/get", {
        params: { taskId },
        queryConfig: { enabled: !!taskId },
    });
}
