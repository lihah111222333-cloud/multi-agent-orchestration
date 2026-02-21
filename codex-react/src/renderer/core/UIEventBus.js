// =============================================================================
// Codex.app — UI 事件总线 (UIEventBus)
// 从 MessageDispatcher.js 中未处理的 UI 控制消息推导
//
// 功能: 接收 Main 进程的 UI 控制指令, 通过发布/订阅分发给 React 组件
//   - 命令面板 (Cmd+K)
//   - 侧边栏切换
//   - 终端面板切换
//   - Diff 面板切换
//   - 字体大小调整
//   - 剪贴板操作
//   - 线程管理 (pin/rename/archive)
// =============================================================================

/**
 * UIEventBus — UI 事件发布/订阅总线
 *
 * 混淆名: 从 MessageDispatcher 中 16 个 UI 控制消息 case 推导
 *
 * 使用方式:
 *   // 组件中订阅
 *   useEffect(() => uiEventBus.on("toggle-sidebar", handler), []);
 *   // MessageDispatcher 中发布
 *   uiEventBus.emit("toggle-sidebar", msg);
 */
class UIEventBus {
    constructor() {
        /** @type {Map<string, Set<Function>>} */
        this.listeners = new Map();
    }

    /**
     * 订阅事件
     * @param {string} event - 事件名
     * @param {Function} handler - 处理函数
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

    /**
     * 发布事件
     * @param {string} event - 事件名
     * @param {Object} data - 事件数据
     */
    emit(event, data = {}) {
        const set = this.listeners.get(event);
        if (set) set.forEach((handler) => handler(data));
    }

    /**
     * 一次性订阅
     * @param {string} event
     * @param {Function} handler
     */
    once(event, handler) {
        const unsub = this.on(event, (data) => {
            unsub();
            handler(data);
        });
        return unsub;
    }
}

// 单例实例
export const uiEventBus = new UIEventBus();

// ===================== UI 事件常量 =====================
/**
 * 所有 UI 事件名称常量
 * 与 MessageDispatcher.js 中的 case 一一对应
 */
export const UIEvents = {
    // 面板控制
    OPEN_COMMAND_MENU: "open-command-menu",
    TOGGLE_SIDEBAR: "toggle-sidebar",
    TOGGLE_TERMINAL: "toggle-terminal",
    TOGGLE_DIFF_PANEL: "toggle-diff-panel",

    // 搜索
    FIND_IN_THREAD: "find-in-thread",

    // 导航
    PREVIOUS_THREAD: "previous-thread",
    NEXT_THREAD: "next-thread",

    // 外观
    STEP_FONT_SIZE: "step-font-size",

    // 剪贴板
    COPY_CONVERSATION_PATH: "copy-conversation-path",
    COPY_WORKING_DIRECTORY: "copy-working-directory",
    COPY_SESSION_ID: "copy-session-id",
    COPY_DEEPLINK: "copy-deeplink",

    // 线程管理
    TOGGLE_THREAD_PIN: "toggle-thread-pin",
    RENAME_THREAD: "rename-thread",
    ARCHIVE_THREAD: "archive-thread",
    PINNED_THREADS_UPDATED: "pinned-threads-updated",
};

export default uiEventBus;
