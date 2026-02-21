// =============================================================================
// Codex.app — 全局消息分发器 (MessageDispatcher)
// 提取自: index-formatted.js L251400-251600
//
// 功能: 监听 window "message" 事件, 将 Electron Main 发来的消息
//       按 type 分发给不同处理器
//
// 这是 Preload → React 的入口, 所有后端消息都经过这里
// =============================================================================

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
                // codex 后端就绪
                break;
            }

            case "codex-app-server-fatal-error": {
                // codex 后端致命错误
                handleFatalError(msg);
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

            case "navigate-back":
            case "navigate-forward":
                break;

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

            // ==================== 应用更新 ====================

            case "app-update-ready-changed":
                break;

            // ==================== UI 控制 ====================

            case "open-command-menu":
            case "toggle-sidebar":
            case "toggle-terminal":
            case "toggle-diff-panel":
            case "find-in-thread":
            case "previous-thread":
            case "next-thread":
            case "step-font-size":
            case "copy-conversation-path":
            case "copy-working-directory":
            case "copy-session-id":
            case "copy-deeplink":
            case "toggle-thread-pin":
            case "rename-thread":
            case "archive-thread":
            case "pinned-threads-updated":
                break;

            // ==================== 终端 ====================

            case "terminal-data":
            case "terminal-exit":
            case "terminal-error":
            case "terminal-init-log":
            case "terminal-attached":
                break;

            // ==================== 跨进程 IPC ====================

            case "ipc-broadcast": {
                if (msg.method === "thread-archived" || msg.method === "thread-unarchived") {
                    queryClient.invalidateQueries({ queryKey: ["tasks"] });
                }
                break;
            }

            // ==================== 其他 ====================

            case "log-out": {
                await cm.logout();
                navigate("/login");
                break;
            }

            case "window-fullscreen-changed":
            case "electron-window-focus-changed":
            case "custom-prompts-updated":
            case "persisted-atom-sync":
            case "persisted-atom-updated":
            case "trace-recording-state-changed":
            case "trace-recording-uploaded":
            case "is-copilot-api-available-updated":
            case "implement-todo":
            case "add-context-file":
            case "thread-overlay-open-current":
            case "toggle-query-devtools":
            case "workspace-root-options-updated":
            case "active-workspace-roots-updated":
                break;
        }
    });
}

module.exports = { setupMessageDispatcher };
