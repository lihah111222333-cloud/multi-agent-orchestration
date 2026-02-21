// =============================================================================
// Codex.app — Composer 输入框组件
// 混淆名: (ChatPage, HomePage 中引用)
// 提取自: index-formatted.js L~210000+ 附近
//
// 功能: 用户输入组件
//   - 多行文本输入 (auto-resize)
//   - 附件栏 (图片/文件拖拽)
//   - 工具栏行: 附件按钮 / 模型选择器 / 自主级别 / 发送按钮
//   - 底部状态栏: workspace / permissions / settings (响应式标签)
//   - 快捷键支持 (Enter 发送, Shift+Enter 换行)
// =============================================================================

import { useState, useRef, useCallback, useEffect } from "react";
import {
    PlusIcon,
    ChevronIcon,
    ArrowUpIcon,
    TerminalIcon,
    ShieldIcon,
    McpIcon,
    LoadingDot,
} from "./Icons";
import { bridge } from "../bridge";

/**
 * Composer — 对话输入框
 *
 * @param {Object} props
 * @param {string|null} props.conversationId - 对话 ID (新建时为 null)
 * @param {Function} props.onSend - (input: InputItem[], attachments: Attachment[]) => void
 * @param {string} props.placeholder
 */
export function Composer({ conversationId, onSend, placeholder = "Send a message..." }) {
    const [text, setText] = useState("");
    const [attachments, setAttachments] = useState([]);
    const textareaRef = useRef(null);

    // 自动调整高度
    useEffect(() => {
        const ta = textareaRef.current;
        if (ta) {
            ta.style.height = "auto";
            ta.style.height = `${Math.min(ta.scrollHeight, 200)}px`;
        }
    }, [text]);

    // 发送消息
    const handleSend = useCallback(() => {
        const trimmed = text.trim();
        if (!trimmed && attachments.length === 0) return;

        const input = [];
        if (trimmed) {
            input.push({ type: "text", text: trimmed });
        }

        onSend(input, attachments);
        setText("");
        setAttachments([]);
    }, [text, attachments, onSend]);

    // 键盘事件
    const handleKeyDown = (e) => {
        if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            handleSend();
        }
    };

    // 文件拖拽
    const handleDrop = (e) => {
        e.preventDefault();
        const files = Array.from(e.dataTransfer?.files ?? []);
        for (const file of files) {
            if (file.type.startsWith("image/")) {
                const path = bridge.getPathForFile(file);
                if (path) {
                    setAttachments((prev) => [...prev, {
                        type: "localImage",
                        path,
                        name: file.name,
                    }]);
                }
            }
        }
    };

    // 移除附件
    const removeAttachment = (index) => {
        setAttachments((prev) => prev.filter((_, i) => i !== index));
    };

    const hasContent = text.trim() || attachments.length > 0;

    return (
        <div
            className="composer px-4 py-3 pb-4"
            onDrop={handleDrop}
            onDragOver={(e) => e.preventDefault()}
        >
            {/* 附件预览栏 */}
            {attachments.length > 0 && (
                <div className="attachment-bar flex gap-2 mb-2 overflow-x-auto">
                    {attachments.map((att, i) => (
                        <AttachmentPreview
                            key={i}
                            attachment={att}
                            onRemove={() => removeAttachment(i)}
                        />
                    ))}
                </div>
            )}

            {/* 主输入容器 — 圆角暗色卡片 */}
            <div className="bg-token-input-background border border-token-input-border rounded-2xl overflow-hidden">
                {/* 文本输入区 */}
                <div className="px-4 pt-3 pb-1">
                    <textarea
                        ref={textareaRef}
                        className="composer-input w-full resize-none bg-transparent text-token-foreground placeholder-token-input-placeholder-foreground text-sm outline-none min-h-[36px] max-h-[200px]"
                        placeholder={placeholder}
                        value={text}
                        onChange={(e) => setText(e.target.value)}
                        onKeyDown={handleKeyDown}
                        rows={1}
                    />
                </div>

                {/* 工具栏行 — 附件 / 模型 / 自主级别 / 发送 */}
                <div className="flex items-center gap-1 px-3 pb-2.5">
                    {/* 添加附件 */}
                    <button
                        className="p-1.5 rounded-lg text-token-description-foreground hover:text-token-foreground hover:bg-token-foreground/5 transition-colors"
                        title="Add attachment"
                    >
                        <PlusIcon className="w-4 h-4" />
                    </button>

                    {/* 模型选择器 */}
                    <button className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs text-token-description-foreground hover:text-token-foreground hover:bg-token-foreground/5 transition-colors">
                        <span>Custom</span>
                        <ChevronIcon direction="down" className="w-3 h-3 opacity-50" />
                    </button>

                    {/* 自主级别选择器 */}
                    <button className="flex items-center gap-1 px-2 py-1 rounded-lg text-xs text-token-description-foreground hover:text-token-foreground hover:bg-token-foreground/5 transition-colors">
                        <span>Extra High</span>
                        <ChevronIcon direction="down" className="w-3 h-3 opacity-50" />
                    </button>

                    <div className="flex-1" />

                    {/* 发送按钮 — 圆形 */}
                    <button
                        className={`p-1.5 rounded-full transition-colors
                            ${hasContent
                                ? "bg-token-foreground text-token-bg-primary hover:opacity-80"
                                : "bg-token-foreground/20 text-token-description-foreground cursor-not-allowed"}`}
                        onClick={handleSend}
                        disabled={!hasContent}
                    >
                        <ArrowUpIcon className="w-4 h-4" />
                    </button>
                </div>
            </div>

            {/* 底部状态栏 — composer-footer 容器查询 (与原版 CSS 1:1) */}
            <div className="composer-footer flex items-center gap-1 px-1 pt-2">
                {/* Workspace / 终端 */}
                <button className="flex items-center gap-1 px-2 py-1 rounded-md text-xs text-token-description-foreground hover:text-token-foreground hover:bg-token-foreground/5 transition-colors">
                    <TerminalIcon className="w-3.5 h-3.5" />
                    <span className="composer-footer__label--sm">Terminal</span>
                    <ChevronIcon direction="down" className="w-3 h-3 opacity-40 composer-footer__secondary-chevron" />
                </button>

                {/* Permissions */}
                <button className="flex items-center gap-1 px-2 py-1 rounded-md text-xs text-token-description-foreground hover:text-token-foreground hover:bg-token-foreground/5 transition-colors">
                    <ShieldIcon className="w-3.5 h-3.5 text-orange-400" />
                    <span className="composer-footer__permissions-label">Protected</span>
                    <ChevronIcon direction="down" className="w-3 h-3 opacity-40 composer-footer__secondary-chevron" />
                </button>

                <div className="flex-1" />

                {/* MCP / 连接 */}
                <button className="flex items-center gap-1 px-2 py-1 rounded-md text-xs text-token-description-foreground hover:text-token-foreground hover:bg-token-foreground/5 transition-colors">
                    <McpIcon className="w-3.5 h-3.5" />
                    <span className="composer-footer__secondary-label">MCP</span>
                    <ChevronIcon direction="down" className="w-3 h-3 opacity-40 composer-footer__secondary-chevron" />
                </button>

                {/* 状态指示 */}
                <div className="p-1.5 text-token-description-foreground">
                    <LoadingDot className="w-3.5 h-3.5" />
                </div>
            </div>
        </div>
    );
}

// ===================== 子组件 =====================

function AttachmentPreview({ attachment, onRemove }) {
    return (
        <div className="attachment-preview relative inline-flex items-center gap-1 px-2 py-1 rounded-md bg-token-background-secondary text-xs text-token-foreground border border-token-border">
            {attachment.type === "localImage" && (
                <img src={`file://${attachment.path}`} alt="" className="w-8 h-8 rounded object-cover" />
            )}
            <span className="truncate max-w-[100px]">{attachment.name}</span>
            <button
                className="ml-1 text-token-description-foreground hover:text-token-foreground"
                onClick={onRemove}
            >
                ×
            </button>
        </div>
    );
}

export default Composer;
