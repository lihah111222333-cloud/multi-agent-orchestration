// =============================================================================
// Codex.app — electronBridge 包装层
// 从 index-formatted.js L251300 附近提取
//
// 功能: 为 React 组件提供与 Electron Main 进程通信的统一接口
//       底层使用 window.electronBridge (由 preload.js 注入)
// =============================================================================

/**
 * 获取 electronBridge 实例
 * 由 preload.js 通过 contextBridge.exposeInMainWorld 注入
 */
function getElectronBridge() {
    if (typeof window !== "undefined" && window.electronBridge) {
        return window.electronBridge;
    }
    return null;
}

/**
 * bridge — 全局通信桥接对象
 *
 * 封装 window.electronBridge, 提供类型安全的 API
 * 在 Web 模式下降级为 no-op
 */
export const bridge = {
    /**
     * 发送消息到 Main 进程
     * 底层: electronBridge.sendMessageFromView({ type, ...payload })
     *
     * @param {string} type - 消息类型 (如 "mcp-request-from-renderer")
     * @param {Object} payload - 消息数据
     */
    dispatchMessage(type, payload = {}) {
        const eb = getElectronBridge();
        if (eb) {
            eb.sendMessageFromView({ type, ...payload });
        }
    },

    /**
     * 发送 MCP JSON-RPC 请求
     * type: "mcp-request-from-renderer"
     *
     * @param {Object} request - { id, method, params }
     */
    sendRequest(request) {
        this.dispatchMessage("mcp-request-from-renderer", { request });
    },

    /**
     * 发送 MCP JSON-RPC 通知
     * type: "mcp-notification-from-renderer"
     *
     * @param {Object} notification - { method, params }
     */
    sendNotification(notification) {
        this.dispatchMessage("mcp-notification-from-renderer", { notification });
    },

    /**
     * 发送 MCP JSON-RPC 响应 (如审批决定)
     * type: "mcp-response"
     *
     * @param {Object} response - { id, result }
     */
    sendResponse(response) {
        this.dispatchMessage("mcp-response", { response });
    },

    /**
     * 获取文件路径 (拖拽场景)
     *
     * @param {File} file
     * @returns {string|null}
     */
    getPathForFile(file) {
        const eb = getElectronBridge();
        return eb?.getPathForFile(file) ?? null;
    },

    /**
     * 获取 Sentry 初始化选项
     */
    getSentryInitOptions() {
        const eb = getElectronBridge();
        return eb?.getSentryInitOptions() ?? null;
    },

    /**
     * 获取 App Session ID
     */
    getAppSessionId() {
        const eb = getElectronBridge();
        return eb?.getAppSessionId() ?? null;
    },

    /**
     * 获取 Build Flavor
     * @returns {"prod"|"internal-alpha"|"public-beta"|"dev"|null}
     */
    getBuildFlavor() {
        const eb = getElectronBridge();
        return eb?.getBuildFlavor() ?? null;
    },

    /**
     * 显示上下文菜单
     * @param {Object} options - 菜单选项
     */
    async showContextMenu(options) {
        const eb = getElectronBridge();
        return eb?.showContextMenu(options);
    },

    /**
     * Worker 消息通道
     */
    sendWorkerMessage(workerName, msg) {
        const eb = getElectronBridge();
        if (eb) {
            eb.sendWorkerMessageFromView(workerName, msg);
        }
    },

    subscribeToWorkerMessages(workerName, callback) {
        const eb = getElectronBridge();
        if (eb) {
            return eb.subscribeToWorkerMessages(workerName, callback);
        }
        return () => { };
    },

    /**
     * 判断是否在 Electron 环境
     */
    isElectron() {
        return typeof window !== "undefined" && window.codexWindowType === "electron";
    },
};

export default bridge;
