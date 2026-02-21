// =============================================================================
// MessageDispatcher.js — 全局消息分发器 (Wails 适配版)
//
// 原版监听 window "message" 事件 (Electron preload → postMessage)
// 现改为监听 Wails bridge-event (Go handleBridgeNotification → Wails Events)
//
// 分发目标:
//   - ConversationManager — MCP 协议通知 + 对话管理
//   - UIEventBus          — UI 控制 (侧边栏/终端/字体/剪贴板)
//   - TerminalManager     — 终端数据流
//   - AppStateManager     — 应用全局状态
//   - QueryClient         — React Query 缓存失效
//   - navigate()          — React Router 导航
// =============================================================================

import { bridge } from "../bridge";
import { uiEventBus } from "./UIEventBus";
import { terminalManager } from "./TerminalManager";
import { appStateManager } from "./AppStateManager";
import { logger } from "../logger";

/**
 * 注册全局消息监听器
 *
 * @param {ConversationManager} conversationManager - 对话管理器
 * @param {Function} navigate - React Router navigate
 * @param {QueryClient} queryClient - React Query client
 */
function setupMessageDispatcher(conversationManager, navigate, queryClient) {
    const cm = conversationManager;

    // ==================== Wails bridge-event 监听 ====================
    // Go handleBridgeNotification → wailsApp.Event.Emit("bridge-event", {type, payload})
    const unsubscribeBridge = bridge.onBridgeEvent((eventData) => {
        const { type, payload } = eventData || {};
        if (!type) return;

        // 按 type 前缀分发
        switch (true) {

            // ---- MCP 通知 → ConversationManager ----

            // 对话/轮次生命周期
            case type === "thread/started":
            case type === "turn/started":
            case type === "turn/completed":
            case type === "turn/diff/updated":
            case type === "turn/plan/updated":
            case type === "thread/compacted":
            case type === "thread/tokenUsage/updated":
            case type === "thread/name/updated":
            // Item 生命周期
            case type === "item/started":
            case type === "item/completed":
            // 流式文本 Delta
            case type === "item/agentMessage/delta":
            case type === "item/plan/delta":
            case type === "item/reasoning/summaryTextDelta":
            case type === "item/reasoning/textDelta":
            case type === "item/reasoning/summaryPartAdded":
            case type === "item/commandExecution/outputDelta":
            case type === "item/commandExecution/terminalInteraction":
            case type === "item/mcpToolCall/progress":
            case type === "item/fileChange/outputDelta":
            // 账户/配置
            case type === "account/updated":
            case type === "account/login/completed":
            case type === "account/rateLimits/updated":
            case type === "mcpServer/oauthLogin/completed":
            // 错误
            case type === "error":
            case type === "configWarning":
            case type === "deprecationNotice": {
                cm.onNotification(type, payload ?? {});
                break;
            }

            // ---- 审批请求 → ConversationManager.onRequest ----
            case type === "item/commandExecution/requestApproval":
            case type === "item/fileChange/requestApproval": {
                // Phase 2 完善: requestId 由 Go 端注入到 payload
                // 暂时用 payload 中已有的 id 字段兜底
                cm.onRequest({
                    id: payload?.requestId ?? payload?.id ?? `approval-${Date.now()}`,
                    method: type,
                    params: payload ?? {},
                });
                break;
            }

            // ---- Skills 更新 → React Query 失效 ----
            case type === "codex/event/skills_update_available": {
                queryClient.invalidateQueries({ queryKey: ["skills"] });
                cm.onNotification(type, payload ?? {});
                break;
            }

            // ---- Codex 后端状态 ----
            case type === "codex/event/shutdown_complete":
            case type === "codex/event/background_event":
            case type === "codex/event/mcp_startup_update":
            case type === "codex/event/mcp_startup_complete":
            case type === "codex/event/mcp_list_tools_response":
            case type === "codex/event/list_skills_response":
            case type === "codex/event/thread_rolled_back": {
                cm.onNotification(type, payload ?? {});
                break;
            }

            // ---- 搜索推送 ----
            case type === "fuzzyFileSearch/sessionUpdated":
            case type === "fuzzyFileSearch/sessionCompleted": {
                cm.onNotification(type, payload ?? {});
                break;
            }

            // ---- thread title 更新 (thread/name/updated 已处理, 兼容旧格式) ----
            case type === "thread-title-updated": {
                cm.setThreadTitle(payload?.conversationId, payload?.title);
                break;
            }

            // ---- Tasks 刷新 ----
            case type === "tasks-reload-requested": {
                queryClient.invalidateQueries({ queryKey: ["tasks"] });
                break;
            }

            // ---- 导航 ----
            case type === "navigate-to-route": {
                navigate(payload?.path);
                break;
            }
            case type === "navigate-back": {
                window.history.back();
                break;
            }
            case type === "navigate-forward": {
                window.history.forward();
                break;
            }

            // ---- App 全局状态 ----
            case type === "app-update-ready-changed": {
                appStateManager.handleAppUpdateReadyChanged(payload);
                break;
            }
            case type === "window-fullscreen-changed": {
                appStateManager.handleWindowFullscreenChanged(payload);
                break;
            }
            case type === "custom-prompts-updated": {
                appStateManager.handleCustomPromptsUpdated(payload);
                break;
            }
            case type === "workspace-root-options-updated": {
                appStateManager.handleWorkspaceRootOptionsUpdated(payload, queryClient);
                break;
            }
            case type === "active-workspace-roots-updated": {
                appStateManager.handleActiveWorkspaceRootsUpdated(payload, queryClient);
                break;
            }

            // ---- UI 控制 → UIEventBus ----
            case type === "toggle-sidebar":
            case type === "toggle-terminal":
            case type === "toggle-diff-panel":
            case type === "open-command-menu":
            case type === "find-in-thread":
            case type === "previous-thread":
            case type === "next-thread":
            case type === "step-font-size":
            case type === "copy-conversation-path":
            case type === "copy-working-directory":
            case type === "copy-session-id":
            case type === "copy-deeplink":
            case type === "rename-thread": {
                uiEventBus.emit(type, payload);
                break;
            }

            case type === "toggle-thread-pin": {
                uiEventBus.emit(type, payload);
                queryClient.invalidateQueries({ queryKey: ["thread/list"] });
                break;
            }
            case type === "archive-thread": {
                uiEventBus.emit(type, payload);
                queryClient.invalidateQueries({ queryKey: ["tasks"] });
                break;
            }
            case type === "pinned-threads-updated": {
                uiEventBus.emit(type, payload);
                queryClient.invalidateQueries({ queryKey: ["thread/list"] });
                break;
            }

            // ---- 终端 → TerminalManager ----
            case type === "terminal-data": {
                terminalManager.handleTerminalData(payload);
                break;
            }
            case type === "terminal-exit": {
                terminalManager.handleTerminalExit(payload);
                break;
            }
            case type === "terminal-error": {
                terminalManager.handleTerminalError(payload);
                break;
            }
            case type === "terminal-init-log": {
                terminalManager.handleTerminalInitLog(payload);
                break;
            }
            case type === "terminal-attached": {
                terminalManager.handleTerminalAttached(payload);
                break;
            }

            default: {
                // 未知 type: 尝试作为通知传给 CM, 仅 debug 日志
                if (type.includes("/")) {
                    cm.onNotification(type, payload ?? {});
                }
                logger.debug("Unhandled bridge-event type", { safe: { type } });
                break;
            }
        }
    });

    // ==================== 兼容: window.message (调试/fallback) ====================
    // 保留旧的 window.message 监听, 用于开发调试或非 Wails 环境
    const windowMessageHandler = async (event) => {
        const msg = event.data;
        if (!msg || !msg.type) return;

        // 仅处理少量关键消息 (大多数已由 bridge-event 覆盖)
        switch (msg.type) {
            case "codex-app-server-initialized": {
                queryClient.invalidateQueries();
                logger.info("Codex app server initialized (via window.message)");
                break;
            }
            case "codex-app-server-fatal-error": {
                logger.error("Codex app server fatal error", {
                    safe: { message: msg.error?.message },
                });
                break;
            }
            default:
                break;
        }
    };
    window.addEventListener("message", windowMessageHandler);

    return () => {
        if (unsubscribeBridge) unsubscribeBridge();
        window.removeEventListener("message", windowMessageHandler);
    };
}

export { setupMessageDispatcher };
