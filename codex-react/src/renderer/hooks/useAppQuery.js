// =============================================================================
// Codex.app — useAppQuery Hook (React Query 包装)
// 从 index-formatted.js 中的 useQuery 调用模式推导
//
// 功能: 封装 @tanstack/react-query, 通过 MCP JSON-RPC 查询数据
// =============================================================================

import { useQuery as useTanstackQuery, useMutation as useTanstackMutation } from "@tanstack/react-query";
import { bridge } from "../bridge";

/**
 * useQuery — Codex 专用查询 Hook
 * 封装 useQuery, 将查询方法映射为 MCP JSON-RPC 调用
 *
 * @param {string} queryKey - 查询键 (同时作为 JSON-RPC method)
 * @param {Object} options
 * @param {Object} options.params - JSON-RPC 参数
 * @param {Object} options.placeholderData - 占位数据
 * @param {Object} options.queryConfig - 传递给 useQuery 的额外配置
 * @returns {UseQueryResult}
 *
 * 内部流程:
 *   1. 构建 JSON-RPC 请求 { method: queryKey, params }
 *   2. 通过 bridge.sendRequest 发送
 *   3. 等待 ConversationManager.onResult 返回
 *
 * 用例:
 *   const { data, isLoading } = useQuery("workspace-root-options");
 *   const { data: skills } = useQuery("skills/list", { params: { scope: "all" } });
 */
export function useQuery(queryKey, options = {}) {
    const { params, placeholderData, queryConfig = {} } = options;

    return useTanstackQuery({
        queryKey: [queryKey, params],
        queryFn: async () => {
            // 通过 MCP JSON-RPC 查询
            const id = `query:${queryKey}:${crypto.randomUUID()}`;
            return new Promise((resolve, reject) => {
                // 注册一次性响应监听
                const handler = (event) => {
                    const msg = event.data;
                    if (msg?.type === "mcp-response" && msg.message?.id === id) {
                        window.removeEventListener("message", handler);
                        if (msg.message.error) {
                            reject(new Error(msg.message.error.message));
                        } else {
                            resolve(msg.message.result);
                        }
                    }
                };
                window.addEventListener("message", handler);
                bridge.sendRequest({ id, method: queryKey, params: params ?? {} });
            });
        },
        placeholderData,
        ...queryConfig,
    });
}

/**
 * useMutation — 重新导出 useMutation
 * 直接暴露 @tanstack/react-query 的 useMutation
 */
export { useTanstackMutation as useMutation };
