// =============================================================================
// Codex.app — CollapsibleSection 可折叠面板
// 从 DebugPage 的 <CollapsibleSection> 使用推导
//
// Props: title, storageKey, variant, children
// =============================================================================

import { useState, useEffect } from "react";

/**
 * CollapsibleSection — 可折叠面板
 *
 * @param {Object} props
 * @param {string} props.title - 标题
 * @param {string} props.storageKey - localStorage 持久化键 (记住展开状态)
 * @param {"global"|"selection"} props.variant - 样式变体
 * @param {React.ReactNode} props.children
 */
export function CollapsibleSection({ title, storageKey, variant = "global", children }) {
    const [isOpen, setIsOpen] = useState(() => {
        if (storageKey) {
            const saved = localStorage.getItem(`collapsible:${storageKey}`);
            return saved !== null ? saved === "true" : true;
        }
        return true;
    });

    useEffect(() => {
        if (storageKey) {
            localStorage.setItem(`collapsible:${storageKey}`, String(isOpen));
        }
    }, [isOpen, storageKey]);

    const variantClasses = {
        global: "border-b border-token-border",
        selection: "bg-token-background-secondary rounded-lg",
    };

    return (
        <div className={`collapsible-section ${variantClasses[variant] || ""}`}>
            <button
                className="w-full flex items-center justify-between px-4 py-2.5 text-left text-sm font-medium text-token-foreground hover:bg-token-foreground/5 transition-colors"
                onClick={() => setIsOpen(!isOpen)}
            >
                <span>{title}</span>
                <svg
                    className={`w-4 h-4 transition-transform ${isOpen ? "rotate-90" : ""}`}
                    viewBox="0 0 16 16"
                    fill="none"
                >
                    <path d="M6 4L10 8L6 12" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
            </button>
            {isOpen && (
                <div className="collapsible-content px-4 pb-3">
                    {children}
                </div>
            )}
        </div>
    );
}

/**
 * DebugRow — DebugPage 中的键值行
 */
export function DebugRow({ label, value }) {
    return (
        <div className="flex items-center justify-between py-1 text-xs">
            <span className="text-token-description-foreground">{label}</span>
            <span className="text-token-foreground font-mono select-all">{value}</span>
        </div>
    );
}

/**
 * DebugLines — DebugPage 中的多行调试内容
 */
export function DebugLines({ lines }) {
    return (
        <div className="flex flex-col gap-1 text-xs font-mono">
            {(lines ?? []).map((line, i) => (
                <div key={i} className="text-token-foreground">{line.text ?? JSON.stringify(line)}</div>
            ))}
        </div>
    );
}

/**
 * LogExportButton — DebugPage 中的日志导出按钮
 */
export function LogExportButton({ disabled, isExporting, onExport }) {
    return (
        <div className="flex gap-2">
            {["all", "codex-cli", "app-server"].map((scope) => (
                <button
                    key={scope}
                    className="text-xs px-2 py-1 rounded border border-token-border hover:bg-token-foreground/5 disabled:opacity-50"
                    disabled={disabled || isExporting}
                    onClick={() => onExport(scope)}
                >
                    Export {scope}
                </button>
            ))}
        </div>
    );
}

export default CollapsibleSection;
