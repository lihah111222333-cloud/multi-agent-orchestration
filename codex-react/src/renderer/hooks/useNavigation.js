// =============================================================================
// Codex.app — useNavigation Hooks
// 从 index-formatted.js 推导
//
// 功能: 导航辅助 Hooks
// =============================================================================

import { useCallback } from "react";
import { useNavigate } from "react-router-dom";

/**
 * useGoHome — 导航回首页
 * 混淆名: (WorktreeInitPage 中引用)
 *
 * @returns {(options?: { prefillPrompt?: string }) => void}
 *
 * 支持传入 prefillPrompt 参数, 导航到首页后预填输入框
 */
export function useGoHome() {
    const navigate = useNavigate();

    return useCallback(({ prefillPrompt } = {}) => {
        if (prefillPrompt) {
            navigate("/", { state: { prefillPrompt } });
        } else {
            navigate("/");
        }
    }, [navigate]);
}
