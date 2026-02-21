// =============================================================================
// Codex.app — AppServerConnection (Electron Main 进程 ↔ Rust codex 通信)
// 提取自: main-B6C8fi5S.js (class rue)
//
// 功能: 管理与 Rust codex binary 的 JSON-RPC 2.0 通信
// 传输层: WebSocket (wss://) 或 stdio (本地 codex app-server 子进程)
// =============================================================================

// 传输层 + 通信工具 (Main 进程 CommonJS 环境)
const { app } = require("electron");
const crypto = require("crypto");

// ===================== 工具函数 (Main 进程本地定义) =====================
// (这些在 renderer/utils.js 中也有 ESM 版本, 此处为 Main 进程 CommonJS 版)

function isResponse(msg) {
    return msg.id != null && (msg.result !== undefined || msg.error !== undefined);
}

function isRequest(msg) {
    return msg.id != null && msg.method != null;
}

function isNotification(msg) {
    return msg.id == null && msg.method != null;
}

function extractThreadId(params) {
    if (!params) return null;
    return params.threadId ?? params.thread?.id ?? null;
}

function enrichStartParams(params, enableFeatures) {
    return { ...params, enableFeatures: enableFeatures ?? [] };
}

function normalizeWsUrl(url) {
    if (!url) return url;
    return url.endsWith("/rpc") ? url : `${url.replace(/\/+$/, "")}/rpc`;
}

/**
 * createTransport — 创建通信传输层 (WebSocket 或 stdio)
 * 简化实现: 仅包含 stdio transport
 *
 * @param {Object} config - { kind, executablePath, args, env } 或 { kind, websocketUrl }
 * @param {Object} callbacks - { onMessageLine, onErrorMessage, onClosed }
 * @returns {{ start: Function, send: Function, stop: Function }}
 */
function createTransport(config, callbacks) {
    if (config.kind === "stdio") {
        const { spawn } = require("child_process");
        let child = null;
        let buffer = "";
        return {
            async start() {
                child = spawn(config.executablePath, config.args, {
                    env: config.env,
                    stdio: ["pipe", "pipe", "pipe"],
                });
                child.stdout.on("data", (data) => {
                    buffer += data.toString();
                    let idx;
                    while ((idx = buffer.indexOf("\n")) !== -1) {
                        const line = buffer.slice(0, idx);
                        buffer = buffer.slice(idx + 1);
                        callbacks.onMessageLine(line);
                    }
                });
                child.stderr.on("data", (data) => {
                    callbacks.onErrorMessage(data.toString());
                });
                child.on("close", (code) => {
                    callbacks.onClosed({ code });
                });
            },
            send(message) {
                if (child?.stdin?.writable) {
                    child.stdin.write(JSON.stringify(message) + "\n");
                }
            },
            stop() {
                if (child) {
                    child.kill();
                    child = null;
                }
            },
        };
    }
    // WebSocket transport
    if (config.kind === "websocket") {
        const WebSocket = require("ws");
        let ws = null;
        return {
            async start() {
                return new Promise((resolve, reject) => {
                    ws = new WebSocket(config.websocketUrl, {
                        headers: config.headers ?? {},
                    });
                    ws.on("open", () => resolve());
                    ws.on("error", (err) => {
                        callbacks.onErrorMessage(err.message);
                        reject(err);
                    });
                    ws.on("message", (data) => {
                        const text = data.toString();
                        // WebSocket 每条消息就是一个完整的 JSON
                        callbacks.onMessageLine(text);
                    });
                    ws.on("close", (code) => {
                        callbacks.onClosed({ code });
                    });
                });
            },
            send(message) {
                if (ws && ws.readyState === WebSocket.OPEN) {
                    ws.send(JSON.stringify(message));
                }
            },
            stop() {
                if (ws) {
                    ws.close();
                    ws = null;
                }
            },
        };
    }
    throw new Error(`Unsupported transport kind: ${config.kind}`);
}

class AppServerConnection {
    // ===================== 初始化 =====================

    constructor(messageChannel, options) {
        this.messageChannel = messageChannel; // IPC channel 名
        this.options = options;               // { hostConfig, repoRoot, globalState, ... }
        this.transport = null;                // WebSocket 或 stdio 传输
        this.initialized = false;
        this.listeners = new Set();           // 注册的 BrowserWindow webContents
        this.pendingRequests = new Map();     // id → { sender, method, params }
        this.internalResponseHandlers = new Map();
        this.internalNotificationHandlers = new Set();
        this.ephemeralThreadTimeouts = new Map();
        this.fatalErrorMessage = null;
        this.authTokenCache = null;
        this.terminalManager = null;
    }

