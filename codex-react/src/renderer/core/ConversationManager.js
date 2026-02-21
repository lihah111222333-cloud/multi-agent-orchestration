// =============================================================================
// Codex.app — ConversationManager (对话状态管理核心类)
// 提取自: index-formatted.js L35700-37500
//
// 这是 React 前端最核心的类, 管理所有对话的状态:
//   - 创建/恢复对话
//   - 发送消息 (turn/start)
//   - 处理所有服务端通知 (25+种)
//   - 处理审批请求
//   - 流式文本渲染
// =============================================================================

import { produce, genId, normalizeTurn, BatchQueue } from "../utils";
import { bridge } from "../bridge";

class ConversationManager {
    constructor() {
        this.conversations = new Map();          // conversationId → ConversationState
        this.streamingConversations = new Set();  // 正在流式输出的对话
        this.streamRoles = new Map();            // conversationId → { role: "owner"|"follower" }
        this.requestPromises = new Map();        // requestId → { resolve, reject }
        this.turnCompletedListeners = [];
        this.approvalRequestListeners = [];
        this.authStatusCallbacks = [];
        this.mcpLoginCallbacks = [];
        this.frameTextDeltaQueue = new BatchQueue(); // agent消息/plan/reasoning delta
        this.outputDeltaQueue = new BatchQueue();    // 命令输出 delta
        this.personality = null;
    }

    // ======================== 创建对话 ========================

    /**
     * 创建新对话: thread/start → 本地状态初始化 → turn/start
     *
     * @param {Object} options
     * @param {Array} options.input - 用户输入 [{type:"text",text:"..."}, {type:"image",...}]
     * @param {Object} options.collaborationMode - 协作模式 (model/effort/instructions)
     * @param {string[]} options.workspaceRoots - 工作区根目录
     * @param {Object} options.permissions - 审批/沙箱策略
     * @param {string} options.cwd - 工作目录
     * @param {Array} options.attachments - 附件列表
     * @returns {string} conversationId
     */
    async startConversation({ input, collaborationMode, workspaceRoots, permissions, cwd, attachments }) {
        // 1. 构建参数并发送 thread/start
        const params = await this.buildNewConversationParams(
            collaborationMode?.settings.model ?? null,
            cwd,
            permissions
        );
        const result = await this.sendRequest("thread/start", params, { timeoutMs: 60000 });
        const threadId = result.thread.id;

        // 2. 初始化本地对话状态
        this.setConversation({
            id: threadId,
            turns: [],
            requests: [],
            createdAt: Date.now(),
            updatedAt: Date.now(),
            title: null,
            latestModel: result.model,
            latestReasoningEffort: result.reasoningEffort ?? null,
            latestCollaborationMode: collaborationMode ?? {
                mode: "default",
                settings: {
                    model: result.model,
                    reasoning_effort: result.reasoningEffort ?? null,
                    developer_instructions: null,
                },
            },
            hasUnreadTurn: false,
            rolloutPath: result.thread.path ?? "",
            cwd: result.thread.cwd || result.cwd || cwd || workspaceRoots[0],
            gitInfo: result.thread.gitInfo,
            resumeState: "resumed",
            latestTokenUsageInfo: null,
            source: result.thread.source,
        });

        // 3. 标记为流式输出中
        this.markConversationStreaming(threadId);

        // 4. 异步生成标题
        this.generateConversationTitle(threadId, input, cwd);

        // 5. 启动第一轮对话 (发送用户消息)
        await this.startTurn(threadId, {
            cwd: result.thread.cwd || result.cwd || cwd,
            approvalPolicy: permissions.approvalPolicy,
            sandboxPolicy: permissions.sandboxPolicy,
            model: collaborationMode != null ? null : result.model,
            effort: collaborationMode?.settings.reasoning_effort,
            summary: "auto",
            input,
            attachments,
            collaborationMode,
        });

        return threadId;
    }

    // ======================== 发送消息 ========================

