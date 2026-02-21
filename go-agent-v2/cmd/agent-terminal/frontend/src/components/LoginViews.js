// =============================================================================
// Codex.app — LoginPage 子组件
// 混淆名: tMn (Electron), $En (Web)
// 提取自: index-formatted.js L320952 附近
//
// 功能: 登录视图
//   - ElectronLoginPage: ChatGPT OAuth / API Key 登录
//   - WebLoginPage: 内联表单登录
// =============================================================================

import { useState, useCallback } from "react";
import { Button } from "./Button";
import { bridge } from "../bridge";

/**
 * ElectronLoginPage — Electron 模式登录
 * 混淆名: tMn
 *
 * 登录方式:
 *   1. ChatGPT OAuth — 打开外部浏览器, 通过 OAuth 回调完成认证
 *   2. API Key — 本地输入 API Key
 */
export function ElectronLoginPage() {
    const [mode, setMode] = useState("choose"); // "choose" | "oauth" | "apikey"
    const [apiKey, setApiKey] = useState("");
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState(null);

    const handleOAuthLogin = useCallback(() => {
        setMode("oauth");
        setIsLoading(true);
        // 通过 IPC 触发 OAuth 流程 (打开外部浏览器)
        bridge.dispatchMessage("auth-start-oauth", {});
        // 等待 Main 进程回调
    }, []);

    const handleApiKeySubmit = useCallback(async () => {
        if (!apiKey.trim()) return;
        setIsLoading(true);
        setError(null);
        try {
            bridge.dispatchMessage("auth-submit-api-key", { apiKey: apiKey.trim() });
        } catch (err) {
            setError(err.message);
            setIsLoading(false);
        }
    }, [apiKey]);

    if (mode === "choose") {
        return (
            <div className="h-full flex items-center justify-center">
                <div className="flex flex-col items-center gap-6 max-w-[360px] px-6">
                    <div className="text-[32px]">🔑</div>
                    <h1 className="text-[24px] font-semibold text-token-foreground">Sign in to Codex</h1>
                    <div className="flex flex-col gap-3 w-full">
                        <Button color="primary" onClick={handleOAuthLogin} className="w-full justify-center">
                            Sign in with ChatGPT
                        </Button>
                        <Button color="secondary" onClick={() => setMode("apikey")} className="w-full justify-center">
                            Use API Key
                        </Button>
                    </div>
                </div>
            </div>
        );
    }

    if (mode === "apikey") {
        return (
            <div className="h-full flex items-center justify-center">
                <div className="flex flex-col items-center gap-6 max-w-[360px] px-6">
                    <h1 className="text-[24px] font-semibold text-token-foreground">Enter API Key</h1>
                    <input
                        type="password"
                        className="w-full bg-token-bg-secondary border border-token-border rounded-md px-3 py-2 text-sm text-token-foreground"
                        placeholder="sk-..."
                        value={apiKey}
                        onChange={(e) => setApiKey(e.target.value)}
                        onKeyDown={(e) => e.key === "Enter" && handleApiKeySubmit()}
                    />
                    {error && <div className="text-sm text-token-error-foreground">{error}</div>}
                    <div className="flex gap-3 w-full">
                        <Button color="ghost" onClick={() => setMode("choose")} className="flex-1 justify-center">
                            Back
                        </Button>
                        <Button color="primary" onClick={handleApiKeySubmit} loading={isLoading} className="flex-1 justify-center">
                            Submit
                        </Button>
                    </div>
                </div>
            </div>
        );
    }

    // OAuth pending
    return (
        <div className="h-full flex items-center justify-center">
            <div className="flex flex-col items-center gap-4 max-w-[360px] px-6 text-center">
                <div className="animate-spin w-8 h-8 border-2 border-token-primary border-t-transparent rounded-full" />
                <p className="text-sm text-token-description-foreground">
                    Waiting for browser login to complete...
                </p>
                <Button color="ghost" size="sm" onClick={() => { setMode("choose"); setIsLoading(false); }}>
                    Cancel
                </Button>
            </div>
        </div>
    );
}

/**
 * WebLoginPage — Web 模式登录
 * 混淆名: $En
 */
export function WebLoginPage() {
    return (
        <div className="h-full flex items-center justify-center">
            <div className="flex flex-col items-center gap-6 max-w-[360px] px-6 text-center">
                <h1 className="text-[24px] font-semibold text-token-foreground">Sign in to Codex</h1>
                <p className="text-sm text-token-description-foreground">
                    Web login is handled through the parent application.
                </p>
            </div>
        </div>
    );
}
