// =============================================================================
// Codex.app — logger 日志工具
// 从 DebugPage 中的 logger.error() 调用推导
//
// 功能:
//   - 结构化日志 (safe/sensitive 分离)
//   - 时间戳前缀
//   - Dev 模式显示 sensitive 数据
//   - 日志分发到 Main 进程 (通过 bridge)
// =============================================================================

import { bridge } from "./bridge";

const isDev = typeof window !== "undefined" &&
    (window.location?.hostname === "localhost" || navigator.userAgent?.includes("Electron"));

/**
 * formatTimestamp — 日志时间戳
 * @returns {string} HH:MM:SS.mmm
 */
function formatTimestamp() {
    const now = new Date();
    return `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}:${String(now.getSeconds()).padStart(2, "0")}.${String(now.getMilliseconds()).padStart(3, "0")}`;
}

/**
 * formatContext — 格式化日志上下文
 * @param {Object} context - { safe?: Object, sensitive?: Object }
 * @returns {Object|undefined}
 */
function formatContext(context) {
    if (!context) return undefined;
    const result = {};
    if (context.safe) Object.assign(result, context.safe);
    // sensitive 数据仅在开发模式输出
    if (isDev && context.sensitive) result.__sensitive = context.sensitive;
    return Object.keys(result).length > 0 ? result : undefined;
}

/**
 * logToMain — 将重要日志发送到 Main 进程
 * Main 进程可将其写入 Sentry breadcrumb 或 Datadog
 */
function logToMain(level, message, context) {
    try {
        bridge.dispatchMessage("renderer-log", {
            level,
            message,
            timestamp: Date.now(),
            context: context?.safe,
        });
    } catch {
        // 静默: bridge 不可用时忽略
    }
}

/**
 * logger — 应用日志工具
 *
 * 原始实现使用 Sentry breadcrumb + Datadog
 * 此实现: console + 时间戳 + safe/sensitive 分离 + Main 进程转发
 */
export const logger = {
    debug(message, context) {
        if (isDev) {
            console.debug(`[Codex ${formatTimestamp()}] ${message}`, formatContext(context));
        }
    },
    info(message, context) {
        console.info(`[Codex ${formatTimestamp()}] ${message}`, formatContext(context));
    },
    warn(message, context) {
        console.warn(`[Codex ${formatTimestamp()}] ${message}`, formatContext(context));
        logToMain("warn", message, context);
    },
    warning(message, context) {
        // 别名, 兼容 main.js 中的 logger.warning() 调用
        this.warn(message, context);
    },
    error(message, context) {
        console.error(`[Codex ${formatTimestamp()}] ${message}`, formatContext(context));
        logToMain("error", message, context);
    },
};

export default logger;