    /**
     * 发送用户消息, 启动 AI 回复
     *
     * @param {string} conversationId
     * @param {Object} options - { input, cwd, approvalPolicy, model, effort, ... }
     */
    async startTurn(conversationId, options, flags = {}) {
        const state = this.conversations.get(conversationId);
        if (!state) throw new Error(`Conversation ${conversationId} not found`);

        // 构建 turn 参数
        const turnParams = {
            threadId: conversationId,
            input: options.input,
            cwd: options.cwd ?? state.cwd ?? null,
            approvalPolicy: options.approvalPolicy ?? null,
            sandboxPolicy: options.sandboxPolicy ?? null,
            model: null,
            effort: null,
            summary: options.summary ?? "auto",
            personality: this.personality,
            outputSchema: options.outputSchema ?? null,
            collaborationMode: options.collaborationMode ?? state.latestCollaborationMode ?? null,
            attachments: options.attachments ?? [],
        };

        // 添加 inProgress turn 到本地状态 (乐观更新)
        if (!flags.isSteering) {
            this.updateConversationState(conversationId, (state) => {
                state.turns.push({
                    params: turnParams,
                    turnId: null,                    // 后端分配
                    status: "inProgress",
                    turnStartedAtMs: Date.now(),
                    finalAssistantStartedAtMs: null,
                    error: null,
                    diff: null,
                    items: [],                       // 消息/工具/文件等 TurnItem
                });
                state.updatedAt = Date.now();
            });
        }

        // 发送到后端
        const result = await this.sendRequest("turn/start", turnParams, { timeoutMs: 60000 });
        return result;
    }

    // ======================== 恢复对话 ========================

    /**
     * 恢复已有对话: thread/resume
     */
    async resumeConversation(conversationId, options = {}) {
        this.updateConversationState(conversationId, (s) => { s.resumeState = "resuming"; });
        const params = await this.buildNewConversationParams(null, options.cwd, options.permissions);
        const result = await this.sendRequest("thread/resume", {
            threadId: conversationId,
            history: null,
            path: null,
            model: params.model,
            modelProvider: params.modelProvider,
            cwd: params.cwd,
            approvalPolicy: params.approvalPolicy,
            sandbox: params.sandbox,
            config: params.config,
            baseInstructions: params.baseInstructions,
            developerInstructions: params.developerInstructions,
            personality: params.personality,
        });
        // 恢复历史 turns
        this.updateConversationState(conversationId, (s) => {
            s.turns = result.thread.turns.map(normalizeTurn);
            s.resumeState = "resumed";
            s.cwd = result.thread.cwd || result.cwd;
        });
        this.markConversationStreaming(conversationId);
    }

    // ======================== 通知处理 (核心) ========================

