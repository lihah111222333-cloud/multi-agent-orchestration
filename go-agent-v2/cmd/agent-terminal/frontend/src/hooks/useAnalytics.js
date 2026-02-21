// =============================================================================
// Codex.app — useAnalytics Hook
// 混淆名: Vo
// 提取自: index-formatted.js (AnalyticsContext 消费)
//
// 功能: 获取 analytics 事件追踪函数
// =============================================================================

import { useContext } from "react";
import { AnalyticsContext } from "../contexts";

/**
 * useAnalytics — 获取 trackEvent 函数
 * 混淆名: Vo()
 *
 * @returns {(event: { eventName: string, metadata?: object }) => void}
 *
 * 用例:
 *   const trackEvent = useAnalytics();
 *   trackEvent({ eventName: "codex_onboarding_step_viewed", metadata: { step: "login" } });
 */
export function useAnalytics() {
    return useContext(AnalyticsContext);
}
