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

/**
 * HomeIcon — 小屋图标
 */
export function HomeIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M2 8L8 2L14 8" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M4 7V13H12V7" strokeLinejoin="round" />
            <path d="M6.5 13V10H9.5V13" />
        </svg>
    );
}

/**
 * InboxIcon — 收件箱图标
 */
export function InboxIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <rect x="2" y="3" width="12" height="10" rx="1.5" strokeLinejoin="round" />
            <path d="M2 9H5L6.5 11H9.5L11 9H14" strokeLinejoin="round" />
        </svg>
    );
}

/**
 * SkillsIcon — 积木/技能图标
 */
export function SkillsIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <rect x="2" y="8" width="5" height="5" rx="0.75" />
            <rect x="9" y="8" width="5" height="5" rx="0.75" />
            <rect x="5.5" y="3" width="5" height="5" rx="0.75" />
        </svg>
    );
}

/**
 * ArrowUpIcon — 上箭头图标 (Composer 发送按钮)
 */
export function ArrowUpIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M8 12V4M4.5 7.5L8 4L11.5 7.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
    );
}

/**
 * TerminalIcon — 终端图标 (Composer footer workspace)
 */
export function TerminalIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <rect x="1.5" y="3" width="13" height="10" rx="1.5" />
            <path d="M4.5 7L6.5 9L4.5 11" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M8.5 11H11.5" strokeLinecap="round" />
        </svg>
    );
}

/**
 * ShieldIcon — 盾牌图标 (Composer footer permissions)
 */
export function ShieldIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="currentColor">
            <path d="M8 1L3 3.5V7C3 10.5 5 13 8 15C11 13 13 10.5 13 7V3.5L8 1Z" />
            <circle cx="8" cy="7" r="1" fill="var(--color-token-bg-primary, #1a1a1a)" />
            <rect x="7.5" y="8.5" width="1" height="2.5" rx="0.5" fill="var(--color-token-bg-primary, #1a1a1a)" />
        </svg>
    );
}

/**
 * McpIcon — MCP 连接图标 (Composer footer)
 */
export function McpIcon({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="4" cy="8" r="1.5" />
            <circle cx="12" cy="5" r="1.5" />
            <circle cx="12" cy="11" r="1.5" />
            <path d="M5.5 7.5L10.5 5.5M5.5 8.5L10.5 10.5" />
        </svg>
    );
}

/**
 * LoadingDot — 加载状态指示图标 (Composer footer)
 */
export function LoadingDot({ className = "w-4 h-4" }) {
    return (
        <svg className={className} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="8" cy="8" r="6" strokeDasharray="8 4" />
        </svg>
    );
}

export default {
    PinIcon, ChevronIcon, SendIcon, PlusIcon, SearchIcon, CloseIcon, SettingsIcon, GitBranchIcon,
    HomeIcon, InboxIcon, SkillsIcon, ArrowUpIcon, TerminalIcon, ShieldIcon, McpIcon, LoadingDot,
};