    // ===================== 窗口注册 =====================

    registerWebContents(webContents) {
        this.listeners.add(webContents);
        webContents.once("destroyed", () => {
            this.listeners.delete(webContents);
            this.dropPendingRequestsFor(webContents);
        });
        // 如果有致命错误, 立即通知新窗口
        if (this.fatalErrorMessage) {
            webContents.send(this.messageChannel, {
                type: "codex-app-server-fatal-error",
                errorMessage: this.fatalErrorMessage,
            });
        }
    }

    // ===================== 核心 API 方法 =====================

    /**
     * 创建对话线程
     * JSON-RPC: thread/start
     */
    async startThread(params) {
        await this.ensureReady();
        const enableFeatures = this.options.sharedObjectRepository.get("statsig_default_enable_features");
        const enrichedParams = enrichStartParams(params, enableFeatures);
        const id = `thread/start:${crypto.randomUUID()}`;
        const response = await this.sendInternalRequest({ id, method: "thread/start", params: enrichedParams });
        if (response.error) throw new Error(response.error.message);
        // 标记临时线程
        if (params.ephemeral === true) this.markEphemeralThread(response.result.thread.id);
        return response.result;
    }

    /**
     * 发送用户消息, 启动 AI 回复
     * JSON-RPC: turn/start
     */
    async startTurn(params) {
        await this.ensureReady();
        const id = `turn/start:${crypto.randomUUID()}`;
        const response = await this.sendInternalRequest({ id, method: "turn/start", params });
        if (response.error) throw new Error(response.error.message);
        return response.result;
    }

    /**
     * 中断正在进行的 AI 回复
     * JSON-RPC: turn/interrupt
     */
    async interruptTurn(params) {
        await this.ensureReady();
        const id = `turn/interrupt:${crypto.randomUUID()}`;
        const response = await this.sendInternalRequest({ id, method: "turn/interrupt", params });
        if (response.error) throw new Error(response.error.message);
        return response.result;
    }

    /**
     * 获取技能列表
     * JSON-RPC: skills/list
     */
    async listSkills(params) {
        await this.ensureReady();
        const response = await this.sendInternalRequest({
            id: `skills:${crypto.randomUUID()}`,
            method: "skills/list",
            params,
        });
        if (response.error) throw new Error(response.error.message);
        return response.result ?? { data: [] };
    }

    /**
     * 读取配置
     * JSON-RPC: config/read
     */
    async readConfig(params) {
        await this.ensureReady();
        const response = await this.sendInternalRequest({
            id: `config/read:${crypto.randomUUID()}`,
            method: "config/read",
            params,
        });
        if (response.error) throw new Error(response.error.message);
        return response.result;
    }

    /**
     * 获取认证令牌
     * JSON-RPC: getAuthStatus
     */
    async getAuthToken({ refreshToken } = {}) {
        await this.ensureReady();
        if (!refreshToken && this.authTokenCache) return this.authTokenCache.value;
        return this.fetchAuthToken(refreshToken);
    }

    // ===================== 传输层管理 =====================

    /**
     * 确保 transport 就绪
     * 如果未初始化, 启动 transport 并发送 initialize 请求
     */
    async ensureReady() {
        if (!this.initialized) {
            if (!this.initializingPromise) {
                this.initializingPromise = this.startProcess();
            }
            await this.initializingPromise;
        }
    }

    /**
     * 启动 transport 并等待 initialize 完成
     */
    async startProcess() {
        await this.startTransport();
        return new Promise((resolve, reject) => {
            this.resolveInitialize = () => {
                this.initialized = true;
                this.broadcastToWindows({ type: "codex-app-server-initialized" });
                resolve();
            };
            this.rejectInitialize = (err) => {
                this.broadcastFatalError(err.message);
                reject(err);
            };
            this.sendInitializeRequest();
        });
    }

    /**
     * 发送 JSON-RPC initialize 请求
     */
    sendInitializeRequest() {
        this.sendMessage({
            id: "__codex-desktop_initialize__",
            method: "initialize",
            params: {
                clientInfo: { name: "Codex Desktop", title: "Codex Desktop", version: app.getVersion() },
                capabilities: { experimentalApi: true },
            },
        });
    }

    /**
     * 解析并启动 transport (WebSocket 或 stdio)
     */
    async startTransport() {
        const config = await this.options.resolveTransportConfig();
        // WebSocket transport: 连接 wss://host/rpc
        // Stdio transport: 启动 codex app-server 子进程, 通过 stdin/stdout 通信
        const transport = createTransport(config, {
            onMessageLine: (line) => this.handleIncomingLine(line),
            onErrorMessage: (msg) => { this.mostRecentErrorMessage = msg; },
            onClosed: (info) => this.handleTransportClosed(info),
        });
        await transport.start();
        this.transport = transport;
    }

