// =============================================================================
// Codex.app — StreamingIndicator 流式输出指示器
// 从 ChatPage TurnRenderer 中的 <StreamingIndicator> 引用推导
//
// 功能: AI 回复流式输出时的动画指示
// =============================================================================

/**
 * StreamingIndicator — 流式输出动画 (3 点闪烁)
 *
 * 显示在 AI 正在回复时的最新 turn 底部
 */
export function StreamingIndicator() {
    return (
        <div className="streaming-indicator flex items-center gap-1 py-2 px-4">
            <span className="streaming-dot w-1.5 h-1.5 rounded-full bg-token-primary animate-pulse" />
            <span className="streaming-dot w-1.5 h-1.5 rounded-full bg-token-primary animate-pulse" style={{ animationDelay: "0.15s" }} />
            <span className="streaming-dot w-1.5 h-1.5 rounded-full bg-token-primary animate-pulse" style={{ animationDelay: "0.3s" }} />
        </div>
    );
}

/**
 * TurnError — 轮次错误显示
 * 从 ChatPage TurnRenderer 中的 <TurnError> 引用推导
 */
export function TurnError({ error }) {
    return (
        <div className="turn-error flex items-center gap-2 py-2 px-4 text-sm text-token-error-foreground">
            <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 16 16" fill="currentColor">
                <path d="M8 1C4.13 1 1 4.13 1 8s3.13 7 7 7 7-3.13 7-7-3.13-7-7-7zm.5 10.5h-1v-1h1v1zm0-2h-1v-5h1v5z" />
            </svg>
            <span>{error.message || "An error occurred"}</span>
        </div>
    );
}

/**
 * ContextCompaction — 上下文压缩提示
 * 从 ChatPage TurnRenderer 中的 <ContextCompaction> 引用推导
 */
export function ContextCompaction({ completed }) {
    return (
        <div className="context-compaction flex items-center gap-2 py-2 px-4 text-xs text-token-description-foreground">
            {completed
                ? "Context was compacted to fit within the model's context window."
                : "Compacting context..."}
        </div>
    );
}

export default StreamingIndicator;