    /**
     * 处理来自 codex 后端的所有通知
     * 每种通知更新不同的对话状态片段
     *
     * @param {string} method - 通知方法名
     * @param {Object} params - 通知参数
     */
    onNotification(method, params) {
        switch (method) {

            // ---- 对话/轮次生命周期 ----

            case "thread/started": {
                // 新线程创建, 初始化状态
                const { thread } = params;
                this.upsertConversationFromThread(thread);
                this.broadcastConversationSnapshot(thread.id);
                break;
            }

            case "turn/started": {
                // AI 开始处理, 绑定 turnId
                const { threadId, turn } = params;
                this.markConversationStreaming(threadId);
                this.updateTurnState(threadId, turn.id, (t) => {
                    t.turnId = turn.id;
                    t.turnStartedAtMs = t.turnStartedAtMs ?? Date.now();
                    t.status = turn.status;
                    t.error = turn.error;
                });
                this.broadcastConversationSnapshot(threadId);
                break;
            }

            case "turn/completed": {
                // AI 回复完成
                this.frameTextDeltaQueue.flushNow(); // 确保所有 delta 已应用
                const { threadId, turn } = params;
                this.unmarkConversationStreaming(threadId);
                this.updateTurnState(threadId, turn.id, (t) => {
                    t.turnId = turn.id;
                    t.status = turn.status;  // "completed" | "cancelled" | "error"
                    t.error = turn.error;
                });
                this.updateConversationState(threadId, (s) => { s.hasUnreadTurn = true; });
                this.broadcastConversationSnapshot(threadId);
                // 触发 turn 完成回调
                const lastMsg = this.getLastAgentMessageForTurn(threadId, turn.id);
                this.turnCompletedListeners.forEach((cb) => cb({
                    conversationId: threadId, turnId: turn.id, lastAgentMessage: lastMsg,
                }));
                break;
            }

            case "turn/diff/updated": {
                const { turnId, diff, threadId } = params;
                this.updateTurnState(threadId, turnId, (t) => { t.diff = diff; });
                break;
            }

            case "turn/plan/updated": {
                const { threadId, turnId, plan, explanation } = params;
                this.updateTurnState(threadId, turnId, (t) => {
                    t.items.push({ id: genId(), type: "todo-list", explanation: explanation ?? null, plan });
                });
                break;
            }

            // ---- Item 生命周期 ----

            case "item/started": {
                // 新 item 开始 (agentMessage, commandExecution, fileChange, etc.)
                const { item, threadId, turnId } = params;
                this.markConversationStreaming(threadId);
                this.updateTurnState(threadId, turnId, (t) => {
                    if (item.type === "agentMessage") t.finalAssistantStartedAtMs = Date.now();
                    this.upsertItem(t, item);
                });
                break;
            }

            case "item/completed": {
                // item 完成
                this.frameTextDeltaQueue.flushNow();
                const { item, threadId, turnId } = params;
                this.updateTurnState(threadId, turnId, (t) => { this.upsertItem(t, item); });
                break;
            }

            // ---- 流式文本 Delta ----

            case "item/agentMessage/delta": {
                // ⭐ AI 回复文本增量 (最频繁的通知)
                const { itemId, delta, threadId, turnId } = params;
                this.frameTextDeltaQueue.enqueue({
                    conversationId: threadId, turnId, itemId,
                    target: { type: "agentMessage" },
                    delta,
                });
                break;
            }

            case "item/plan/delta": {
                const { itemId, delta, threadId, turnId } = params;
                this.frameTextDeltaQueue.enqueue({
                    conversationId: threadId, turnId, itemId,
                    target: { type: "plan" },
                    delta,
                });
                break;
            }

            case "item/reasoning/summaryTextDelta": {
                // Thinking 摘要 delta
                const { itemId, delta, summaryIndex, threadId, turnId } = params;
                this.frameTextDeltaQueue.enqueue({
                    conversationId: threadId, turnId, itemId,
                    target: { type: "reasoningSummary", summaryIndex },
                    delta,
                });
                break;
            }

            case "item/reasoning/textDelta": {
                const { itemId, delta, contentIndex, threadId, turnId } = params;
                this.frameTextDeltaQueue.enqueue({
                    conversationId: threadId, turnId, itemId,
                    target: { type: "reasoningContent", contentIndex },
                    delta,
                });
                break;
            }

            case "item/commandExecution/outputDelta": {
                // 命令输出 delta
                const { itemId, delta, threadId, turnId } = params;
                this.outputDeltaQueue.enqueue({ conversationId: threadId, turnId, itemId, delta });
                break;
            }

            case "item/mcpToolCall/progress":
                break; // MCP 进度日志

            // ---- 账户/配置 ----

            case "account/updated": {
                const { authMode } = params;
                this.authStatusCallbacks.forEach((cb) => cb({ authMethod: authMode ?? null }));
                break;
            }

            case "account/login/completed": {
                const { loginId, success, error } = params;
                if (this.activeLogin?.loginId === loginId) {
                    this.activeLogin.complete({ loginId, success, ...(error ? { error } : {}) });
                }
                break;
            }

            case "account/rateLimits/updated":
                this.notifyRateLimitCallbacks();
                break;

            case "thread/tokenUsage/updated": {
                const { threadId, tokenUsage } = params;
                this.updateConversationState(threadId, (s) => { s.latestTokenUsageInfo = tokenUsage; });
                break;
            }

            case "mcpServer/oauthLogin/completed": {
                const { name, success, error } = params;
                this.mcpLoginCallbacks.forEach((cb) => cb({ name, success, ...(error ? { error } : {}) }));
                break;
            }

            case "error": {
                const { error, willRetry, threadId, turnId } = params;
                this.updateTurnState(threadId, turnId, (t) => {
                    t.items.push({
                        id: genId(), type: "error",
                        message: error.message,
                        willRetry,
                        errorInfo: error.codexErrorInfo,
                        additionalDetails: error.additionalDetails ?? null,
                    });
                });
                break;
            }
        }
    }

