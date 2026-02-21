// =============================================================================
// Codex.app — Preload Bridge (IPC 桥接层)
// 原始位置: .vite/build/preload.js
// 功能: 连接 React Renderer 与 Electron Main 进程
// =============================================================================

"use strict";

const { ipcRenderer, contextBridge, webUtils } = require("electron");

// IPC channel 常量
const CONTEXT_MENU_CHANNEL = "codex_desktop:show-context-menu";
const SENTRY_INIT_CHANNEL = "codex_desktop:get-sentry-init-options";
const BUILD_FLAVOR_CHANNEL = "codex_desktop:get-build-flavor";
const SENTRY_TEST_CHANNEL = "codex_desktop:trigger-sentry-test";
const MESSAGE_FROM_VIEW = "codex_desktop:message-from-view";
const MESSAGE_FOR_VIEW = "codex_desktop:message-for-view";

// Worker channels
function workerFromViewChannel(workerName) {
  return `codex_desktop:worker:${workerName}:from-view`;
}
function workerForViewChannel(workerName) {
  return `codex_desktop:worker:${workerName}:for-view`;
}

// 同步获取初始化数据
const sentryInitOptions = ipcRenderer.sendSync(SENTRY_INIT_CHANNEL);
const buildFlavor = ipcRenderer.sendSync(BUILD_FLAVOR_CHANNEL);

// Worker 订阅管理
const workerSubscribers = new Map();
const workerHandlers = new Map();

// =============================================================================
// electronBridge — 暴露给 Renderer 的 API
// =============================================================================
const electronBridge = {
  windowType: "electron",

  // Renderer → Main: 发送消息 (所有 MCP 请求/响应/通知都走这里)
  sendMessageFromView: async (msg) => {
    await ipcRenderer.invoke(MESSAGE_FROM_VIEW, msg);
  },

  // 获取文件路径 (拖拽等场景)
  getPathForFile: (file) => {
    const path = webUtils.getPathForFile(file);
    return path || null;
  },

  // Worker 消息通道
  sendWorkerMessageFromView: async (workerName, msg) => {
    await ipcRenderer.invoke(workerFromViewChannel(workerName), msg);
  },

  // 订阅 Worker 消息
  subscribeToWorkerMessages: (workerName, callback) => {
    let subscribers = workerSubscribers.get(workerName);
    if (!subscribers) {
      subscribers = new Set();
      workerSubscribers.set(workerName, subscribers);
    }

    let handler = workerHandlers.get(workerName);
    if (!handler) {
      handler = (event, data) => {
        const subs = workerSubscribers.get(workerName);
        if (subs) subs.forEach((cb) => cb(data));
      };
      workerHandlers.set(workerName, handler);
      ipcRenderer.on(workerForViewChannel(workerName), handler);
    }

    subscribers.add(callback);

    // 返回取消订阅函数
    return () => {
      const subs = workerSubscribers.get(workerName);
      if (!subs) return;
      subs.delete(callback);
      if (subs.size > 0) return;
      workerSubscribers.delete(workerName);
      const h = workerHandlers.get(workerName);
      if (h) ipcRenderer.removeListener(workerForViewChannel(workerName), h);
      workerHandlers.delete(workerName);
    };
  },

  showContextMenu: async (options) => ipcRenderer.invoke(CONTEXT_MENU_CHANNEL, options),
  triggerSentryTestError: async () => { await ipcRenderer.invoke(SENTRY_TEST_CHANNEL); },
  getSentryInitOptions: () => sentryInitOptions,
  getAppSessionId: () => sentryInitOptions.codexAppSessionId,
  getBuildFlavor: () => buildFlavor,
};

// =============================================================================
// 反向通道: Main → Renderer
// Main 进程通过 webContents.send(MESSAGE_FOR_VIEW, data) 发送消息
// 这里转成 window MessageEvent, React 通过 window.addEventListener("message") 接收
// =============================================================================
ipcRenderer.on(MESSAGE_FOR_VIEW, (event, data) => {
  window.dispatchEvent(new MessageEvent("message", { data }));
});

// 暴露到 window 对象
contextBridge.exposeInMainWorld("codexWindowType", "electron");
contextBridge.exposeInMainWorld("electronBridge", electronBridge);
