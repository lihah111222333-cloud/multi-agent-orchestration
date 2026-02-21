// =============================================================================
// Codex.app — 工具函数与核心类
// 从 index-formatted.js 多处提取
//
// 功能: 全局共享的工具函数和辅助类
// =============================================================================

import { produce as immerProduce } from "immer";

// ===================== Immer produce =====================
/**
 * 重新导出 Immer produce
 * 在 ConversationManager.updateConversationState() 中使用
 */
export const produce = immerProduce;

// ===================== ID 生成 =====================
/**
 * genId — 生成唯一 ID
 * 混淆名: 在 onNotification 中用于创建 TurnItem 的 id
 *
 * 使用 crypto.randomUUID() (浏览器原生 API)
 */
export function genId() {
    return crypto.randomUUID();
}

// ===================== Thread ID 提取 =====================
/**
 * extractThreadId — 从通知 params 中提取 threadId
 * 混淆名: (AppServerConnection.handleIncomingLine 中引用)
 *
 * @param {Object} params - 通知参数
 * @returns {string|null}
 */
export function extractThreadId(params) {
    if (!params) return null;
    return params.threadId ?? params.thread?.id ?? null;
}

// ===================== Turn 规范化 =====================
/**
 * normalizeTurn — 将后端返回的 turn 对象规范化为前端格式
 * 混淆名: (ConversationManager.resumeConversation 中引用)
 *
 * @param {Object} turn - 后端返回的原始 turn
 * @returns {Turn}
 */
export function normalizeTurn(turn) {
    return {
        params: turn.params ?? null,
        turnId: turn.id ?? turn.turnId ?? null,
        status: turn.status ?? "completed",
        turnStartedAtMs: turn.startedAt ? new Date(turn.startedAt).getTime() : Date.now(),
        finalAssistantStartedAtMs: null,
        error: turn.error ?? null,
        diff: turn.diff ?? null,
        items: (turn.items ?? []).map((item) => ({
            id: item.id ?? genId(),
            ...item,
        })),
    };
}

// ===================== 启动参数增强 =====================
/**
 * enrichStartParams — 增强 thread/start 参数
 * 混淆名: (AppServerConnection.startThread + WorktreeInitPage.createFromEntry 中引用)
 *
 * 添加 feature flags / 默认配置
 *
 * @param {Object} params - 原始参数
 * @param {string[]} enableFeatures - 启用的 feature Flag 列表
 * @returns {Object} 增强后的参数
 */
export function enrichStartParams(params, enableFeatures) {
    return {
        ...params,
        enableFeatures: enableFeatures ?? [],
    };
}

// ===================== Onboarding Phase 计算 =====================
/**
 * computeOnboardingPhase — 根据状态计算当前 onboarding 阶段
 * 混淆名: NEn (RootLayout 中引用)
 *
 * @param {Object} options
 * @param {string} options.windowType - "electron" | "web"
 * @param {Object} options.auth - 认证状态
 * @param {Object} options.workspaceRootsData - workspace 数据
 * @param {boolean} options.workspaceRootsIsLoading
 * @param {Object|null} options.forcedOverride
 * @param {boolean} options.postLoginWelcomePending
 * @param {string} options.pathname - 当前路径
 * @returns {"login"|"welcome"|"workspace"|"app"|null}
 */
export function computeOnboardingPhase({
    windowType,
    auth,
    workspaceRootsData,
    workspaceRootsIsLoading,
    forcedOverride,
    postLoginWelcomePending,
    pathname,
}) {
    // 非 Electron → 直接进入 app
    if (windowType !== "electron") return "app";

    // 强制 override
    if (forcedOverride) return forcedOverride;

    // 未确定 auth → 加载中
    if (auth.requiresAuth === null) return null;

    // 需要登录但未登录
    if (auth.requiresAuth && auth.authMethod == null) return "login";

    // 登录后欢迎页
    if (postLoginWelcomePending) return "welcome";

    // workspace 数据加载中
    if (workspaceRootsIsLoading) return null;

    // 无 workspace → 选择页
    const roots = workspaceRootsData?.roots ?? [];
    if (roots.length === 0) return "workspace";

    // 已就绪
    return "app";
}