    // ======================== 请求处理 (审批) ========================

    /**
     * 处理来自 codex 后端的请求 (需要用户交互)
     */
    onRequest(request) {
        const { id, method, params } = request;

        switch (method) {
            case "item/commandExecution/requestApproval":
            case "item/fileChange/requestApproval": {
                // 添加到 requests 队列, 触发审批 UI
                const threadId = params.threadId;
                this.updateConversationState(threadId, (s) => {
                    s.requests.push(request);
                    s.hasUnreadTurn = true;
                });
                this.approvalRequestListeners.forEach((cb) => cb({
                    conversationId: threadId,
                    requestId: id,
                    kind: method === "item/commandExecution/requestApproval" ? "commandExecution" : "fileChange",
                    reason: params.reason ?? null,
                }));
                break;
            }

            case "item/tool/requestUserInput": {
                const threadId = params.threadId;
                this.updateConversationState(threadId, (s) => {
                    s.requests.push(request);
                    s.hasUnreadTurn = true;
                });
                break;
            }

            case "item/tool/call": {
                // Dynamic tools 不支持, 返回错误
                this.dispatchMessage("mcp-response", {
                    response: {
                        id,
                        result: {
                            contentItems: [{ type: "inputText", text: "Dynamic tools are not supported." }],
                            success: false,
                        },
                    },
                });
                break;
            }
        }
    }

    // ======================== 审批响应 ========================

    /**
     * 回复命令执行审批
     * @param {string} decision - "accept" | "acceptForSession" | "decline"
     */
    replyWithCommandExecutionApprovalDecision(conversationId, requestId, decision) {
        this.dispatchMessage("mcp-response", {
            response: {
                id: requestId,
                result: { decision },
            },
        });
        this.updateConversationState(conversationId, (s) => {
            s.requests = s.requests.filter((r) => r.id !== requestId);
        });
    }

    replyWithFileChangeApprovalDecision(conversationId, requestId, decision) {
        // 同上
        this.replyWithCommandExecutionApprovalDecision(conversationId, requestId, decision);
    }

    // ======================== JSON-RPC 响应处理 ========================

    onResult(requestId, result) {
        const promise = this.requestPromises.get(requestId);
        if (promise) {
            promise.resolve(result);
            this.requestPromises.delete(requestId);
        }
    }

    onError(requestId, error) {
        const promise = this.requestPromises.get(requestId);
        if (promise) {
            promise.reject(new Error(error.message ?? `Request ${requestId} failed`));
            this.requestPromises.delete(requestId);
        }
    }

    // ======================== 流式渲染 ========================

    /**
     * 批量应用文本 delta 到对话状态
     * 使用 requestAnimationFrame 节流以提高渲染性能
     */
    applyFrameTextDeltas(deltas) {
        if (deltas.length === 0) return;

        // 按 conversationId 分组
        const grouped = new Map();
        for (const d of deltas) {
            const arr = grouped.get(d.conversationId) || [];
            arr.push(d);
            grouped.set(d.conversationId, arr);
        }

        // 对每个对话应用 deltas
        for (const [convId, items] of grouped) {
            this.updateConversationState(convId, (state) => {
                for (const item of items) {
                    const turn = this.findTurnForEvent(state, item.turnId);
                    if (!turn) continue;

                    switch (item.target.type) {
                        case "agentMessage": {
                            // AI 回复文本追加
                            const msg = this.findItem(turn, item.itemId, "agentMessage");
                            if (msg) msg.text = (msg.text ?? "") + item.delta;
                            break;
                        }
                        case "plan": {
                            const plan = this.findItem(turn, item.itemId, "plan");
                            if (plan) plan.text = (plan.text ?? "") + item.delta;
                            break;
                        }
                        case "reasoningSummary": {
                            const reasoning = this.findItem(turn, item.itemId, "reasoning");
                            if (reasoning) reasoning.summary[item.target.summaryIndex] += item.delta;
                            break;
                        }
                        case "reasoningContent": {
                            const reasoning = this.findItem(turn, item.itemId, "reasoning");
                            if (reasoning) reasoning.content[item.target.contentIndex] += item.delta;
                            break;
                        }
                    }
                }
            });
        }
    }

