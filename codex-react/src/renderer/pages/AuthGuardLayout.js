// =============================================================================
// Codex.app — 认证守卫布局 (AuthGuardLayout)
// 混淆名: EPt
// 路由: (包裹所有需要认证的页面)
// 提取自: index-formatted.js L107501
//
// 功能: 检查用户认证状态, 未登录则重定向到 /login
// =============================================================================

import { Navigate } from "react-router-dom";
import { useAuth } from "../hooks/useAuth";
import { AuthenticatedLayout } from "../components/AuthenticatedLayout";

/**
 * AuthGuardLayout — 认证守卫
 *
 * 逻辑:
 *   1. 如果 isLoading → 返回空 Fragment (等待)
 *   2. 如果已认证 (authMethod 存在) 或不需要认证 (requiresAuth === false)
 *      → 渲染 AuthenticatedLayout (kPt, 包含侧边栏等)
 *   3. 否则 → 重定向到 /login
 *
 * 依赖:
 *   - useAuth() (k1) — { authMethod, requiresAuth, isLoading }
 *   - AuthenticatedLayout (kPt) — 已认证后的布局 (含 Sidebar + Outlet)
 */
function AuthGuardLayout() {
    const { authMethod, requiresAuth, isLoading } = useAuth(); // k1()

    // 加载中或 requiresAuth 尚未确定, 不渲染任何内容
    if (isLoading || requiresAuth === null) {
        return <></>;
    }

    // 不需要认证 (如 API key 模式) 或已认证 → 渲染已认证布局
    if (requiresAuth === false || authMethod != null) {
        return <AuthenticatedLayout />;  // kPt
    }

    // 需要认证但未认证 → 重定向到登录页
    return <Navigate to="/login" replace />;
}

export { AuthGuardLayout };
