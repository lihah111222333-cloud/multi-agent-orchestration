// =============================================================================
// Codex.app — Electron Main 进程入口
// 提取自: main-B6C8fi5S.js
//
// 功能: 应用初始化, 窗口管理, IPC 路由, 子进程管理
// =============================================================================

const { app, ipcMain, BrowserWindow, dialog } = require("electron");
const path = require("path");
const fs = require("fs");
const EventEmitter = require("events");
const { AppServerConnection } = require("./AppServerConnection");

// ===================== 日志 =====================

const logger = {
    debug(msg, ctx) { console.debug(`[Codex:Main] ${msg}`, ctx ?? ""); },
    info(msg, ctx) { console.info(`[Codex:Main] ${msg}`, ctx ?? ""); },
    warning(msg, ctx) { console.warn(`[Codex:Main] ${msg}`, ctx ?? ""); },
    error(msg, ctx) { console.error(`[Codex:Main] ${msg}`, ctx ?? ""); },
};

// ===================== 应用级事件总线 =====================

const appEventEmitter = new EventEmitter();

// ===================== IPC Channel 常量 =====================

const MESSAGE_FROM_VIEW = "codex_desktop:message-from-view";
const MESSAGE_FOR_VIEW = "codex_desktop:message-for-view";

// ===================== 全局状态管理 =====================

/**
 * GlobalState — 持久化全局状态 (.codex-global-state.json)
 * 混淆名: d$
 */
class GlobalState {
    constructor(filePath) {
        this.filePath = filePath;
        this.data = {};
        this._load();
    }

    _load() {
        try {
            if (fs.existsSync(this.filePath)) {
                this.data = JSON.parse(fs.readFileSync(this.filePath, "utf-8"));
            }
        } catch {
            this.data = {};
        }
    }

    get(key) { return this.data[key]; }

    set(key, value) {
        this.data[key] = value;
    }

    flush() {
        try {
            const dir = path.dirname(this.filePath);
            if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true });
            fs.writeFileSync(this.filePath, JSON.stringify(this.data, null, 2));
        } catch (err) {
            logger.error("Failed to flush global state", err.message);
        }
    }
}

// ===================== 共享对象仓库 =====================

/**
 * SharedObjectRepository — 跨模块共享的键值存储
 * 用于 feature flags, statsig 配置等
 */
class SharedObjectRepository {
    constructor() { this.store = new Map(); }
    get(key) { return this.store.get(key); }
    set(key, value) { this.store.set(key, value); }
}

// ===================== WindowManager =====================

/**
 * WindowManager (混淆名: Xpe/Vt) — 管理所有 BrowserWindow
 */
class WindowManager {
    constructor() {
        this.windows = new Map();       // hostId → BrowserWindow[]
        this.contextMap = new Map();    // webContents.id → HostContextManager
    }

    /**
     * 验证 IPC 发送者是否可信
     */
    isTrustedIpcSender(sender, senderFrame) {
        // 简化: 仅验证发送者存在且未销毁
        return sender && !sender.isDestroyed();
    }

    /**
     * 根据 webContents 获取对应的 HostContextManager
     */
    getContextForWebContents(webContents) {
        return this.contextMap.get(webContents.id) ?? null;
    }

    /**
     * 注册 webContents 与 HostContextManager 的关联
     */
    registerContext(webContents, context) {
        this.contextMap.set(webContents.id, context);
        webContents.once("destroyed", () => {
            this.contextMap.delete(webContents.id);
        });
    }

    /**
     * 获取 Host 的主窗口
     */
    getPrimaryWindow(hostId) {
        const wins = this.windows.get(hostId);
        return wins && wins.length > 0 ? wins[0] : null;
    }

    /**
     * 显示主窗口 (如果存在)
     */
    showPrimaryWindow(hostId) {
        const win = this.getPrimaryWindow(hostId);
        if (win && !win.isDestroyed()) {
            if (win.isMinimized()) win.restore();
            win.show();
            win.focus();
            return true;
        }
        return false;
    }

    /**
     * 创建新窗口
     */
    async createWindow({ title, hostId }) {
        const win = new BrowserWindow({
            width: 1200,
            height: 800,
            title: title ?? "Codex",
            webPreferences: {
                preload: path.join(__dirname, "..", "preload", "preload.js"),
                contextIsolation: true,
                nodeIntegration: false,
            },
            // macOS: 使用 hiddenInset 标题栏
            titleBarStyle: process.platform === "darwin" ? "hiddenInset" : "default",
        });

        // 加载 Renderer
        const indexPath = path.join(__dirname, "..", "renderer", "index.html");
        if (fs.existsSync(indexPath)) {
            await win.loadFile(indexPath);
        } else {
            // 开发模式: 从 Vite dev server 加载
            const devUrl = process.env.VITE_DEV_SERVER_URL ?? "http://localhost:5173";
            await win.loadURL(devUrl);
        }

        // 记录窗口
        if (!this.windows.has(hostId)) {
            this.windows.set(hostId, []);
        }
        this.windows.get(hostId).push(win);

        win.on("closed", () => {
            const wins = this.windows.get(hostId);
            if (wins) {
                const idx = wins.indexOf(win);
                if (idx >= 0) wins.splice(idx, 1);
            }
        });

        return win;
    }
}

// ===================== SparkleManager (自动更新) =====================

/**
 * SparkleManager (混淆名: Ice/wS) — 应用自动更新
 * 简化实现: 不执行实际更新检查
 */