    // ======================== 辅助方法 ========================

    async sendRequest(method, params, options = {}) {
        const id = `${method}:${crypto.randomUUID()}`;
        return new Promise((resolve, reject) => {
            this.requestPromises.set(id, { resolve, reject });
            this.dispatchMessage("mcp-request-from-renderer", {
                request: { id, method, params },
            });
        });
    }

    updateConversationState(conversationId, updater) {
        const state = this.conversations.get(conversationId);
        if (!state) return;
        // 使用 Immer produce 进行不可变更新
        const next = produce(state, updater);
        this.conversations.set(conversationId, next);
        this.notifyConversationCallbacks(conversationId);
    }

    updateTurnState(conversationId, turnId, updater) {
        this.updateConversationState(conversationId, (state) => {
            const turn = state.turns.find((t) => t.turnId === turnId) ?? state.turns[state.turns.length - 1];
            if (turn) updater(turn);
        });
    }

    // ======================== 外部引用方法 ========================
    // (MessageDispatcher.js / App.js 中引用)

    /**
     * 获取对话状态
     * 被 MessageDispatcher.js 中的 desktop-notification-action 处理引用
     *
     * @param {string} conversationId
     * @returns {Conversation|undefined}
     */
    getConversation(conversationId) {
        return this.conversations.get(conversationId);
    }

    /**
     * 设置对话标题
     * 被 MessageDispatcher.js 的 thread-title-updated 处理引用
     *
     * @param {string} conversationId
     * @param {string} title
     */
    setThreadTitle(conversationId, title) {
        this.updateConversationState(conversationId, (s) => {
            s.title = title;
        });
    }

    /**
     * 注册 Auth 状态变化回调
     * 被 App.js 的 useEffect 引用
     *
     * @param {Function} callback - (status: { authMethod }) => void
     */
    addAuthStatusCallback(callback) {
        this.authStatusCallbacks.push(callback);
    }

    /**
     * 登出 — 清理所有状态
     * 被 MessageDispatcher.js 的 log-out 处理引用
     */
    async logout() {
        this.conversations.clear();
        this.streamingConversations.clear();
        this.streamRoles.clear();
        this.requestPromises.clear();
        this.turnCompletedListeners = [];
        this.approvalRequestListeners = [];
        this.authStatusCallbacks = [];
        this.mcpLoginCallbacks = [];
    }

    // ======================== 内部辅助方法 ========================

    /**
     * setConversation — 将对话状态存入 Map
     * @param {Conversation} conversation
     */
    setConversation(conversation) {
        this.conversations.set(conversation.id, conversation);
        this.notifyConversationCallbacks(conversation.id);
    }

    /**
     * markConversationStreaming — 标记对话为流式输出中
     * @param {string} conversationId
     */
    markConversationStreaming(conversationId) {
        this.streamingConversations.add(conversationId);
        this._notifyStreamingCallbacks(conversationId, true);
    }

    /**
     * unmarkConversationStreaming — 取消流式输出标记
     * 在 turn/completed 时调用
     * @param {string} conversationId
     */
    unmarkConversationStreaming(conversationId) {
        this.streamingConversations.delete(conversationId);
        this._notifyStreamingCallbacks(conversationId, false);
    }

    /**
     * isStreaming — 检查对话是否处于流式输出中
     * 被 useStreamingState hook 调用
     *
     * @param {string} conversationId
     * @returns {boolean}
     */
    isStreaming(conversationId) {
        return this.streamingConversations.has(conversationId);
    }

    /**
     * addStreamingCallback — 订阅流式状态变化
     * 被 useStreamingState hook 调用
     *
     * @param {string} conversationId
     * @param {Function} callback - (isStreaming: boolean) => void
     * @returns {Function} unsubscribe
     */
    addStreamingCallback(conversationId, callback) {
        if (!this._streamingCallbacks) this._streamingCallbacks = new Map();
        let cbs = this._streamingCallbacks.get(conversationId);
        if (!cbs) {
            cbs = new Set();
            this._streamingCallbacks.set(conversationId, cbs);
        }
        cbs.add(callback);
        return () => cbs.delete(callback);
    }

