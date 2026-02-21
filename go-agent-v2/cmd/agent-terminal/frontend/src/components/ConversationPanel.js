// =============================================================================
// Codex.app — ConversationPanel 对话面板 (共享组件)
// 混淆名: rO
// 提取自: index-formatted.js L~200000
//
// 功能: 对话渲染容器 (被 ChatPage, ThreadOverlayPage, RemoteTaskPage 共享)
//   - 轮次列表渲染 (8 种 item 类型)
//   - 流式文本追加
//   - 审批弹窗
//   - 自动滚动
//   - 可配置: 轻量模式 / 完整模式
//
// Turn Item 类型 (来自 ConversationManager.upsertItem):
//   - agentMessage:      AI 回复文本
//   - commandExecution:  终端命令 (.command / .aggregatedOutput / .exitCode)
//   - fileChange:        文件修改 (.file / .files)
//   - mcpToolCall:       MCP 工具调用 (.name / .args)
//   - reasoning:         推理步骤 (.summary[] / .content[])
//   - plan:              计划文本 (.text)
//   - todo-list:         待办列表 (.plan / .explanation)
//   - error:             错误信息 (.message / .willRetry / .errorInfo)
// =============================================================================

import { useRef, useEffect, useState } from "react";
import { useConversation } from "../hooks/useConversation";
import { MarkdownRenderer } from "./MarkdownRenderer";
import { AnsiRenderer } from "./AnsiRenderer";
import { StreamingIndicator, TurnError, ContextCompaction } from "./StreamingIndicator";
import { Composer } from "./Composer";
import { ChevronIcon } from "./Icons";
import { bridge } from "../bridge";

/**
 * ConversationPanel — 可复用对话面板
 * 混淆名: rO
 */
export function ConversationPanel({
    conversationId,
    header,
    shouldResume = false,
    allowMissingConversation = false,
    showExternalFooter = true,
    isLightweight = false,
    className = "",
}) {
    const conversation = useConversation(conversationId);
    const scrollRef = useRef(null);

    // 自动滚动到底部
    useEffect(() => {
        const el = scrollRef.current;
        if (el) {
            el.scrollTop = el.scrollHeight;
        }
    }, [conversation?.turns]);

    if (!conversation && !allowMissingConversation) {
        return (
            <div className="flex h-full items-center justify-center text-token-description-foreground text-sm">
                Conversation not found
            </div>
        );
    }

    const turns = conversation?.turns ?? [];
    const currentTurn = turns[turns.length - 1];
    const isStreaming = currentTurn?.status === "inProgress" || currentTurn?.status === "in_progress";

    return (
        <div className={`conversation-panel flex flex-col h-full ${className}`}>
            {header}

            {/* 对话内容区 */}
            <div ref={scrollRef} className="flex-1 overflow-y-auto">
                <div className={`mx-auto ${isLightweight ? "max-w-full px-3" : "max-w-[800px] px-6"} py-6`}>
                    {/* 用户消息 (从 turn.params.input 渲染) */}
                    {turns.map((turn, idx) => (
                        <TurnRenderer key={turn.turnId || idx} turn={turn} isLightweight={isLightweight} />
                    ))}

                    {isStreaming && <StreamingIndicator />}
                </div>
            </div>

            {/* 底部 Composer */}
            {showExternalFooter && !isLightweight && (
                <Composer conversationId={conversationId} onSend={() => { }} />
            )}
        </div>
    );
}

// ===================== TurnRenderer =====================

function TurnRenderer({ turn, isLightweight }) {
    if (!turn) return null;

    // 从 turn.params.input 渲染用户消息
    const userInput = turn.params?.input;
    const userText = Array.isArray(userInput)
        ? userInput.filter(i => i.type === "text").map(i => i.text).join("\n")
        : (typeof userInput === "string" ? userInput : null);

    return (
        <div className="turn-container mb-2">
            {/* 用户消息 */}
            {userText && <UserMessage message={userText} />}

            {/* Turn items */}
            {(turn.items ?? []).map((item, idx) => (
                <TurnItemRenderer key={item.id || idx} item={item} isLightweight={isLightweight} />
            ))}

            {turn.error && <TurnError error={turn.error} />}
        </div>
    );
}

// ===================== TurnItemRenderer =====================