// ===================== BatchQueue =====================
/**
 * BatchQueue — 批处理队列 (用于流式文本 delta 渲染)
 * 混淆名: (ConversationManager 中的 frameTextDeltaQueue/outputDeltaQueue)
 *
 * 收集 delta 事件, 使用 requestAnimationFrame 批量应用
 * 避免每个 delta 触发一次 React 重渲染
 */
export class BatchQueue {
    constructor() {
        this.queue = [];
        this.rafId = null;
        this.consumer = null;
    }

    /**
     * 设置消费者函数 (由 ConversationManager 调用)
     * @param {Function} fn - 接收 delta 数组的函数
     */
    setConsumer(fn) {
        this.consumer = fn;
    }

    /**
     * 添加 delta 到队列
     * 如果未调度 RAF, 则调度
     */
    enqueue(delta) {
        this.queue.push(delta);
        if (this.rafId == null && this.consumer) {
            this.rafId = requestAnimationFrame(() => {
                this.flushNow();
            });
        }
    }

    /**
     * 立即刷新队列 (无需等待 RAF)
     * 在 turn/completed 和 item/completed 时调用
     */
    flushNow() {
        if (this.rafId != null) {
            cancelAnimationFrame(this.rafId);
            this.rafId = null;
        }
        if (this.queue.length > 0 && this.consumer) {
            const batch = this.queue.splice(0);
            this.consumer(batch);
        }
    }
}

// ===================== JSON-RPC 类型判断 =====================
/**
 * isResponse / isRequest / isNotification
 * 混淆名: (AppServerConnection.handleIncomingLine 中引用)
 *
 * 根据 JSON-RPC 2.0 规范判断消息类型
 */
export function isResponse(msg) {
    return msg.id != null && (msg.result !== undefined || msg.error !== undefined);
}

export function isRequest(msg) {
    return msg.id != null && msg.method != null;
}

export function isNotification(msg) {
    return msg.id == null && msg.method != null;
}

// ===================== 路径工具 =====================
/**
 * extractLastPathComponent — 提取路径最后一段作为显示名
 * 混淆名: (SelectWorkspacePage 中引用)
 */
export function extractLastPathComponent(path) {
    if (!path) return "";
    const parts = path.replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || path;
}

/**
 * extractWorkspacePath — 从远程任务中提取 workspace 路径
 * 混淆名: (SelectWorkspacePage 中引用)
 */
export function extractWorkspacePath(task) {
    return task?.workspacePath ?? task?.cwd ?? null;
}

// ===================== NUX 常量 =====================
/**
 * NuxKeys — New User Experience 配置键
 * 混淆名: (FirstRunPage 中引用)
 */
export const NuxKeys = {
    NUX_2025_09_15: "nux:2025-09-15",
    NUX_2025_09_15_FULL_CHATGPT_AUTH_VIEWED: "nux:2025-09-15:full-chatgpt-auth-viewed",
    NUX_2025_09_15_APIKEY_AUTH_VIEWED: "nux:2025-09-15:apikey-auth-viewed",
};

/**
 * STEP 常量 — FirstRunPage 引导步骤
 * 混淆名: M6, b4
 */
export const STEP_CHATGPT_INTRO = "chatgpt-intro";
export const STEP_APIKEY_INTRO = "apikey-intro";

/**
 * DOUBLE_LIMITS_PLANS — 拥有双倍限额的计划列表
 * 混淆名: (WelcomePage 中引用)
 */
export const DOUBLE_LIMITS_PLANS = ["plus", "pro", "team", "enterprise"];

// ===================== 设置页 Slugs =====================
/**
 * settingsSlugs — 设置页子路由定义
 * 混淆名: WA (routes.js 中引用)
 */
export const settingsSlugs = [
    { slug: "agent-settings", label: "Agent" },
    { slug: "mcp-settings", label: "MCP Servers" },
    { slug: "git-settings", label: "Git" },
    { slug: "personalization", label: "Personalization" },
    { slug: "local-environments", label: "Local Environments" },
    { slug: "worktrees", label: "Worktrees" },
    { slug: "skills-settings", label: "Skills" },
    { slug: "data-controls", label: "Data Controls" },
];

export const defaultSettingsSlug = "agent-settings";