    /**
     * _notifyStreamingCallbacks — 通知流式状态变化订阅者
     * @param {string} conversationId
     * @param {boolean} isStreaming
     */
    _notifyStreamingCallbacks(conversationId, isStreaming) {
        if (!this._streamingCallbacks) return;
        const cbs = this._streamingCallbacks.get(conversationId);
        if (cbs) cbs.forEach((cb) => cb(isStreaming));
    }

    /**
     * buildNewConversationParams — 构建 thread/start 或 thread/resume 参数
     * 混淆名: 从 config/read 获取当前 agent 配置
     *
     * @param {string|null} model
     * @param {string|null} cwd
     * @param {Object|null} permissions
     * @returns {Object} 构建好的参数
     */
    async buildNewConversationParams(model, cwd, permissions) {
        // 从后端读取当前配置
        const config = await this.sendRequest("config/read", {});
        return {
            model: model ?? config.model ?? "o4-mini",
            modelProvider: config.modelProvider ?? null,
            cwd: cwd ?? config.cwd ?? null,
            approvalPolicy: permissions?.approvalPolicy ?? config.approvalPolicy ?? "on-failure",
            sandbox: permissions?.sandboxPolicy ?? config.sandbox ?? null,
            config: config.config ?? null,
            baseInstructions: config.baseInstructions ?? null,
            developerInstructions: config.developerInstructions ?? null,
            personality: this.personality ?? config.personality ?? null,
        };
    }

    /**
     * generateConversationTitle — 异步生成对话标题
     * 使用 thread/generateTitle 请求
     *
     * @param {string} conversationId
     * @param {InputItem[]} input
     * @param {string|null} cwd
     */
    async generateConversationTitle(conversationId, input, cwd) {
        try {
            const result = await this.sendRequest("thread/generateTitle", {
                threadId: conversationId,
                input,
                cwd,
            });
            if (result?.title) {
                this.setThreadTitle(conversationId, result.title);
            }
        } catch {
            // 静默失败, 标题生成不影响核心功能
        }
    }

    /**
     * upsertConversationFromThread — 从 thread 对象创建或更新对话状态
     * @param {Object} thread - 后端返回的 thread 对象
     */
    upsertConversationFromThread(thread) {
        const existing = this.conversations.get(thread.id);
        if (existing) {
            this.updateConversationState(thread.id, (s) => {
                s.title = thread.title ?? s.title;
                s.cwd = thread.cwd ?? s.cwd;
                s.gitInfo = thread.gitInfo ?? s.gitInfo;
            });
        } else {
            this.setConversation({
                id: thread.id,
                turns: (thread.turns ?? []).map(normalizeTurn),
                requests: [],
                createdAt: thread.createdAt ? new Date(thread.createdAt).getTime() : Date.now(),
                updatedAt: Date.now(),
                title: thread.title ?? null,
                latestModel: thread.model ?? "o4-mini",
                latestReasoningEffort: null,
                latestCollaborationMode: null,
                hasUnreadTurn: false,
                rolloutPath: thread.path ?? "",
                cwd: thread.cwd ?? "",
                gitInfo: thread.gitInfo ?? null,
                resumeState: "resumed",
                latestTokenUsageInfo: null,
                source: thread.source ?? null,
            });
        }
    }

    /**
     * broadcastConversationSnapshot — 通知所有订阅者对话状态变化
     * @param {string} conversationId
     */
    broadcastConversationSnapshot(conversationId) {
        this.notifyConversationCallbacks(conversationId);
    }

    /**
     * getLastAgentMessageForTurn — 获取指定 turn 中最后一条 AI 消息
     * @param {string} conversationId
     * @param {string} turnId
     * @returns {string|null}
     */
    getLastAgentMessageForTurn(conversationId, turnId) {
        const state = this.conversations.get(conversationId);
        if (!state) return null;
        const turn = state.turns.find((t) => t.turnId === turnId);
        if (!turn) return null;
        const agentMessages = turn.items.filter((item) => item.type === "agentMessage");
        return agentMessages.length > 0 ? agentMessages[agentMessages.length - 1].text : null;
    }