function TurnItemRenderer({ item, isLightweight }) {
    if (!item) return null;

    switch (item.type) {
        // ---- 用户消息 (直接推送的) ----
        case "user_message":
            return <UserMessage message={item.text} />;

        // ---- AI 回复 ----
        case "agentMessage":
        case "assistant_message":
            return <AgentMessageRenderer item={item} />;

        // ---- 终端命令 ----
        case "commandExecution":
            return <CommandExecutionRenderer item={item} />;

        // ---- 文件变更 ----
        case "fileChange":
        case "file_change":
            return <FileChangeRenderer item={item} isLightweight={isLightweight} />;

        // ---- MCP 工具调用 ----
        case "mcpToolCall":
        case "tool_call":
            return <McpToolCallRenderer item={item} />;

        // ---- 推理/思考 ----
        case "reasoning":
            return <ReasoningRenderer item={item} />;

        // ---- 计划 ----
        case "plan":
            return <PlanRenderer item={item} />;

        // ---- 待办列表 ----
        case "todo-list":
            return <TodoListRenderer item={item} />;

        // ---- 错误 ----
        case "error":
            return <ErrorRenderer item={item} />;

        // ---- 旧兼容类型 ----
        case "terminal_command":
            return <LegacyTerminalCommand command={item.command} />;
        case "terminal_output":
            return <LegacyTerminalOutput output={item.output} />;
        case "tool_result":
            return <ToolResultRenderer result={item} />;
        case "approval_request":
            return <ApprovalRequestRenderer request={item} />;
        case "context_compaction":
            return <ContextCompaction completed={item.completed} />;
        case "system_message":
            return (
                <div className="py-2 text-xs text-token-description-foreground text-center">
                    {item.text}
                </div>
            );
        default:
            return null;
    }
}

// ===================== 1. UserMessage =====================

export function UserMessage({ message }) {
    return (
        <div className="flex justify-end py-2.5">
            <div className="flex max-w-[88%] flex-col items-end gap-1">
                <span className="px-1 text-[11px] font-medium tracking-wide text-token-description-foreground/80">
                    You
                </span>
                <div className="max-w-full rounded-2xl rounded-br-md border border-token-border/40 bg-token-foreground/10 px-4 py-2.5">
                    <span className="text-sm whitespace-pre-wrap break-words leading-relaxed">{message}</span>
                </div>
            </div>
        </div>
    );
}

// ===================== 2. AgentMessage =====================

function AgentMessageRenderer({ item }) {
    return (
        <div className="py-2.5">
            <div className="flex justify-start">
                <div className="flex w-full max-w-[95%] flex-col items-start gap-1">
                    <span className="px-1 text-[11px] font-medium tracking-wide text-token-description-foreground/80">
                        Codex
                    </span>
                    <div className="w-full rounded-2xl rounded-tl-md border border-token-border/50 bg-token-bg-secondary/70 px-4 py-3">
                        <MarkdownRenderer className="text-sm leading-relaxed break-words [&_.code-block]:rounded-lg [&_.code-block]:border [&_.code-block]:border-token-border/50 [&_.code-block]:bg-token-editor-background/80 [&_.inline-code]:bg-token-editor-background [&_p:first-child]:mt-0 [&_p:last-child]:mb-0">
                            {item.text}
                        </MarkdownRenderer>
                    </div>
                </div>
            </div>
        </div>
    );
}

