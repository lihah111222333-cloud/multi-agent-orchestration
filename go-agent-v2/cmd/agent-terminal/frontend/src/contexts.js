// =============================================================================
// Codex.app — React Context 定义
// 从 index-formatted.js 中的 createContext() 调用推导
//
// 功能: 提供全局共享状态的 Context 对象
// =============================================================================

import { createContext } from "react";

// ===================== ConversationManagerContext =====================
/**
 * ConversationManagerContext
 * 混淆名: 在 useConversation.js 中通过 useContext() 引用
 *
 * 提供 ConversationManager 实例给整个 React 树
 * 通过 App.js 中的 Provider 注入
 */
export const ConversationManagerContext = createContext(null);

// ===================== AuthContext =====================
/**
 * AuthContext
 * 混淆名: k1 (useAuth hook 内部)
 *
 * @type {React.Context<AuthState>}
 *
 * AuthState:
 *   authMethod: "chatgpt" | "copilot" | "api-key" | null
 *   userId: string | null
 *   accountId: string | null
 *   email: string | null
 *   requiresAuth: boolean | null
 *   planAtLogin: "free" | "go" | "plus" | "pro" | "team" | "enterprise" | null
 */
export const AuthContext = createContext({
    authMethod: null,
    userId: null,
    accountId: null,
    email: null,
    requiresAuth: null,
    planAtLogin: null,
});

// ===================== AnalyticsContext =====================
/**
 * AnalyticsContext
 * 混淆名: Vo (useAnalytics hook 内部)
 *
 * 提供 trackEvent 函数
 * @type {React.Context<(event: { eventName: string, metadata?: object }) => void>}
 */
export const AnalyticsContext = createContext(() => { });

// ===================== WindowTypeContext =====================
/**
 * WindowTypeContext
 * 混淆名: Sr (useWindowType hook 内部)
 *
 * 区分 Electron 桌面端 vs Web 端
 * @type {React.Context<"electron" | "web">}
 */
export const WindowTypeContext = createContext("web");

// ===================== ToastContext =====================
/**
 * ToastContext
 * 混淆名: (从 useToast 推导)
 *
 * 提供 toast 通知方法
 * @type {React.Context<{ success: Function, danger: Function, info: Function }>}
 */
export const ToastContext = createContext({
    success: () => { },
    danger: () => { },
    info: () => { },
});

// ===================== NuxContext =====================
/**
 * NuxContext — New User Experience 状态
 * 混淆名: XVe (useNuxVariant)
 *
 * @type {React.Context<string|null>}
 */
export const NuxContext = createContext(null);