    /**
     * upsertItem — 插入或更新 turn 中的 item
     * @param {Turn} turn - Immer draft
     * @param {TurnItem} item - 要 upsert 的 item
     */
    upsertItem(turn, item) {
        const idx = turn.items.findIndex((i) => i.id === item.id);
        if (idx >= 0) {
            // 用新 item 合并 (保留现有 text delta)
            turn.items[idx] = { ...turn.items[idx], ...item };
        } else {
            turn.items.push({ ...item });
        }
    }

    /**
     * notifyRateLimitCallbacks — 通知限速变化订阅者
     */
    notifyRateLimitCallbacks() {
        if (!this._rateLimitCallbacks) return;
        this._rateLimitCallbacks.forEach((cb) => cb());
    }

    /**
     * addRateLimitCallback — 订阅限速通知
     * @param {Function} callback - () => void
     * @returns {Function} unsubscribe
     */
    addRateLimitCallback(callback) {
        if (!this._rateLimitCallbacks) this._rateLimitCallbacks = new Set();
        this._rateLimitCallbacks.add(callback);
        return () => this._rateLimitCallbacks.delete(callback);
    }

    /**
     * dispatchMessage — 发送消息到 Electron Main 进程
     * 底层使用 bridge.dispatchMessage()
     *
     * @param {string} type - 消息类型
     * @param {Object} payload - 消息数据
     */
    dispatchMessage(type, payload = {}) {
        bridge.dispatchMessage(type, payload);
    }

    /**
     * notifyConversationCallbacks — 通知对话状态变化订阅者
     * 被 useConversation hook 中的 subscribe 使用
     *
     * @param {string} conversationId
     */
    notifyConversationCallbacks(conversationId) {
        // 使用 Map 存储订阅者
        if (!this._conversationCallbacks) this._conversationCallbacks = new Map();
        const cbs = this._conversationCallbacks.get(conversationId);
        if (cbs) cbs.forEach((cb) => cb(this.conversations.get(conversationId)));
    }

    /**
     * 订阅对话状态变化 (被 useConversation hook 调用)
     *
     * @param {string} conversationId
     * @param {Function} callback
     * @returns {Function} unsubscribe
     */
    subscribeToConversation(conversationId, callback) {
        if (!this._conversationCallbacks) this._conversationCallbacks = new Map();
        let cbs = this._conversationCallbacks.get(conversationId);
        if (!cbs) {
            cbs = new Set();
            this._conversationCallbacks.set(conversationId, cbs);
        }
        cbs.add(callback);
        return () => cbs.delete(callback);
    }

    /**
     * findTurnForEvent — 根据 turnId 查找 turn (或返回最新 turn)
     * @param {ConversationState} state - Immer draft
     * @param {string|null} turnId
     * @returns {Turn|undefined}
     */
    findTurnForEvent(state, turnId) {
        if (turnId) {
            return state.turns.find((t) => t.turnId === turnId) ?? state.turns[state.turns.length - 1];
        }
        return state.turns[state.turns.length - 1];
    }

    /**
     * findItem — 在 turn 中查找指定 id 和 type 的 item
     * @param {Turn} turn - Immer draft
     * @param {string} itemId
     * @param {string} type
     * @returns {TurnItem|undefined}
     */
    findItem(turn, itemId, type) {
        return turn.items.find((i) => i.id === itemId && i.type === type);
    }

    /**
     * applyOutputDeltas — 批量应用命令输出 delta
     * 类似 applyFrameTextDeltas, 但针对 commandExecution 的 aggregatedOutput
     *
     * @param {Array} deltas
     */
    applyOutputDeltas(deltas) {
        if (deltas.length === 0) return;
        const grouped = new Map();
        for (const d of deltas) {
            const arr = grouped.get(d.conversationId) || [];
            arr.push(d);
            grouped.set(d.conversationId, arr);
        }
        for (const [convId, items] of grouped) {
            this.updateConversationState(convId, (state) => {
                for (const item of items) {
                    const turn = this.findTurnForEvent(state, item.turnId);
                    if (!turn) continue;
                    const cmd = turn.items.find((i) => i.id === item.itemId && i.type === "commandExecution");
                    if (cmd) cmd.aggregatedOutput = (cmd.aggregatedOutput ?? "") + item.delta;
                }
            });
        }
    }
}

export { ConversationManager };