function CommandExecutionRenderer({ item }) {
    const [expanded, setExpanded] = useState(true);
    const hasOutput = item.aggregatedOutput && item.aggregatedOutput.length > 0;
    const exitCode = item.exitCode;
    const isError = exitCode != null && exitCode !== 0;

    const [showOutput, setShowOutput] = useState(expanded);

    return (
        <div className="py-2.5">
            <div
                className="cursor-interaction group flex items-start gap-1 px-0 py-0"
                onClick={() => {
                    setExpanded(!expanded);
                    if (!expanded) setShowOutput(true);
                }}
            >
                <div className="min-w-0 flex items-center gap-1">
                    <div className="text-token-description-foreground group-hover:text-token-foreground flex min-w-0 items-center gap-1 text-size-chat">
                        <span className="inline-flex min-w-0 max-w-full">
                            <span className="truncate bg-token-bg-secondary rounded px-1.5 font-mono text-xs py-0.5">
                                {item.command || "Running command..."}
                            </span>
                        </span>

                        {exitCode != null && (
                            <div className="flex items-center gap-1.5">
                                <span className={`block size-1.5 rounded-full ${isError ? "bg-token-charts-red/70" : "bg-token-charts-green/70"}`}></span>
                            </div>
                        )}

                        <span className={`inline-chevron flex-shrink-0 text-token-input-placeholder-foreground transition-opacity duration-200 opacity-0 group-hover:opacity-100 ${expanded ? "opacity-100" : ""}`}>
                            <C0 className={`icon-2xs text-current transition-transform duration-300 ${expanded ? "rotate-90" : ""}`} />
                        </span>
                    </div>
                </div>
            </div>

            {/* Output */}
            {expanded && hasOutput && (
                <div className="pt-2">
                    <div className="bg-token-text-code-block-background border-token-input-background group flex flex-col overflow-hidden rounded-lg border">
                        <div className="font-mono text-[13px] leading-relaxed overflow-x-auto break-all max-h-[350px] overflow-y-auto px-3 md:px-4 py-2 md:py-3">
                            <AnsiRenderer>{item.aggregatedOutput}</AnsiRenderer>
                        </div>

                        {!item.isInProgress && (
                            <div className="text-token-input-placeholder-foreground flex items-center gap-2 px-2.5 pb-1 pt-0.5 text-size-chat">
                                {isError ? (
                                    <span className="ml-auto">Exit code {exitCode}</span>
                                ) : (
                                    <span className="ml-auto flex items-center gap-1">
                                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="icon-xxs">
                                            <polyline points="20 6 9 17 4 12"></polyline>
                                        </svg>
                                        Success
                                    </span>
                                )}
                            </div>
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}

// ===================== 3b. Local C0 / SVG Helpers =====================
function C0({ className }) {
    return (
        <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
            <polyline points="9 18 15 12 9 6"></polyline>
        </svg>
    );
}

function FileChangeRenderer({ item }) {
    const changes = item.changes || {};
    const hasChanges = Object.keys(changes).length > 0;
    const isPending = item.success == null;
    const isSuccess = item.success === true;
    const status = isPending ? "pending" : isSuccess ? "applied" : "rejected";

    if (!hasChanges) return null;

    return (
        <div className="py-2.5">
            <div className="flex flex-col gap-[8px]">
                {Object.entries(changes).map(([path, change]) => (
                    <FileChangeItem key={path} path={path} change={change} status={status} />
                ))}
            </div>
        </div>
    );
}

function FileChangeItem({ path, change, status }) {
    const [expanded, setExpanded] = useState(false);
    const isPending = status === "pending";
    const isRejected = status === "rejected";

    // Build the status badge
    let badge = null;
    if (change.type === "add") {
        if (isRejected) badge = <span className="text-token-description-foreground">Rejected</span>;
        else badge = <span className="text-token-description-foreground">{isPending ? "Creating" : "Created"}</span>;
    } else if (change.type === "delete") {
        if (isRejected) badge = <span className="text-token-description-foreground">Rejected</span>;
        else badge = <span className="text-token-description-foreground">{isPending ? "Deleting" : "Deleted"}</span>;
    } else {
        if (isRejected) badge = <span className="text-token-description-foreground">Rejected</span>;
        else badge = <span className="text-token-description-foreground">{isPending ? "Editing" : "Edited"}</span>;
    }

    // Header label
    let headerLabel = null;
    if (!isPending && !isRejected) {
        if (change.type === "add") headerLabel = "Created file";
        else if (change.type === "delete") headerLabel = "Deleted file";
        else headerLabel = "Edited file";
    }

    const isHoverable = true;

    return (
        <div className={`flex flex-col overflow-clip transition-[box-shadow] duration-300 ${isPending ? "rounded-xl" : "rounded-lg"}`}>
            <div
                className="cursor-interaction group flex items-center justify-between gap-1 text-ellipsis text-size-chat px-0 py-0"
                onClick={() => setExpanded(!expanded)}
            >
                <div className="text-token-description-foreground flex min-w-0 items-center gap-1 text-size-chat">
                    {!isPending && (
                        <span className="text-token-description-foreground group-hover:text-token-foreground select-text">
                            {headerLabel || badge}
                        </span>
                    )}

                    {headerLabel && (
                        <div className="flex items-center gap-1.5 ml-1">
                            {change.type === "delete" ? (
                                <span className="bg-token-charts-red/70 block size-1.5 rounded-full"></span>
                            ) : change.type === "add" ? (
                                <span className="bg-token-charts-blue/70 block size-1.5 rounded-full"></span>
                            ) : (
                                <span className="bg-token-charts-yellow/70 block size-1.5 rounded-full"></span>
                            )}
                        </div>
                    )}

                    {!headerLabel && (
                        <button type="button" className="text-token-text-link-foreground cursor-interaction max-w-full select-text truncate text-start hover:underline" onClick={(e) => e.stopPropagation()}>
                            {path.split("/").pop() ?? path}
                        </button>
                    )}

                    <span className={`inline-chevron ml-1 text-token-input-placeholder-foreground transition-opacity duration-200 opacity-0 group-hover:opacity-100 ${expanded ? "opacity-100" : ""}`}>
                        <C0 className={`icon-2xs text-current transition-transform duration-200 ${expanded ? "rotate-90" : ""}`} />
                    </span>
                </div>
            </div>

            {expanded && (
                <div className="mt-2 pl-3 border-l-2 border-token-border/30 text-size-chat text-token-foreground/80 overflow-x-auto">
                    {change.content || change.unified_diff || "No content preview available"}
                </div>
            )}
        </div>
    );
}

// ===================== 5. McpToolCall =====================

function McpToolCallRenderer({ item }) {
    const [expanded, setExpanded] = useState(false);
    const hasArgs = item.args || item.arguments;
    const completed = item.status === "completed" || item.status === "error" || item.status === "failed";
    const server = item.server || item.name?.split("__")[0] || "unknown";
    const tool = item.tool || item.name?.split("__")[1] || item.name || "tool";

    return (
        <div className="py-1">
            <div
                className="cursor-interaction group flex items-center gap-1.5"
                onClick={() => setExpanded(!expanded)}
            >
                <span className={`text-size-chat truncate ${completed && !expanded ? "text-token-input-placeholder-foreground" : "text-token-foreground/80"} group-hover:text-token-foreground`}>
                    <span className="text-token-description-foreground group-hover:text-token-foreground mr-1">
                        {completed ? "Called" : "Calling"}
                    </span>
                    <span className="text-token-foreground/80 group-hover:text-token-foreground">
                        {server} MCP {tool} tool
                    </span>
                </span>
                {hasArgs && (
                    <C0 className={`text-token-input-placeholder-foreground icon-2xs flex-shrink-0 transition-all duration-300 opacity-0 group-hover:opacity-100 ${expanded ? "opacity-100 rotate-90" : "rotate-0"}`} />
                )}
            </div>
            {expanded && hasArgs && (
                <div className="mt-2 pl-3 border-l-2 border-token-border/30 text-size-chat text-token-foreground/80 overflow-x-auto whitespace-pre-wrap max-h-48">
                    {typeof (item.args || item.arguments) === "string"
                        ? (item.args || item.arguments)
                        : JSON.stringify(item.args || item.arguments, null, 2)}
                </div>
            )}
        </div>
    );
}

// ===================== 6. Reasoning =====================

function ReasoningRenderer({ item }) {
    const [expanded, setExpanded] = useState(false);
    const summary = item.summary || [];
    const content = item.content || [];
    const hasSummary = summary.some(s => s && s.length > 0);
    const hasContent = content.some(c => c && c.length > 0);

    const isHoverable = hasContent;

    if (!hasSummary && !hasContent) return null;

    return (
        <div className="my-2">
            <div
                className={`group flex items-center gap-1.5 ${isHoverable ? "cursor-interaction" : "cursor-default"}`}
                onClick={() => { if (isHoverable) setExpanded(!expanded); }}
            >
                <span className="group-hover:text-token-foreground text-size-chat text-token-description-foreground truncate min-w-0">
                    {hasSummary ? summary.filter(Boolean).join(" ") : "Thinking"}
                </span>
                {isHoverable && (
                    <C0 className={`text-token-input-placeholder-foreground group-hover:text-token-foreground icon-2xs flex-shrink-0 transition-all duration-500 opacity-0 group-hover:opacity-100 ${expanded ? "opacity-100 rotate-90" : ""}`} />
                )}
            </div>
            {expanded && hasContent && (
                <div className="mt-2 pl-3 border-l-2 border-token-border/30 text-size-chat text-token-foreground/80 whitespace-pre-wrap">
                    {content.filter(Boolean).join("\n")}
                </div>
            )}
        </div>
    );
}

function PlanRenderer({ item }) {
    if (!item.text) return null;

    return (
        <div className="my-2">
            <div className="flex items-center gap-1.5 mb-1 cursor-default text-token-description-foreground">
                <PlanIcon className="w-3.5 h-3.5 opacity-70" />
                <span className="text-size-chat font-medium">Plan</span>
            </div>
            <div className="pl-5 text-size-chat text-token-foreground/90">
                <MarkdownRenderer>{item.text}</MarkdownRenderer>
            </div>
        </div>
    );
}

function TodoListRenderer({ item }) {
    const plan = item.plan || [];
    if (plan.length === 0) return null;

    const [expanded, setExpanded] = useState(true);

    return (
        <div className="my-3 flex flex-col items-start w-full">
            <button
                type="button"
                className="group flex w-full items-start gap-1 py-1 px-0 cursor-interaction text-token-description-foreground hover:text-token-foreground focus-visible:outline-none"
                onClick={() => setExpanded(!expanded)}
            >
                <div className="flex-shrink-0 pt-[2px]">
                    <C0 className={`icon-2xs text-current transition-transform duration-300 opacity-0 group-hover:opacity-100 ${expanded ? "opacity-100 rotate-90" : ""}`} />
                </div>
                <div className="min-w-0 flex-1 flex flex-col text-left">
                    <span className="truncate text-size-chat font-medium text-token-foreground">
                        {item.explanation || "Working on plan..."}
                    </span>
                </div>
            </button>

            {expanded && (
                <div className="mt-1 pl-5 w-full space-y-1">
                    {plan.map((stepDesc, idx) => {
                        const step = stepDesc.step || stepDesc.title || stepDesc;
                        const status = stepDesc.status || "pending";
                        const isCompleted = status === "completed";
                        const isPending = status === "pending";

                        return (
                            <div key={idx} id={`plan-item-${idx}`} className="flex items-start gap-2">
                                <div className="flex flex-shrink-0 items-start gap-0.5">
                                    <span className="text-size-chat leading-4 text-token-description-foreground font-mono">
                                        {idx + 1}.
                                    </span>
                                </div>
                                <span className={`text-size-chat flex-1 leading-4 ${isCompleted ? "line-through text-token-description-foreground max-w-[80%]" : (isPending ? "text-token-foreground" : "text-token-foreground font-medium")}`}>
                                    {step}
                                </span>
                            </div>
                        );
                    })}
                </div>
            )}
        </div>
    );
}

// ===================== 9. Error =====================

function ErrorRenderer({ item }) {
    return (
        <div className="my-2 rounded-xl border border-red-500/25 bg-red-500/5 overflow-hidden">
            <div className="flex items-start gap-2 px-3 py-2.5">
                <ErrorIcon className="w-4 h-4 text-red-400 flex-shrink-0 mt-0.5" />
                <div className="flex-1 min-w-0">
                    <div className="text-sm text-red-400 font-medium">
                        {item.message || "An error occurred"}
                    </div>
                    {item.willRetry && (
                        <div className="text-xs text-token-description-foreground mt-1">Retrying...</div>
                    )}
                    {item.additionalDetails && (
                        <div className="text-xs text-token-description-foreground mt-1 font-mono">
                            {item.additionalDetails}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

// ===================== 10. ApprovalRequest =====================

function ApprovalRequestRenderer({ request }) {
    const handleApprove = () => {
        bridge.sendResponse({
            id: request.requestId || request.id,
            result: { decision: "approve" },
        });
    };
    const handleDeny = () => {
        bridge.sendResponse({
            id: request.requestId || request.id,
            result: { decision: "deny" },
        });
    };

    return (
        <div className="my-2 rounded-xl border border-yellow-500/30 bg-yellow-500/5 overflow-hidden">
            <div className="flex items-center gap-2 px-4 py-2.5 border-b border-yellow-500/20">
                <ShieldAlertIcon className="w-4 h-4 text-yellow-400" />
                <span className="text-sm font-medium text-token-foreground">Approval Required</span>
            </div>
            <div className="px-4 py-3">
                <div className="text-[13px] text-token-description-foreground leading-relaxed">
                    {request.description || `${request.toolName || request.name || "action"}: ${request.command || "execute"}`}
                </div>
                <div className="flex items-center gap-2 mt-3">
                    <button
                        className="px-4 py-1.5 text-xs font-medium bg-green-500/15 text-green-400 rounded-lg hover:bg-green-500/25 transition-colors border border-green-500/20"
                        onClick={handleApprove}
                    >
                        Approve
                    </button>
                    <button
                        className="px-4 py-1.5 text-xs font-medium bg-red-500/10 text-red-400 rounded-lg hover:bg-red-500/20 transition-colors border border-red-500/20"
                        onClick={handleDeny}
                    >
                        Deny
                    </button>
                </div>
            </div>
        </div>
    );
}

// ===================== Legacy Renderers (兼容旧 item 类型) =====================

function LegacyTerminalCommand({ command }) {
    return (
        <div className="my-1.5 rounded-lg bg-token-editor-background border border-token-border/50 px-3 py-2 font-mono text-sm text-token-foreground">
            <span className="text-token-primary">$ </span>{command}
        </div>
    );
}

function LegacyTerminalOutput({ output }) {
    if (!output) return null;
    return (
        <div className="my-1 ml-2 px-3 py-2 rounded-lg bg-token-editor-background font-mono text-xs text-token-description-foreground max-h-[200px] overflow-auto whitespace-pre-wrap border-l-2 border-token-border/50">
            <AnsiRenderer>{output}</AnsiRenderer>
        </div>
    );
}

function ToolResultRenderer({ result }) {
    if (result.error) {
        return (
            <div className="py-1.5 px-3 ml-2 rounded-lg border border-token-error-foreground/20 bg-token-error-foreground/5 text-xs text-token-error-foreground my-1">
                <span className="font-medium">Error: </span>{result.error}
            </div>
        );
    }
    const output = typeof result.output === "string"
        ? result.output
        : JSON.stringify(result.output, null, 2);
    if (!output) return null;
    return (
        <div className="py-1 ml-2">
            <div className="px-3 py-2 rounded-lg bg-token-bg-secondary text-xs font-mono text-token-description-foreground max-h-[150px] overflow-auto whitespace-pre-wrap">
                {output}
            </div>
        </div>
    );
}

// ===================== SVG Icons (Private) =====================

function TerminalPromptIcon({ className }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M4 5L7 8L4 11" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M9 11H12" strokeLinecap="round" />
        </svg>
    );
}

function ToolIcon({ className }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M9.5 2.5L13.5 6.5L6 14H2V10L9.5 2.5Z" strokeLinejoin="round" />
            <path d="M8 4L12 8" />
        </svg>
    );
}

function FileEditIcon({ className }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M9 1H3C2.45 1 2 1.45 2 2V14C2 14.55 2.45 15 3 15H13C13.55 15 14 14.55 14 14V6L9 1Z" strokeLinejoin="round" />
            <path d="M9 1V6H14" strokeLinejoin="round" />
        </svg>
    );
}

function ThinkingIcon({ className }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="8" cy="8" r="6" />
            <path d="M6 6.5C6 5.5 6.9 4.5 8 4.5C9.1 4.5 10 5.4 10 6.5C10 7.6 9 8 8 8.5V9.5" strokeLinecap="round" />
            <circle cx="8" cy="11.5" r="0.5" fill="currentColor" />
        </svg>
    );
}

function PlanIcon({ className }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <rect x="2" y="2" width="12" height="12" rx="2" />
            <path d="M5 5H11M5 8H11M5 11H9" strokeLinecap="round" />
        </svg>
    );
}

function ShieldAlertIcon({ className }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M8 1.5L3 3.5V7.5C3 10.5 5 13 8 14.5C11 13 13 10.5 13 7.5V3.5L8 1.5Z" strokeLinejoin="round" />
            <path d="M8 5.5V8.5" strokeLinecap="round" />
            <circle cx="8" cy="10.5" r="0.5" fill="currentColor" />
        </svg>
    );
}

function ErrorIcon({ className }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="8" cy="8" r="6" />
            <path d="M8 5V9" strokeLinecap="round" />
            <circle cx="8" cy="11" r="0.5" fill="currentColor" />
        </svg>
    );
}

export default ConversationPanel;
