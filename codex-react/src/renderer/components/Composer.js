// =============================================================================
// Codex.app — Composer 输入框组件
// 混淆名: (ChatPage, HomePage 中引用)
// 提取自: index-formatted.js L~210000+ 附近
//
// 功能: 用户输入组件
//   - 多行文本输入 (auto-resize)
//   - 附件栏 (图片/文件拖拽)
//   - 模型选择器 (ModelSelector)
//   - 发送按钮
//   - 快捷键支持 (Enter 发送, Shift+Enter 换行)
// =============================================================================

import { useState, useRef, useCallback, useEffect } from "react";
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

    return (
        <div
            className="composer border-t border-token-border px-4 py-3"
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

            {/* 输入区域 */}
            <div className="flex items-end gap-2">
                <textarea
                    ref={textareaRef}
                    className="composer-input flex-1 resize-none bg-transparent text-token-foreground placeholder-token-input-placeholder-foreground text-sm outline-none min-h-[36px] max-h-[200px]"
                    placeholder={placeholder}
                    value={text}
                    onChange={(e) => setText(e.target.value)}
                    onKeyDown={handleKeyDown}
                    rows={1}
                />
                <button
                    className={`send-button p-2 rounded-lg transition-colors
                        ${text.trim() || attachments.length > 0
                            ? "bg-token-primary text-white hover:bg-token-primary-hover"
                            : "text-token-description-foreground cursor-not-allowed"}`}
                    onClick={handleSend}
                    disabled={!text.trim() && attachments.length === 0}
                >
                    <SendIcon className="w-4 h-4" />
                </button>
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

function SendIcon({ className }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none">
            <path d="M3 8L13 3L8 13L7 9L3 8Z" fill="currentColor" />
        </svg>
    );
}

export default Composer;
