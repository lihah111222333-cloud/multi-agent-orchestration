// =============================================================================
// Codex.app — useAppState Hook
// 与 AppStateManager.js 配合使用
//
// 功能: 在 React 组件中订阅应用全局状态
// =============================================================================

import { useState, useEffect } from "react";
import { appStateManager } from "../core/AppStateManager";

/**
 * useAppState — 订阅应用全局状态事件
 *
 * @param {string} eventName - 事件名
 * @param {*} initialValue - 初始值
 * @returns {*} 最新状态值
 *
 * @example
 *   const isFullscreen = useAppState("window-fullscreen-changed", false);
 */
export function useAppState(eventName, initialValue) {
    const [value, setValue] = useState(initialValue);

    useEffect(() => {
        return appStateManager.on(eventName, (data) => setValue(data));
    }, [eventName]);

    return value;
}

/**
 * useWindowFocus — 窗口焦点状态
 * @returns {boolean}
 */
export function useWindowFocus() {
    const [focused, setFocused] = useState(appStateManager.windowFocused);

    useEffect(() => {
        return appStateManager.on("electron-window-focus-changed", ({ focused: f }) => setFocused(f));
    }, []);

    return focused;
}

/**
 * useWindowFullscreen — 窗口全屏状态
 * @returns {boolean}
 */
export function useWindowFullscreen() {
    const [fullscreen, setFullscreen] = useState(appStateManager.windowFullscreen);

    useEffect(() => {
        return appStateManager.on("window-fullscreen-changed", ({ isFullscreen }) => setFullscreen(isFullscreen));
    }, []);

    return fullscreen;
}

/**
 * useAppUpdateReady — 应用更新就绪状态
 * @returns {boolean}
 */
export function useAppUpdateReady() {
    const [ready, setReady] = useState(appStateManager.appUpdateReady);

    useEffect(() => {
        return appStateManager.on("app-update-ready-changed", ({ ready: r }) => setReady(r));
    }, []);

    return ready;
}
