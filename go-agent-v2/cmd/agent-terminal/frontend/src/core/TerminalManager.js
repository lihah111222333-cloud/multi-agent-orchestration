// =============================================================================
// Codex.app — 终端管理器 (TerminalManager)
// 从 MessageDispatcher.js 中 5 个终端消息 case + main.js 终端 IPC 推导
//
// 功能: 管理内嵌终端会话的生命周期和数据流
//   - 接收终端数据 (stdin/stdout)
//   - 处理终端退出和错误
//   - 管理终端实例的创建/销毁
//   - 提供给 React 组件的订阅接口
// =============================================================================

/**
 * TerminalSession — 单个终端会话
 *
 * 每个终端实例对应一个 node-pty 进程 (Main 端)
 * Renderer 端通过 TerminalManager 管理状态
 */
class TerminalSession {
    /**
     * @param {string} id - 终端 ID
     * @param {Object} options
     * @param {string} options.cwd - 工作目录
     * @param {string[]} options.env - 环境变量
     */
    constructor(id, options = {}) {
        this.id = id;
        this.cwd = options.cwd ?? null;
        this.status = "initializing"; // "initializing" | "attached" | "exited" | "error"
        this.exitCode = null;
        this.error = null;
        this.dataListeners = new Set();
        this.statusListeners = new Set();
        this.initLogs = [];
    }

    /**
     * 追加数据 (from terminal-data 消息)
     * @param {string} data - 终端输出数据
     */
    appendData(data) {
        this.dataListeners.forEach((cb) => cb(data));
    }

    /**
     * 订阅终端数据流
     * @param {Function} callback - (data: string) => void
     * @returns {Function} unsubscribe
     */
    onData(callback) {
        this.dataListeners.add(callback);
        return () => this.dataListeners.delete(callback);
    }

    /**
     * 订阅状态变化
     * @param {Function} callback - (status) => void
     * @returns {Function} unsubscribe
     */
    onStatusChange(callback) {
        this.statusListeners.add(callback);
        return () => this.statusListeners.delete(callback);
    }

    /** 通知状态变化 */
    _notifyStatusChange() {
        this.statusListeners.forEach((cb) => cb({
            status: this.status,
            exitCode: this.exitCode,
            error: this.error,
        }));
    }
}

/**
 * TerminalManager — 终端会话管理器
 *
 * 管理多个终端实例, 提供创建/销毁/订阅接口
 */
class TerminalManager {
    constructor() {
        /** @type {Map<string, TerminalSession>} */
        this.sessions = new Map();
        /** @type {Set<Function>} */
        this.sessionListListeners = new Set();
    }

    // ======================== 消息处理 (被 MessageDispatcher 调用) ========================

    /**
     * 处理终端数据消息
     * @param {Object} msg - { terminalId, data }
     */
    handleTerminalData(msg) {
        const session = this.getOrCreateSession(msg.terminalId);
        session.appendData(msg.data);
    }

    /**
     * 处理终端退出
     * @param {Object} msg - { terminalId, exitCode }
     */
    handleTerminalExit(msg) {
        const session = this.sessions.get(msg.terminalId);
        if (session) {
            session.status = "exited";
            session.exitCode = msg.exitCode ?? null;
            session._notifyStatusChange();
        }
    }

    /**
     * 处理终端错误
     * @param {Object} msg - { terminalId, error }
     */
    handleTerminalError(msg) {
        const session = this.getOrCreateSession(msg.terminalId);
        session.status = "error";
        session.error = msg.error ?? "Unknown error";
        session._notifyStatusChange();
    }

    /**
     * 处理终端初始化日志
     * @param {Object} msg - { terminalId, log }
     */
    handleTerminalInitLog(msg) {
        const session = this.getOrCreateSession(msg.terminalId);
        session.initLogs.push(msg.log);
    }

    /**
     * 处理终端已连接
     * @param {Object} msg - { terminalId }
     */
    handleTerminalAttached(msg) {
        const session = this.getOrCreateSession(msg.terminalId);
        session.status = "attached";
        session._notifyStatusChange();
    }

    // ======================== 会话管理 ========================

    /**
     * 获取或创建终端会话
     * @param {string} terminalId
     * @returns {TerminalSession}
     */
    getOrCreateSession(terminalId) {
        let session = this.sessions.get(terminalId);
        if (!session) {
            session = new TerminalSession(terminalId);
            this.sessions.set(terminalId, session);
            this._notifySessionListChange();
        }
        return session;
    }

    /**
     * 获取终端会话
     * @param {string} terminalId
     * @returns {TerminalSession|undefined}
     */
    getSession(terminalId) {
        return this.sessions.get(terminalId);
    }

    /**
     * 获取所有会话
     * @returns {TerminalSession[]}
     */
    getAllSessions() {
        return Array.from(this.sessions.values());
    }

    /**
     * 销毁终端会话
     * @param {string} terminalId
     */
    destroySession(terminalId) {
        const session = this.sessions.get(terminalId);
        if (session) {
            session.dataListeners.clear();
            session.statusListeners.clear();
            this.sessions.delete(terminalId);
            this._notifySessionListChange();
        }
    }

    /**
     * 订阅会话列表变化
     * @param {Function} callback
     * @returns {Function} unsubscribe
     */
    onSessionListChange(callback) {
        this.sessionListListeners.add(callback);
        return () => this.sessionListListeners.delete(callback);
    }

    /** 通知会话列表变化 */
    _notifySessionListChange() {
        this.sessionListListeners.forEach((cb) => cb(this.getAllSessions()));
    }
}

// 单例实例
export const terminalManager = new TerminalManager();
export { TerminalSession };
export default terminalManager;
