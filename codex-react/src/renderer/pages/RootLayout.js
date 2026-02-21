// =============================================================================
// Codex.app — 根布局 (RootLayout)
// 混淆名: PEn
// 路由: (顶层 layout, 包裹所有需要布局的页面)
// 提取自: index-formatted.js L318182
//
// 功能: 全局路由守卫 + 窗口模式管理
//   - 根据认证状态和 workspace 选择状态, 决定 onboarding 流程
//   - 控制 Electron 窗口模式 (app / onboarding)
//   - 跟踪 onboarding 步骤的 analytics
// =============================================================================

import { useState, useEffect } from "react";
import { Outlet, Navigate, useLocation } from "react-router-dom";
import { useAuth } from "../hooks/useAuth";
import { useWindowType } from "../hooks/useWindowType";
import { useAnalytics } from "../hooks/useAnalytics";
import { useQuery } from "../hooks/useAppQuery";
import { computeOnboardingPhase } from "../utils";
import { bridge } from "../bridge";
import { appStateManager } from "../core/AppStateManager";

/**
 * RootLayout — 顶层路由布局
 *
 * 核心逻辑:
 *   1. 检查 auth 状态、workspace 配置、强制 override
 *   2. 计算当前应处于的 phase: "login" | "welcome" | "workspace" | "app"
 *   3. 如果用户在错误页面, 自动重定向
 *   4. 通知 Electron 切换窗口模式 (app vs onboarding)
 *   5. 通过 Outlet 渲染子路由
 *
 * 依赖:
 *   - useAuth() (k1) — 认证状态
 *   - useLocation() (To) — 当前路径
 *   - useQuery("workspace-root-options") — workspace 列表
 *   - useAnalytics() (Vo) — analytics 事件
 *   - useWindowType() (Sr) — electron 或 web
 */
function RootLayout() {
    const windowType = useWindowType(); // Sr()
    const location = useLocation();     // To()
    const auth = useAuth();             // k1()
    const trackEvent = useAnalytics();  // Vo()

    const isElectron = windowType === "electron";

    // autoLoginState: 跟踪自动登录状态
    // 原版通过 jotai atom + persisted-atom-sync 管理
    // 这里通过 AppStateManager 的 persisted-atom-sync 事件监听
    const [autoLoginState, setAutoLoginState] = useState(() => {
        // 初始值: 如果 auth 已有方法则非 "auto"
        return auth.authMethod != null ? "done" : "auto";
    });

    // postLoginWelcomePending: 登录后是否需要显示欢迎页
    const [postLoginWelcomePending, setPostLoginWelcomePending] = useState(false);

    useEffect(() => {
        // 监听 persisted atom 同步 (跨窗口 jotai 同步)
        const unsub1 = appStateManager.on("persisted-atom-sync", (msg) => {
            if (msg.key === "autoLoginState") setAutoLoginState(msg.value ?? "auto");
            if (msg.key === "postLoginWelcome") setPostLoginWelcomePending(!!msg.value);
        });
        const unsub2 = appStateManager.on("persisted-atom-updated", (msg) => {
            if (msg.key === "autoLoginState") setAutoLoginState(msg.value ?? "auto");
            if (msg.key === "postLoginWelcome") setPostLoginWelcomePending(!!msg.value);
        });
        return () => { unsub1(); unsub2(); };
    }, []);

    // 当 auth 方法确认后, 自动更新 autoLoginState
    useEffect(() => {
        if (auth.authMethod != null && autoLoginState === "auto") {
            setAutoLoginState("done");
        }
    }, [auth.authMethod, autoLoginState]);

    // 获取 workspace 列表 (仅 Electron 且 auth 就绪)
    const shouldFetchWorkspaces = isElectron &&
        (autoLoginState !== "auto" || auth.authMethod != null || auth.requiresAuth === false);
    const { data: workspaceData, isLoading: workspaceLoading } = useQuery("workspace-root-options", {
        queryConfig: { enabled: shouldFetchWorkspaces },
    });

    // 计算当前 phase
    const phase = computeOnboardingPhase({
        windowType,
        auth,
        workspaceRootsData: workspaceData,
        workspaceRootsIsLoading: workspaceLoading,
        forcedOverride: null,
        postLoginWelcomePending,
        pathname: location.pathname,
    });

    // 切换 Electron 窗口模式
    useEffect(() => {
        if (!isElectron || !phase) return;
        const mode = phase === "app" ? "app" : "onboarding";
        bridge.dispatchMessage("electron-set-window-mode", { mode });
    }, [phase, isElectron]);

    // 跟踪 onboarding 步骤
    useEffect(() => {
        if (!isElectron) return;
        const step = mapPathnameToOnboardingStep(location.pathname);
        if (step) {
            trackEvent({
                eventName: "codex_onboarding_step_viewed",
                metadata: { step },
            });
        }
    }, [location.pathname, trackEvent, isElectron]);

    // 加载中
    if (!phase) return <></>;

    // Electron 模式下的重定向逻辑
    const isOnboardingPage = ["/login", "/welcome", "/select-workspace"].includes(location.pathname);
    if (isElectron) {
        if (phase === "login" && location.pathname !== "/login") {
            return <Navigate to="/login" replace />;
        }
        if (phase === "welcome" && location.pathname !== "/welcome") {
            return <Navigate to="/welcome" replace />;
        }
        if (phase === "workspace" && location.pathname !== "/select-workspace") {
            return <Navigate to="/select-workspace" replace />;
        }
        if (phase === "app" && isOnboardingPage) {
            return <Navigate to="/" replace />;
        }
    }

    // 渲染子路由
    return <Outlet />;
}

/**
 * mapPathnameToOnboardingStep — 路径 → onboarding 步骤名
 * OEn()
 */
function mapPathnameToOnboardingStep(pathname) {
    if (pathname === "/login") return "login";
    if (pathname === "/welcome") return "welcome";
    if (pathname === "/select-workspace") return "workspace";
    return null;
}

export { RootLayout };
