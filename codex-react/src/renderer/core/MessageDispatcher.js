// =============================================================================
// Codex.app — 全局消息分发器 (MessageDispatcher)
// 提取自: index-formatted.js L251400-251600
//
// 功能: 监听 window "message" 事件, 将 Electron Main 发来的消息
//       按 type 分发给不同处理器
//
// 这是 Preload → React 的入口, 所有后端消息都经过这里
//
// 分发目标:
//   - ConversationManager — MCP 协议 + 对话管理
//   - UIEventBus          — UI 控制 (侧边栏/终端/字体/剪贴板)
//   - TerminalManager     — 终端数据流
//   - AppStateManager     — 应用全局状态
//   - QueryClient         — React Query 缓存失效
//   - navigate()          — React Router 导航
// =============================================================================

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

    window.addEventListener("message", async (event) => {
        const msg = event.data;
        if (!msg || !msg.type) return;

        switch (msg.type) {

            // ==================== MCP 协议消息 ====================

            case "mcp-response": {
                // JSON-RPC 响应 → 回调 ConversationManager
                if (msg.message.result) {
                    cm.onResult(msg.message.id, msg.message.result);
                } else if (msg.message.error) {
                    cm.onError(msg.message.id, msg.message.error);
                }
                break;
            }

            case "mcp-notification": {
                // JSON-RPC 通知 → ConversationManager.onNotification
                if (msg.method === "codex/event/skills_update_available") {
                    queryClient.invalidateQueries({ queryKey: ["skills"] });
                }
                cm.onNotification(msg.method, msg.params ?? {});
                break;
            }

            case "mcp-request": {
                // 服务端请求 (如审批) → ConversationManager.onRequest
                cm.onRequest(msg.request);
                break;
            }

            // ==================== 应用状态 ====================

            case "codex-app-server-initialized": {
                // codex 后端就绪 — 可刷新依赖后端的查询
                queryClient.invalidateQueries();
                logger.info("Codex app server initialized");
                break;
            }

            case "codex-app-server-fatal-error": {
                // codex 后端致命错误
                logger.error("Codex app server fatal error", {
                    safe: { message: msg.error?.message },
                });
                break;
            }

            // ==================== 对话管理 ====================

            case "thread-title-updated": {
                cm.setThreadTitle(msg.conversationId, msg.title);
                break;
            }

            case "tasks-reload-requested": {
                queryClient.invalidateQueries({ queryKey: ["tasks"] });
                break;
            }

            // ==================== 导航 ====================

            case "navigate-to-route": {
                // 从 Main 进程触发页面导航
                navigate(msg.path);
                break;
            }

            case "navigate-back": {
                window.history.back();
                break;
            }

            case "navigate-forward": {
                window.history.forward();
                break;
            }

            // ==================== 桌面通知操作 ====================

            case "desktop-notification-action": {
                const { requestId, actionType, conversationId } = msg;
                const conv = conversationId ? cm.getConversation(conversationId) : null;

                if (actionType === "open") {
                    // 打开对话
                    navigate(conv ? `/local/${conversationId}` : `/remote/${conversationId}`);
                    break;
                }

                // 审批操作
                if (requestId && conversationId && conv) {
                    const decision =
                        actionType === "approve" ? "accept" :
                            actionType === "approve-for-session" ? "acceptForSession" :
                                actionType === "decline" ? "decline" : null;

                    if (decision) {
                        const req = conv.requests.find((r) => r.id === requestId);
                        if (req?.method === "item/commandExecution/requestApproval") {
                            cm.replyWithCommandExecutionApprovalDecision(conversationId, requestId, decision);
                        } else if (req?.method === "item/fileChange/requestApproval") {
                            cm.replyWithFileChangeApprovalDecision(conversationId, requestId, decision);
                        }
                    }
                }
                break;
            }

            // ==================== 收件箱/自动化 ====================

            case "automation-runs-updated":
            case "new-chat": {
                queryClient.invalidateQueries({ queryKey: ["inbox-items"] });
                queryClient.invalidateQueries({ queryKey: ["pending-automation-runs"] });
                break;
            }

            // ==================== UI 控制 → UIEventBus ====================

            case "open-command-menu": {
                uiEventBus.emit("open-command-menu", msg);
                break;
            }

            case "toggle-sidebar": {
                uiEventBus.emit("toggle-sidebar", msg);
                break;
            }

            case "toggle-terminal": {
                uiEventBus.emit("toggle-terminal", msg);
                break;
            }

            case "toggle-diff-panel": {
                uiEventBus.emit("toggle-diff-panel", msg);
                break;
            }

            case "find-in-thread": {
                uiEventBus.emit("find-in-thread", msg);
                break;
            }

            case "previous-thread": {
                uiEventBus.emit("previous-thread", msg);
                break;
            }

            case "next-thread": {
                uiEventBus.emit("next-thread", msg);
                break;
            }

            case "step-font-size": {
                uiEventBus.emit("step-font-size", msg);
                break;
            }

            case "copy-conversation-path": {
                uiEventBus.emit("copy-conversation-path", msg);
                break;
            }

            case "copy-working-directory": {
                uiEventBus.emit("copy-working-directory", msg);
                break;
            }

            case "copy-session-id": {
                uiEventBus.emit("copy-session-id", msg);
                break;
            }

            case "copy-deeplink": {
                uiEventBus.emit("copy-deeplink", msg);
                break;
            }

            case "toggle-thread-pin": {
                uiEventBus.emit("toggle-thread-pin", msg);
                queryClient.invalidateQueries({ queryKey: ["threads/list"] });
                break;
            }

            case "rename-thread": {
                uiEventBus.emit("rename-thread", msg);
                break;
            }

            case "archive-thread": {
                uiEventBus.emit("archive-thread", msg);
                queryClient.invalidateQueries({ queryKey: ["tasks"] });
                break;
            }

            case "pinned-threads-updated": {
                uiEventBus.emit("pinned-threads-updated", msg);
                queryClient.invalidateQueries({ queryKey: ["threads/list"] });
                break;
            }

            // ==================== 终端 → TerminalManager ====================

            case "terminal-data": {
                terminalManager.handleTerminalData(msg);
                break;
            }

            case "terminal-exit": {
                terminalManager.handleTerminalExit(msg);
                break;
            }

            case "terminal-error": {
                terminalManager.handleTerminalError(msg);
                break;
            }

            case "terminal-init-log": {
                terminalManager.handleTerminalInitLog(msg);
                break;
            }

            case "terminal-attached": {
                terminalManager.handleTerminalAttached(msg);
                break;
            }

            // ==================== 跨进程 IPC ====================

            case "ipc-broadcast": {
                if (msg.method === "thread-archived" || msg.method === "thread-unarchived") {
                    queryClient.invalidateQueries({ queryKey: ["tasks"] });
                }
                if (msg.method === "thread-pinned" || msg.method === "thread-unpinned") {
                    queryClient.invalidateQueries({ queryKey: ["threads/list"] });
                }
                break;
            }

            // ==================== 应用全局状态 → AppStateManager ====================

            case "log-out": {
                await cm.logout();
                navigate("/login");
                break;
            }

            case "app-update-ready-changed": {
                appStateManager.handleAppUpdateReadyChanged(msg);
                break;
            }

            case "window-fullscreen-changed": {
                appStateManager.handleWindowFullscreenChanged(msg);
                break;
            }

            case "electron-window-focus-changed": {
                appStateManager.handleWindowFocusChanged(msg);
                break;
            }

            case "custom-prompts-updated": {
                appStateManager.handleCustomPromptsUpdated(msg);
                break;
            }

            case "persisted-atom-sync": {
                appStateManager.handlePersistedAtomSync(msg);
                break;
            }

            case "persisted-atom-updated": {
                appStateManager.handlePersistedAtomUpdated(msg);
                break;
            }

            case "trace-recording-state-changed": {
                appStateManager.handleTraceRecordingStateChanged(msg);
                break;
            }

            case "trace-recording-uploaded": {
                appStateManager.handleTraceRecordingUploaded(msg);
                break;
            }

            case "is-copilot-api-available-updated": {
                appStateManager.handleCopilotApiAvailableUpdated(msg);
                break;
            }

            case "implement-todo": {
                appStateManager.handleImplementTodo(msg);
                break;
            }

            case "add-context-file": {
                appStateManager.handleAddContextFile(msg);
                break;
            }

            case "thread-overlay-open-current": {
                appStateManager.handleThreadOverlayOpenCurrent(msg);
                break;
            }

            case "toggle-query-devtools": {
                appStateManager.handleToggleQueryDevtools(msg);
                break;
            }

            case "workspace-root-options-updated": {
                appStateManager.handleWorkspaceRootOptionsUpdated(msg, queryClient);
                break;
            }

            case "active-workspace-roots-updated": {
                appStateManager.handleActiveWorkspaceRootsUpdated(msg, queryClient);
                break;
            }

            default: {
                // 未知消息类型 — 仅 debug 日志
                logger.debug("Unhandled message type", { safe: { type: msg.type } });
                break;
            }
        }
    });
}

export { setupMessageDispatcher };
