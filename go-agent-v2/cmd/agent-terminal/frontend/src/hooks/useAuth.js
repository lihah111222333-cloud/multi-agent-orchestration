// =============================================================================
// Codex.app — useAuth Hook
// 混淆名: k1
// 提取自: index-formatted.js (AuthContext 消费)
//
// 功能: 从 AuthContext 读取当前认证状态
// =============================================================================

import { useContext } from "react";
import { AuthContext } from "../contexts";

/**
 * useAuth — 获取认证状态
 * 混淆名: k1()
 *
 * @returns {{
 *   authMethod: "chatgpt"|"copilot"|"api-key"|null,
 *   userId: string|null,
 *   accountId: string|null,
 *   email: string|null,
 *   requiresAuth: boolean|null,
 *   planAtLogin: string|null
 * }}
 */
export function useAuth() {
    return useContext(AuthContext);
}