    // ===================== 消息处理 =====================

    /**
     * 处理从 codex 收到的每一行 JSON (核心消息路由)
     */
    handleIncomingLine(line) {
        const trimmed = line.trim();
        if (!trimmed) return;

        let message;
        try { message = JSON.parse(trimmed); } catch { return; }

        // 未初始化时只接受 initialize 响应
        if (!this.initialized) {
            if (isResponse(message) && message.id === "__codex-desktop_initialize__") {
                if (message.error) this.rejectInitialize?.(new Error(JSON.stringify(message.error)));
                else this.resolveInitialize?.();
            }
            return;
        }

        // ① 响应 → 路由回请求发起方
        if (isResponse(message)) {
            this.routeResponse(message);
            return;
        }

        // ② 服务端请求 (如 tool approval) → 广播给 Renderer
        if (isRequest(message)) {
            this.broadcastToWindows({ type: "mcp-request", request: message });
            return;
        }

        // ③ 通知 (无 id) → 特殊处理 + 广播
        if (isNotification(message)) {
            // 清除 auth 缓存
            if (message.method === "account/updated") this.clearAuthTokenCache();

            // 内部通知处理器 (如 turn/completed → emit turnComplete)
            for (const handler of this.internalNotificationHandlers) {
                try { handler(message); } catch { }
            }

            // 过滤 ephemeral thread 通知
            const threadId = extractThreadId(message.params);
            if (threadId && this.ephemeralThreadTimeouts.has(threadId)) return;

            // 广播到所有窗口
            this.broadcastToWindows({ type: "mcp-notification", method: message.method, params: message.params });
        }
    }

    /**
     * 路由响应: 找到对应的 pending request, 发回给请求方 webContents
     */
    routeResponse(response) {
        const key = this.toRequestKey(response.id);
        const pending = this.pendingRequests.get(key);
        const internal = this.internalResponseHandlers.get(key);

        // 内部请求 (startThread/startTurn 等直接调用)
        if (internal) {
            this.internalResponseHandlers.delete(key);
            if (response.error) internal.reject(new Error(response.error.message));
            else internal.resolve(response);
            return;
        }

        // 特殊处理: turn/start 成功时记录, thread/archive 成功时刷新
        if (pending && !response.error) {
            switch (pending.method) {
                case "turn/start": /* 记录自动化 */ break;
                case "thread/archive": /* 广播刷新 */ break;
                case "thread/unarchive": /* 广播刷新 */ break;
            }
        }

        // 发回给发起请求的 renderer window
        const sender = pending?.sender;
        if (sender && !sender.isDestroyed()) {
            sender.send(this.messageChannel, { type: "mcp-response", message: response });
        } else {
            this.broadcastToWindows({ type: "mcp-response", message: response });
        }
        this.pendingRequests.delete(key);
    }

    // ===================== Renderer 请求处理 =====================

    /**
     * 处理来自 Renderer 的 JSON-RPC 请求
     */
    async handleClientRequest(sender, request) {
        await this.ensureReady();
        const key = this.toRequestKey(request.id);
        this.pendingRequests.set(key, { sender, method: request.method, params: request.params });
        this.sendMessage(request);
    }

    /**
     * 处理来自 Renderer 的通知 (如审批决定)
     */
    async handleClientNotification(notification) {
        await this.ensureReady();
        this.sendMessage({ method: notification.method, params: notification.params });
    }

    /**
     * 处理来自 Renderer 的响应 (如 tool approval response)
     */
    async handleClientResponse(response) {
        await this.ensureReady();
        this.sendMessage({ id: response.id, result: response.result });
    }

    // ===================== 辅助方法 =====================

    broadcastToWindows(message) {
        for (const wc of this.listeners) {
            if (!wc.isDestroyed()) wc.send(this.messageChannel, message);
        }
    }

    sendMessage(message) {
        if (!this.transport) throw new Error("Transport not available");
        this.transport.send(message);
    }

    async restart() {
        this.stopProcess();
        this.fatalErrorMessage = null;
        this.initializingPromise = null;
        await this.ensureReady();
    }

    // ===================== 内部请求 =====================

    /**
     * sendInternalRequest — 发送内部 JSON-RPC 请求并等待响应
     * 用于 startThread/startTurn/interruptTurn/listSkills/readConfig 等
     *
     * @param {Object} request - { id, method, params }
     * @returns {Promise<Object>} JSON-RPC response
     */
    sendInternalRequest(request) {
        return new Promise((resolve, reject) => {
            const key = this.toRequestKey(request.id);
            this.internalResponseHandlers.set(key, { resolve, reject });
            this.sendMessage(request);
        });
    }