class SparkleManager {
    async initialize() {
        logger.info("SparkleManager initialized (auto-update check skipped)");
    }
}

// ===================== 实例化核心模块 =====================

const globalStatePath = path.join(
    app.getPath("userData"),
    ".codex-global-state.json"
);
const globalState = new GlobalState(globalStatePath);
const globalStates = new Map([["default", globalState]]);
const sharedObjectRepository = new SharedObjectRepository();
const windowManager = new WindowManager();
const sparkleManager = new SparkleManager();

// Host 配置
const localHostId = "local";
const hosts = new Map([
    [localHostId, { id: localHostId, kind: "local", display_name: "Codex" }],
]);

// HostContext 实例缓存
const hostContexts = new Map();

// ===================== 工具函数 =====================

/**
 * normalizeWsUrl — 确保 WebSocket URL 以 /rpc 结尾
 */
function normalizeWsUrl(url) {
    if (!url) return url;
    return url.endsWith("/rpc") ? url : `${url.replace(/\/+$/, "")}/rpc`;
}

/**
 * findCodexBinary — 搜索 codex CLI 二进制文件
 *
 * 搜索路径:
 *   1. CODEX_CLI_PATH 环境变量
 *   2. process.resourcesPath/codex
 *   3. process.resourcesPath/app.asar.unpacked/codex
 *   4. repoRoot/extension/bin/codex
 */
function findCodexBinary() {
    const candidates = [];

    // 1. 环境变量
    if (process.env.CODEX_CLI_PATH) {
        candidates.push(process.env.CODEX_CLI_PATH);
    }

    // 2. resourcesPath
    if (process.resourcesPath) {
        candidates.push(path.join(process.resourcesPath, "codex"));
        candidates.push(path.join(process.resourcesPath, "app.asar.unpacked", "codex"));
    }

    // 3. 开发模式: 相对路径
    candidates.push(path.join(__dirname, "..", "..", "extension", "bin", "codex"));
    candidates.push(path.join(__dirname, "..", "..", "codex"));

    for (const candidate of candidates) {
        if (fs.existsSync(candidate)) {
            return { executablePath: candidate };
        }
    }

    return null;
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
        websocketUrl: normalizeWsUrl(wsUrl),
        headers: {},
    };
}

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
        windowManager: wm,
        globalState: gs,
        repoRoot,
        sharedObjectRepository: sor,
    }) {
        this.host = host;
        this.windowManager = wm;

        // 创建 AppServerConnection (连接 Rust codex)
        this.appServerConnection = new AppServerConnection(MESSAGE_FOR_VIEW, {
            hostId: host.id,
            globalState: gs,
            repoRoot,
            sharedObjectRepository: sor,
            resolveTransportConfig: () => resolveTransport(host),
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

            // Electron 操作: 设置窗口模式
            case "electron-set-window-mode":
                // 完整实现: 切换窗口大小/样式 (onboarding 模式 vs app 模式)
                break;

            // Telemetry 事件
            case "telemetry-track-event":
                // 完整实现: 转发到 Datadog/analytics
                break;

            default:
                logger.debug(`Unhandled message type from renderer: ${msg.type}`);
                break;
        }
    }

    /**
     * 注册窗口 webContents
     */
    registerWebContents(webContents) {
        this.appServerConnection.registerWebContents(webContents);
        this.windowManager.registerContext(webContents, this);
    }

    getMessageHandler() {
        return this;
    }

    flushTelemetry() {
        // 完整实现: 刷新 Datadog/analytics 缓冲
    }
}

/**
 * 获取或创建 HostContextManager
 */
function getOrCreateHostContext(hostConfig) {
    let context = hostContexts.get(hostConfig.id);
    if (!context) {
        context = new HostContextManager({
            host: hostConfig,
            windowManager,
            globalState,
            repoRoot: null,
            sharedObjectRepository,
        });
        hostContexts.set(hostConfig.id, context);
    }
    return context;
}

// ===================== IPC 路由 (核心) =====================

/**
 * Renderer → Main 的消息入口
 *
 * 所有 React 前端消息都通过此 channel 进入 Main 进程
 */
ipcMain.handle(MESSAGE_FROM_VIEW, async (event, msg) => {
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

// ===================== 同步 IPC 处理器 =====================

ipcMain.on("codex_desktop:get-sentry-init-options", (event) => {
    event.returnValue = {
        dsn: null,
        environment: "dev",
        codexAppSessionId: `session-${Date.now()}`,
    };
});

ipcMain.on("codex_desktop:get-build-flavor", (event) => {
    event.returnValue = process.env.CODEX_BUILD_FLAVOR ?? "dev";
});

ipcMain.handle("codex_desktop:show-context-menu", async (event, options) => {
    // 完整实现: 创建 Electron Menu 并弹出
    return null;
});

ipcMain.handle("codex_desktop:trigger-sentry-test", async () => {
    logger.info("Sentry test triggered (no-op in dev)");
});

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
    const context = getOrCreateHostContext(hostConfig);

    const win = await windowManager.createWindow({
        title: hostConfig.kind === "local" ? "Codex" : hostConfig.display_name,
        hostId: hostConfig.id,
    });

    // 注册窗口到 context
    context.registerWebContents(win.webContents);

    return win;
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

module.exports = { HostContextManager, resolveTransport, WindowManager, GlobalState };
