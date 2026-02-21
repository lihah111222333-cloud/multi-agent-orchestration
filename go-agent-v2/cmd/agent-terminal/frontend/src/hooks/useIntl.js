// =============================================================================
// Codex.app — useIntl Hook + 轻量 Atom 系统 + NUX
// 从 WorktreeInitPage / WelcomePage / SelectWorkspacePage 中的 i18n 使用推导
//
// 功能:
//   - useIntl() — 国际化 (支持参数插值)
//   - useSetAtom() / useAtomValue() — 轻量状态原子 (替代 jotai)
//   - useNuxVariant() — NUX 引导变体
//   - postLoginWelcomeAtom — 登录后欢迎页状态
// =============================================================================

import { useState, useEffect, useCallback } from "react";
import { useAuth } from "./useAuth";

// ===================== 轻量 Atom 系统 (替代 jotai) =====================

/**
 * AtomStore — 轻量发布/订阅状态管理
 * 替代 jotai 的跨组件状态共享
 */
const atomStore = {
    /** @type {Map<string, any>} */
    values: new Map(),
    /** @type {Map<string, Set<Function>>} */
    listeners: new Map(),

    get(atom) {
        if (this.values.has(atom.key)) return this.values.get(atom.key);
        return atom.default;
    },

    set(atom, value) {
        this.values.set(atom.key, value);
        const listeners = this.listeners.get(atom.key);
        if (listeners) listeners.forEach((cb) => cb(value));
    },

    subscribe(atom, callback) {
        let set = this.listeners.get(atom.key);
        if (!set) {
            set = new Set();
            this.listeners.set(atom.key, set);
        }
        set.add(callback);
        return () => set.delete(callback);
    },
};

// ===================== useIntl =====================

/**
 * useIntl — 获取国际化工具
 * 混淆名: (WorktreeInitPage, SelectWorkspacePage 中引用)
 *
 * 支持参数插值: formatMessage({ defaultMessage: "Hello {name}" }, { name: "World" })
 * 原始实现使用 react-intl, 此处为兼容实现
 *
 * @returns {{ formatMessage: Function }}
 */
export function useIntl() {
    const formatMessage = useCallback(({ id, defaultMessage }, values) => {
        let message = defaultMessage ?? id ?? "";
        if (values && typeof values === "object") {
            for (const [key, val] of Object.entries(values)) {
                message = message.replace(new RegExp(`\\{${key}\\}`, "g"), String(val));
            }
        }
        return message;
    }, []);

    return { formatMessage };
}

// ===================== useSetAtom =====================

/**
 * useSetAtom — 获取 atom setter (类 jotai API)
 * 混淆名: (WelcomePage 中引用的 useSetAtom)
 *
 * @param {Object} atom - { key: string, default: any }
 * @returns {Function} setter - (value | updater) => void
 */
export function useSetAtom(atom) {
    return useCallback((valueOrUpdater) => {
        if (typeof valueOrUpdater === "function") {
            const current = atomStore.get(atom);
            atomStore.set(atom, valueOrUpdater(current));
        } else {
            atomStore.set(atom, valueOrUpdater);
        }
    }, [atom]);
}

// ===================== useAtomValue =====================

/**
 * useAtomValue — 订阅 atom 值 (类 jotai API)
 * 混淆名: (WelcomePage 中引用)
 *
 * @param {Object} atom - { key: string, default: any }
 * @returns {any} 当前值
 */
export function useAtomValue(atom) {
    const [value, setValue] = useState(() => atomStore.get(atom));

    useEffect(() => {
        // 同步初始值 (可能在其他组件中已更新)
        setValue(atomStore.get(atom));
        return atomStore.subscribe(atom, setValue);
    }, [atom]);

    return value;
}

// ===================== useNuxVariant =====================

/**
 * useNuxVariant — 获取 NUX 引导变体
 * 混淆名: XVe
 *
 * 根据认证状态决定 NUX 变体:
 *   - 已认证 → "nux:2025-09-15" (显示完整引导)
 *   - 未认证 → null (跳过引导)
 *
 * 原始实现通过 statsig feature flag 控制, 此处基于 auth 状态简化
 *
 * @returns {string|null}
 */
export function useNuxVariant() {
    const auth = useAuth();

    // 已登录且 requiresAuth 已确定 → 返回 NUX 变体
    if (auth.authMethod != null) {
        return "nux:2025-09-15";
    }

    // 不需要 auth (如 API key 模式) 也显示引导
    if (auth.requiresAuth === false) {
        return "nux:2025-09-15";
    }

    return null;
}

// ===================== Atoms =====================

/**
 * postLoginWelcomeAtom — 登录后是否需要显示欢迎页
 * 混淆名: (WelcomePage / RootLayout 中引用)
 */
export const postLoginWelcomeAtom = { key: "postLoginWelcome", default: false };