    /**
     * toRequestKey — 将请求 ID 归一化为 Map key
     * 请求 ID 直接作为 key (string)
     *
     * @param {string} id
     * @returns {string}
     */
    toRequestKey(id) {
        return String(id);
    }

    /**
     * registerInternalNotificationHandler — 注册内部通知处理器
     * 被 HostContextManager 调用以监听 turn/completed 等
     *
     * @param {Function} handler - (notification: { method, params }) => void
     */
    registerInternalNotificationHandler(handler) {
        this.internalNotificationHandlers.add(handler);
    }

    // ===================== 进程/传输层生命周期 =====================

    /**
     * stopProcess — 停止 transport 并重置状态
     */
    stopProcess() {
        if (this.transport) {
            this.transport.stop();
            this.transport = null;
        }
        this.initialized = false;
        this.initializingPromise = null;
        this.resolveInitialize = null;
        this.rejectInitialize = null;
    }

    /**
     * handleTransportClosed — transport 意外关闭时的处理
     * @param {Object} info - { code }
     */
    handleTransportClosed(info) {
        const wasInitialized = this.initialized;
        this.initialized = false;
        this.initializingPromise = null;
        this.transport = null;

        if (!wasInitialized) {
            // 初始化过程中关闭 → 视为致命错误
            const errorMsg = this.mostRecentErrorMessage || `Transport closed with code ${info.code}`;
            this.rejectInitialize?.(new Error(errorMsg));
        } else {
            // 已初始化后关闭 → 广播致命错误
            const errorMsg = this.mostRecentErrorMessage || `Codex process exited with code ${info.code}`;
            this.broadcastFatalError(errorMsg);
        }

        // 拒绝所有 pending 的内部请求
        for (const [key, handler] of this.internalResponseHandlers) {
            handler.reject(new Error("Transport closed"));
        }
        this.internalResponseHandlers.clear();
    }

    // ===================== 请求清理 =====================

    /**
     * dropPendingRequestsFor — 删除特定 webContents 发起的所有 pending 请求
     * 在窗口关闭时调用, 避免向已销毁的 webContents 发送响应
     *
     * @param {Electron.WebContents} webContents
     */
    dropPendingRequestsFor(webContents) {
        for (const [key, pending] of this.pendingRequests) {
            if (pending.sender === webContents) {
                this.pendingRequests.delete(key);
            }
        }
    }

    // ===================== 致命错误处理 =====================

    /**
     * broadcastFatalError — 广播致命错误到所有窗口
     * @param {string} errorMessage
     */
    broadcastFatalError(errorMessage) {
        this.fatalErrorMessage = errorMessage;
        this.broadcastToWindows({
            type: "codex-app-server-fatal-error",
            errorMessage,
        });
    }

    // ===================== Auth Token 管理 =====================

    /**
     * fetchAuthToken — 从 codex 后端获取认证令牌
     * @param {boolean} refreshToken - 是否强制刷新
     * @returns {Promise<Object>} token 对象
     */
    async fetchAuthToken(refreshToken) {
        const response = await this.sendInternalRequest({
            id: `auth:${crypto.randomUUID()}`,
            method: "getAuthStatus",
            params: { refreshToken: refreshToken ?? false },
        });
        if (response.error) throw new Error(response.error.message);
        // 缓存 token (有效期内)
        this.authTokenCache = {
            value: response.result,
            cachedAt: Date.now(),
        };
        return response.result;
    }

    /**
     * clearAuthTokenCache — 清除认证令牌缓存
     * 在收到 account/updated 通知时调用
     */
    clearAuthTokenCache() {
        this.authTokenCache = null;
    }

    // ===================== Ephemeral Thread 管理 =====================

    /**
     * markEphemeralThread — 标记临时线程
     * 临时线程的通知不会广播给 Renderer (避免 UI 闪烁)
     * 设置超时后自动清除标记
     *
     * @param {string} threadId
     * @param {number} timeoutMs - 超时时间 (默认 30s)
     */
    markEphemeralThread(threadId, timeoutMs = 30000) {
        // 清除之前的超时
        if (this.ephemeralThreadTimeouts.has(threadId)) {
            clearTimeout(this.ephemeralThreadTimeouts.get(threadId));
        }
        const timeout = setTimeout(() => {
            this.ephemeralThreadTimeouts.delete(threadId);
        }, timeoutMs);
        this.ephemeralThreadTimeouts.set(threadId, timeout);
    }
}

module.exports = { AppServerConnection };
