// =============================================================================
// Codex.app — 应用状态管理器 (AppStateManager)
// 从 MessageDispatcher.js 中未处理的剩余消息推导
//
// 功能: 管理应用全局状态事件
//   - 窗口焦点/全屏状态
//   - 持久化 atom 同步
//   - Workspace 根目录更新
//   - 应用更新状态
//   - 追踪录制状态
//   - Copilot API 可用性
//   - 自定义 prompt 更新
// =============================================================================

import { uiEventBus } from "./UIEventBus";

/**
 * AppStateManager — 应用全局状态管理
 *
 * 管理不属于 ConversationManager / TerminalManager / UIEventBus 的应用状态
 */
class AppStateManager {
    constructor() {
        this.windowFocused = true;
        this.windowFullscreen = false;
        this.appUpdateReady = false;
        this.isTraceRecording = false;
        this.isCopilotApiAvailable = false;
        this.persistedAtoms = new Map();
        this.listeners = new Map();
    }

    // ======================== 消息处理 (被 MessageDispatcher 调用) ========================

    /**
     * 窗口全屏状态变化
     * @param {Object} msg - { isFullscreen }
     */
    handleWindowFullscreenChanged(msg) {
        this.windowFullscreen = msg.isFullscreen ?? false;
        this._notify("window-fullscreen-changed", { isFullscreen: this.windowFullscreen });
    }

    /**
     * Electron 窗口焦点变化
     * @param {Object} msg - { focused }
     */
    handleWindowFocusChanged(msg) {
        this.windowFocused = msg.focused ?? true;
        this._notify("electron-window-focus-changed", { focused: this.windowFocused });
    }

    /**
     * 自定义 prompt 更新
     * @param {Object} msg - { prompts }
     */
    handleCustomPromptsUpdated(msg) {
        this._notify("custom-prompts-updated", msg);
    }

    /**
     * 持久化 atom 同步 (jotai 全窗口同步)
     * @param {Object} msg - { key, value }
     */
    handlePersistedAtomSync(msg) {
        if (msg.key) {
            this.persistedAtoms.set(msg.key, msg.value);
        }
        this._notify("persisted-atom-sync", msg);
    }

    /**
     * 持久化 atom 更新
     * @param {Object} msg - { key, value }
     */
    handlePersistedAtomUpdated(msg) {
        if (msg.key) {
            this.persistedAtoms.set(msg.key, msg.value);
        }
        this._notify("persisted-atom-updated", msg);
    }

    /**
     * 追踪录制状态变化
     * @param {Object} msg - { isRecording }
     */
    handleTraceRecordingStateChanged(msg) {
        this.isTraceRecording = msg.isRecording ?? false;
        this._notify("trace-recording-state-changed", { isRecording: this.isTraceRecording });
    }

    /**
     * 追踪录制已上传
     * @param {Object} msg - { traceId, url }
     */
    handleTraceRecordingUploaded(msg) {
        this._notify("trace-recording-uploaded", msg);
    }

    /**
     * Copilot API 可用性变化
     * @param {Object} msg - { isAvailable }
     */
    handleCopilotApiAvailableUpdated(msg) {
        this.isCopilotApiAvailable = msg.isAvailable ?? false;
        this._notify("is-copilot-api-available-updated", { isAvailable: this.isCopilotApiAvailable });
    }

    /**
     * 应用更新就绪状态变化
     * @param {Object} msg - { ready }
     */
    handleAppUpdateReadyChanged(msg) {
        this.appUpdateReady = msg.ready ?? false;
        this._notify("app-update-ready-changed", { ready: this.appUpdateReady });
    }

    /**
     * 实现 TODO (从菜单触发)
     * @param {Object} msg - { todoId, conversationId }
     */
    handleImplementTodo(msg) {
        // 转发给 UI 事件总线, 由 ChatPage 等组件处理
        uiEventBus.emit("implement-todo", msg);
    }

    /**
     * 添加上下文文件 (从菜单触发)
     * @param {Object} msg - { filePath }
     */
    handleAddContextFile(msg) {
        uiEventBus.emit("add-context-file", msg);
    }

    /**
     * 打开当前线程浮窗
     * @param {Object} msg
     */
    handleThreadOverlayOpenCurrent(msg) {
        uiEventBus.emit("thread-overlay-open-current", msg);
    }

    /**
     * 切换 React Query DevTools
     * @param {Object} msg
     */
    handleToggleQueryDevtools(msg) {
        uiEventBus.emit("toggle-query-devtools", msg);
    }

    /**
     * Workspace 根选项更新
     * @param {Object} msg
     * @param {import("@tanstack/react-query").QueryClient} queryClient
     */
    handleWorkspaceRootOptionsUpdated(msg, queryClient) {
        queryClient.invalidateQueries({ queryKey: ["workspace-root-options"] });
        this._notify("workspace-root-options-updated", msg);
    }

    /**
     * 活跃 workspace 根更新
     * @param {Object} msg
     * @param {import("@tanstack/react-query").QueryClient} queryClient
     */
    handleActiveWorkspaceRootsUpdated(msg, queryClient) {
        queryClient.invalidateQueries({ queryKey: ["workspace-roots"] });
        this._notify("active-workspace-roots-updated", msg);
    }

    // ======================== 订阅接口 ========================

    /**
     * 订阅状态变化
     * @param {string} event
     * @param {Function} handler
     * @returns {Function} unsubscribe
     */
    on(event, handler) {
        let set = this.listeners.get(event);
        if (!set) {
            set = new Set();
            this.listeners.set(event, set);
        }
        set.add(handler);
        return () => set.delete(handler);
    }

    /** @private */
    _notify(event, data) {
        const set = this.listeners.get(event);
        if (set) set.forEach((cb) => cb(data));
    }
}

// 单例实例
export const appStateManager = new AppStateManager();
export default appStateManager;
