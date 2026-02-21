// =============================================================================
// Codex.app — SVG Icons 图标集合
// 从各页面引用的图标推导
// =============================================================================

/**
 * PinIcon — 钉子/固定图标
 * 混淆名: (ThreadOverlayPage 中引用)
 */
export function PinIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M9.5 2.5L13.5 6.5L10 10L8 14L2 8L6 6L9.5 2.5Z" strokeLinejoin="round" />
            <path d="M6 10L2.5 13.5" strokeLinecap="round" />
        </svg>
    );
}

/**
 * ChevronIcon — 箭头图标
 */
export function ChevronIcon({ direction = "right", className = "w-4 h-4" }) {
    const rotations = { right: "0", down: "90", left: "180", up: "270" };
    return (
        <svg className={className} style={{ transform: `rotate(${rotations[direction] || 0}deg)` }} viewBox="0 0 16 16" fill="none">
            <path d="M6 4L10 8L6 12" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
    );
}

/**
 * SendIcon — 发送图标
 */
export function SendIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="currentColor">
            <path d="M3 8L13 3L8 13L7 9L3 8Z" />
        </svg>
    );
}

/**
 * PlusIcon — 加号图标
 */
export function PlusIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M8 3V13M3 8H13" strokeLinecap="round" />
        </svg>
    );
}

/**
 * SearchIcon — 搜索图标
 */
export function SearchIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="7" cy="7" r="4" />
            <path d="M10 10L13.5 13.5" strokeLinecap="round" />
        </svg>
    );
}

/**
 * CloseIcon — 关闭/X 图标
 */
export function CloseIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M4 4L12 12M12 4L4 12" strokeLinecap="round" />
        </svg>
    );
}

/**
 * SettingsIcon — 设置齿轮图标
 */
export function SettingsIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="8" cy="8" r="2.5" />
            <path d="M8 1V3M8 13V15M1 8H3M13 8H15M3 3L4.5 4.5M11.5 11.5L13 13M13 3L11.5 4.5M4.5 11.5L3 13" strokeLinecap="round" />
        </svg>
    );
}

/**
 * GitBranchIcon — Git 分支图标
 */
export function GitBranchIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="5" cy="4" r="1.5" />
            <circle cx="5" cy="12" r="1.5" />
            <circle cx="11" cy="7" r="1.5" />
            <path d="M5 5.5V10.5M5 7H9.5" />
        </svg>
    );
}

export default {
    PinIcon, ChevronIcon, SendIcon, PlusIcon, SearchIcon, CloseIcon, SettingsIcon, GitBranchIcon,
};
