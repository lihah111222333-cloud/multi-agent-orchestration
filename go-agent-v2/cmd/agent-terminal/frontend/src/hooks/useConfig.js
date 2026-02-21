// =============================================================================
// Codex.app — useConfig Hooks
// 从 index-formatted.js 中的 config/read 调用推导
//
// 功能: 读写 Codex 配置 (通过 bridge.callAPI → Go config/read + config/write)
//
// 适配: Wails Go 绑定 — 直接 async/await, 不依赖 window.message 回调
// =============================================================================

import { useState, useEffect, useCallback } from "react";
import { bridge } from "../bridge";

/**
 * useConfigValue — 读取配置值
 *
 * @param {string} key - 配置键 (如 "host_config", "approval_policy")
 * @returns {[any, boolean]} - [value, isLoading]
 *
 * 内部通过 bridge.callAPI("config/read") 获取
 */
export function useConfigValue(key) {
    const [value, setValue] = useState(null);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        let cancelled = false;

        async function fetchConfig() {
            try {
                const result = await bridge.callAPI("config/read", { key });
                if (!cancelled) {
                    setValue(result?.value ?? result ?? null);
                    setIsLoading(false);
                }
            } catch (err) {
                console.warn("[useConfigValue] config/read failed:", key, err);
                if (!cancelled) {
                    setIsLoading(false);
                }
            }
        }

        fetchConfig();
        return () => { cancelled = true; };
    }, [key]);

    return [value, isLoading];
}

/**
 * useConfigData — 读/写配置值
 *
 * @param {string} key - 配置键
 * @returns {{ data: any, isLoading: boolean, setData: (value: any) => Promise<void> }}
 */
export function useConfigData(key) {
    const [value, isLoading] = useConfigValue(key);

    const setData = useCallback(async (newValue) => {
        try {
            await bridge.callAPI("config/value/write", { key, value: newValue });
        } catch (err) {
            console.warn("[useConfigData] config/write failed:", key, err);
        }
    }, [key]);

    return { data: value, isLoading, setData };
}
