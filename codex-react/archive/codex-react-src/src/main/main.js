// =============================================================================
// Codex.app — Electron Main 进程入口
// 提取自: main-B6C8fi5S.js
//
// 功能: 应用初始化, 窗口管理, IPC 路由, 子进程管理
// =============================================================================

const { app, ipcMain, BrowserWindow } = require("electron");

// ===================== 核心模块 =====================

// WindowManager (混淆名: Xpe/Vt) — 管理所有 BrowserWindow
// HostContextManager (混淆名: ape) — 每个 Host 的上下文管理器
// AppServerConnection (混淆名: rue) — 与 Rust codex 通信
// IpcClient (混淆名: d9)  — 跨实例 Unix socket 通信
// GlobalState (混淆名: d$) — 持久化全局状态 (.codex-global-state.json)
// SparkleManager (混淆名: Ice/wS) — 应用自动更新
// GitManager (混淆名: kZ/zB) — Git 操作
// InboxManager (混淆名: b7/fde) — 收件箱管理

// ===================== 启动流程 =====================

/**
 * 1. 解析 build flavor (prod/internal-alpha/public-beta/dev)
 * 2. 加载 shell 环境变量
 * 3. 初始化 Sentry 错误报告
 * 4. 初始化 Datadog 日志
 * 5. 创建 GlobalState
 * 6. 创建 WindowManager
 * 7. 注册 IPC 处理器
 * 8. app.whenReady → 创建第一个窗口
 */

// ===================== IPC 路由 (核心) =====================

/**
 * Renderer → Main 的消息入口
 *
 * 所有 React 前端消息都通过此 channel 进入 Main 进程
 */
ipcMain.handle("codex_desktop:message-from-view", async (event, msg) => {
    // 验证发送者可信
    if (!windowManager.isTrustedIpcSender(event.sender, event.senderFrame)) return;

    // 根据窗口找到对应的 HostContextManager
    const context = windowManager.getContextForWebContents(event.sender);
    if (!context) {
        logger.warning("Message received for unknown window context");
        return;
    }

    // 将消息转发给 HostContextManager 处理
    await context.handleMessage(event.sender, msg);
});

// ===================== HostContextManager =====================

/**
 * HostContextManager (混淆名: ape)
 *
 * 每个 Host (本地或远程) 对应一个实例
 * 内含 AppServerConnection, 负责 Renderer ↔ codex 的消息路由
 */
class HostContextManager {
    constructor({
        host,
        windowManager,
        globalState,
        repoRoot,
        errorReporter,
        // ...更多依赖
    }) {
        this.host = host;
        this.windowManager = windowManager;

        // 创建 AppServerConnection (连接 Rust codex)
        this.appServerConnection = new AppServerConnection(messageChannel, {
            hostId: host.id,
            globalState,
            repoRoot,
            resolveTransportConfig: () => resolveTransport(host),
            // ...
        });

        // 注册 turn/completed 监听
        this.appServerConnection.registerInternalNotificationHandler((notification) => {
            if (notification.method === "turn/completed") {
                appEventEmitter.emit("turnComplete");
            }
        });
    }

    /**
     * 处理来自 Renderer 的消息
     * 根据消息类型分发到 AppServerConnection
     */
    async handleMessage(sender, msg) {
        switch (msg.type) {
            // Renderer 发来的 JSON-RPC 请求 → 转发给 codex
            case "mcp-request-from-renderer":
                await this.appServerConnection.handleClientRequest(sender, msg.request);
                break;

            // Renderer 发来的通知
            case "mcp-notification-from-renderer":
                await this.appServerConnection.handleClientNotification(msg.notification);
                break;

            // Renderer 发来的响应 (如审批决定)
            case "mcp-response":
                await this.appServerConnection.handleClientResponse(msg.response);
                break;

            // 其他消息类型...
        }
    }

    getMessageHandler() {
        return this;
    }
}

// ===================== 传输层选择 =====================

/**
 * 解析 Transport 配置: WebSocket 或 stdio
 */
async function resolveTransport(hostConfig) {
    // 1. 尝试 WebSocket (远程连接)
    const wsConfig = resolveWebSocketConfig(hostConfig);
    if (wsConfig) return wsConfig;

    // 2. 回退到 stdio (本地 codex 子进程)
    const stdioConfig = resolveStdioConfig(hostConfig);
    if (stdioConfig) return stdioConfig;

    throw new Error("Unable to locate the Codex CLI binary.");
}

/**
 * stdio transport: 找到 codex 二进制并启动 app-server
 *
 * 搜索路径:
 *   1. CODEX_CLI_PATH 环境变量
 *   2. process.resourcesPath/codex
 *   3. process.resourcesPath/app.asar.unpacked/codex
 *   4. repoRoot/extension/bin/codex
 */
function resolveStdioConfig(hostConfig) {
    const executable = findCodexBinary();
    if (!executable) return null;

    return {
        kind: "stdio",
        executablePath: executable.executablePath,
        args: ["app-server", "--analytics-default-enabled"],
        env: {
            ...process.env,
            RUST_LOG: process.env.RUST_LOG ?? "warn",
        },
    };
}

/**
 * WebSocket transport: 连接远程 codex 服务器
 */
function resolveWebSocketConfig(hostConfig) {
    const wsUrl = process.env.CODEX_APP_SERVER_WS_URL ?? hostConfig.websocket_url;
    if (!wsUrl) return null;

    return {
        kind: "websocket",
        websocketUrl: normalizeWsUrl(wsUrl), // 确保以 /rpc 结尾
        headers: {},
    };
}

// ===================== 窗口创建 =====================

/**
 * 创建或显示主窗口
 */
async function ensurePrimaryWindow(hostId) {
    const existing = windowManager.getPrimaryWindow(hostId);
    if (existing) {
        if (existing.isMinimized()) existing.restore();
        existing.show();
        existing.focus();
        return existing;
    }

    const hostConfig = hosts.get(hostId);
    if (!hostConfig) return null;

    // 确保 HostContextManager 已创建
    getOrCreateHostContext(hostConfig);

    return await windowManager.createWindow({
        title: hostConfig.kind === "local" ? "Codex" : hostConfig.display_name,
        hostId: hostConfig.id,
    });
}

// ===================== 应用事件 =====================

app.whenReady().then(async () => {
    await sparkleManager.initialize();
    await ensurePrimaryWindow(localHostId);
});

app.on("activate", () => {
    windowManager.showPrimaryWindow(localHostId) || ensurePrimaryWindow(localHostId);
});

app.on("before-quit", (event) => {
    const confirmQuit = dialog.showMessageBoxSync({
        type: "warning",
        buttons: ["Quit", "Cancel"],
        title: "Quit Codex?",
        message: "Quit Codex?",
        detail: "Any local threads running on this machine will be interrupted",
    });
    if (confirmQuit !== 0) event.preventDefault();
});

app.on("will-quit", () => {
    // 刷新所有状态并清理
    for (const state of globalStates.values()) state.flush();
    for (const context of hostContexts.values()) context.flushTelemetry();
});

module.exports = { HostContextManager, resolveTransport };
