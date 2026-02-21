// =============================================================================
// Codex.app — React 根组件 (App)
// 从 index-formatted.js L248000 附近推导 (React DOM 挂载点)
//
// 功能: 应用根组件
//   - 初始化 ConversationManager
//   - 挂载 Provider 树 (Context / React Query / Router / I18n)
//   - 设置全局消息分发器
//   - 渲染路由
//   - Toast 通知 UI
// =============================================================================

import React, { useState, useEffect, useMemo, useCallback, useRef } from "react";
import { BrowserRouter, useNavigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { ConversationManagerContext, AuthContext, AnalyticsContext, WindowTypeContext, ToastContext } from "./contexts";
import { ConversationManager } from "./core/ConversationManager";
import { setupMessageDispatcher } from "./core/MessageDispatcher";
import { routes } from "./routes";
import { bridge } from "./bridge";

// ===================== QueryClient =====================
const queryClient = new QueryClient({
    defaultOptions: {
        queries: {
            staleTime: 30_000, // 30s
            refetchOnWindowFocus: false,
        },
    },
});

// ===================== Toast 通知系统 =====================

/** Toast 消息类型 */
const TOAST_VARIANTS = {
    success: {
        bgClass: "bg-green-500",
        icon: "✓",
    },
    danger: {
        bgClass: "bg-red-500",
        icon: "✕",
    },
    info: {
        bgClass: "bg-token-primary",
        icon: "ℹ",
    },
};

/**
 * ToastProvider — Toast 通知容器
 * 管理 Toast 消息的生命周期和渲染
 */
function ToastProvider({ children }) {
    const [toasts, setToasts] = useState([]);
    const nextIdRef = useRef(0);

    const addToast = useCallback((variant, message) => {
        const id = nextIdRef.current++;
        setToasts((prev) => [...prev, { id, variant, message }]);
        // 自动消失 (3 秒)
        setTimeout(() => {
            setToasts((prev) => prev.filter((t) => t.id !== id));
        }, 3000);
    }, []);

    const toast = useMemo(() => ({
        success: (msg) => addToast("success", msg),
        danger: (msg) => addToast("danger", msg),
        info: (msg) => addToast("info", msg),
    }), [addToast]);

    return (
        <ToastContext.Provider value={toast}>
            {children}
            {/* Toast 渲染区域 — 右上角定位 */}
            {toasts.length > 0 && (
                <div
                    style={{
                        position: "fixed",
                        top: "var(--h-toolbar, 44px)",
                        right: "16px",
                        zIndex: 9999,
                        display: "flex",
                        flexDirection: "column",
                        gap: "8px",
                        pointerEvents: "none",
                    }}
                >
                    {toasts.map((t) => {
                        const variant = TOAST_VARIANTS[t.variant] ?? TOAST_VARIANTS.info;
                        return (
                            <div
                                key={t.id}
                                style={{
                                    display: "flex",
                                    alignItems: "center",
                                    gap: "8px",
                                    padding: "10px 16px",
                                    borderRadius: "8px",
                                    color: "#fff",
                                    fontSize: "13px",
                                    lineHeight: "1.4",
                                    boxShadow: "0 4px 12px rgba(0,0,0,0.3)",
                                    pointerEvents: "auto",
                                    animation: "toast-slide-in 200ms ease-out",
                                    maxWidth: "360px",
                                    wordBreak: "break-word",
                                    backgroundColor: t.variant === "success" ? "#22c55e"
                                        : t.variant === "danger" ? "#ef4444"
                                            : "var(--token-primary, #6366f1)",
                                }}
                            >
                                <span style={{ flexShrink: 0, fontSize: "14px" }}>{variant.icon}</span>
                                <span>{t.message}</span>
                            </div>
                        );
                    })}
                </div>
            )}
            {/* Toast 动画 — 内联 keyframes */}
            <style>{`
                @keyframes toast-slide-in {
                    from { transform: translateX(100%); opacity: 0; }
                    to { transform: translateX(0); opacity: 1; }
                }
            `}</style>
        </ToastContext.Provider>
    );
}

// ===================== App 根组件 =====================
/**
 * App — React 应用根组件
 *
 * Provider 嵌套顺序 (从外到内):
 *   QueryClientProvider
 *   └── WindowTypeContext.Provider
 *       └── AuthContext.Provider
 *           └── AnalyticsContext.Provider
 *               └── ToastProvider
 *                   └── ConversationManagerContext.Provider
 *                       └── BrowserRouter
 *                           └── AppRoutes (含 MessageDispatcher 初始化)
 */
export function App() {
    // 初始化 ConversationManager (单例)
    const conversationManager = useMemo(() => {
        const cm = new ConversationManager();
        // 设置 delta 队列消费者
        cm.frameTextDeltaQueue.setConsumer((deltas) => cm.applyFrameTextDeltas(deltas));
        cm.outputDeltaQueue.setConsumer((deltas) => cm.applyOutputDeltas?.(deltas));
        return cm;
    }, []);

    // 窗口类型 (electron / web)
    const windowType = typeof window !== "undefined" && window.codexWindowType === "electron"
        ? "electron" : "web";

    // Auth 状态 (由 account/updated 通知更新)
    const [authState, setAuthState] = useState({
        authMethod: null,
        userId: null,
        accountId: null,
        email: null,
        requiresAuth: null,
        planAtLogin: null,
    });

    useEffect(() => {
        conversationManager.addAuthStatusCallback((status) => {
            setAuthState((prev) => ({ ...prev, authMethod: status.authMethod }));
        });
    }, [conversationManager]);

    // Analytics
    const trackEvent = useMemo(() => {
        return ({ eventName, metadata }) => {
            bridge.dispatchMessage("telemetry-track-event", { eventName, metadata });
        };
    }, []);

    return (
        <QueryClientProvider client={queryClient}>
            <WindowTypeContext.Provider value={windowType}>
                <AuthContext.Provider value={authState}>
                    <AnalyticsContext.Provider value={trackEvent}>
                        <ToastProvider>
                            <ConversationManagerContext.Provider value={conversationManager}>
                                <BrowserRouter>
                                    <AppRoutes
                                        conversationManager={conversationManager}
                                        queryClient={queryClient}
                                    />
                                </BrowserRouter>
                            </ConversationManagerContext.Provider>
                        </ToastProvider>
                    </AnalyticsContext.Provider>
                </AuthContext.Provider>
            </WindowTypeContext.Provider>
        </QueryClientProvider>
    );
}

// ===================== AppRoutes (初始化 MessageDispatcher) =====================
/**
 * AppRoutes — 在 Router 内部初始化消息分发器并渲染路由
 *
 * 必须在 BrowserRouter 内部, 因为需要 useNavigate()
 */
function AppRoutes({ conversationManager, queryClient }) {
    const navigate = useNavigate();

    useEffect(() => {
        setupMessageDispatcher(conversationManager, navigate, queryClient);
    }, [conversationManager, navigate, queryClient]);

    return <>{routes}</>;
}

export default App;
