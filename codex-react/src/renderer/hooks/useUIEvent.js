// =============================================================================
// Codex.app — useUIEvent Hook
// 与 UIEventBus.js 配合使用
//
// 功能: 在 React 组件中订阅 UIEventBus 事件
// =============================================================================

import { useEffect, useCallback } from "react";
import { uiEventBus, UIEvents } from "../core/UIEventBus";

/**
 * useUIEvent — 订阅一个 UI 事件
 *
 * @param {string} eventName - UIEvents 中的事件名
 * @param {Function} handler - 事件处理函数
 *
 * @example
 *   useUIEvent(UIEvents.TOGGLE_SIDEBAR, () => setSidebarOpen(prev => !prev));
 */
export function useUIEvent(eventName, handler) {
    const stableHandler = useCallback(handler, [handler]);

    useEffect(() => {
        return uiEventBus.on(eventName, stableHandler);
    }, [eventName, stableHandler]);
}

/**
 * useUIEventEmitter — 获取 UIEventBus 的 emit 能力
 *
 * @returns {(event: string, data?: Object) => void}
 */
export function useUIEventEmitter() {
    return useCallback((event, data) => uiEventBus.emit(event, data), []);
}

// 重新导出事件常量方便使用
export { UIEvents };
