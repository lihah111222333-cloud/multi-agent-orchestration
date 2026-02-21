// =============================================================================
// Codex.app — AppServerConnection (Electron Main 进程 ↔ Rust codex 通信)
// 提取自: main-B6C8fi5S.js (class rue)
//
// 功能: 管理与 Rust codex binary 的 JSON-RPC 2.0 通信
// 传输层: WebSocket (wss://) 或 stdio (本地 codex app-server 子进程)
// =============================================================================

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
        await this.ensureReady();
    }
}

module.exports = { AppServerConnection };
