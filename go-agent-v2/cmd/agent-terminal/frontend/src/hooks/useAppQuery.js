// =============================================================================
// useAppQuery.js — React Query 封装 (Wails 适配版)
//
// 原版通过 bridge.sendRequest + window.message 回调实现查询
// 现简化为直接调用 bridge.callAPI (Wails Go 绑定, 原生 Promise)
// =============================================================================

import { useQuery as useTanstackQuery, useMutation as useTanstackMutation } from "@tanstack/react-query";
import { bridge } from "../bridge";

/**
 * useQuery — 通用查询 Hook
 * 封装 useQuery, 通过 bridge.callAPI 查询后端
 *
 * @param {string} queryKey - 查询键 (同时作为 JSON-RPC method)
 * @param {Object} options
 * @param {Object} options.params - 查询参数
 * @param {Object} options.placeholderData - 占位数据
 * @param {Object} options.queryConfig - 传递给 useQuery 的额外配置
 * @returns {UseQueryResult}
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
            // 直接调用 Wails Go 绑定 (原生 async/await)
            return bridge.callAPI(queryKey, params ?? {});
        },
        placeholderData,
        ...queryConfig,
    });
}

/**
 * useMutation — 重新导出 useMutation
 */
export { useTanstackMutation as useMutation };
