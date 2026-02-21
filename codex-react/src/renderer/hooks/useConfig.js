// =============================================================================
// Codex.app — useConfig Hooks
// 从 index-formatted.js 中的 config/read 调用推导
//
// 功能: 读写 Codex 配置 (通过 MCP JSON-RPC config/read + config/write)
// =============================================================================

import { useState, useEffect, useCallback } from "react";
import { bridge } from "../bridge";

/**
 * useConfigValue — 读取配置值
 * 混淆名: (SelectWorkspacePage 中引用)
 *
 * @param {string} key - 配置键 (如 "host_config", "approval_policy")
 * @returns {[any, boolean]} - [value, isLoading]
 *
 * 内部通过 config/read JSON-RPC 获取
 */
export function useConfigValue(key) {
    const [value, setValue] = useState(null);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const id = `config/read:${key}:${crypto.randomUUID()}`;

        const handler = (event) => {
            const msg = event.data;
            if (msg?.type === "mcp-response" && msg.message?.id === id) {
                window.removeEventListener("message", handler);
                if (!msg.message.error) {
                    setValue(msg.message.result?.value ?? null);
                }
                setIsLoading(false);
            }
        };
        window.addEventListener("message", handler);
        bridge.sendRequest({ id, method: "config/read", params: { key } });

        return () => window.removeEventListener("message", handler);
    }, [key]);

    return [value, isLoading];
}

/**
 * useConfigData — 读/写配置值
 * 混淆名: (FirstRunPage 中引用)
 *
 * @param {string} key - 配置键
 * @returns {{ data: any, isLoading: boolean, setData: (value: any) => Promise<void> }}
 */
export function useConfigData(key) {
    const [value, isLoading] = useConfigValue(key);

    const setData = useCallback(async (newValue) => {
        const id = `config/write:${key}:${crypto.randomUUID()}`;
        bridge.sendRequest({ id, method: "config/write", params: { key, value: newValue } });
    }, [key]);

    return { data: value, isLoading, setData };
}
