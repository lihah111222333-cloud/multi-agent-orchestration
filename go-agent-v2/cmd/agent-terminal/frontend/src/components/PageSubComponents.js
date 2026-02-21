// =============================================================================
// Codex.app — 页面子组件 (杂项)
// 从各页面中未定义的组件引用汇总
//
// 功能: 为 FilePreviewPage, PlanSummaryPage, OpenSourceLicensesPage,
//       AnnouncementPage, InboxPage, SelectWorkspacePage 提供缺失的子组件
// =============================================================================

import { Toolbar, ToolbarTitle } from "./Toolbar";

// ===================== FilePreviewPage 子组件 =====================

/**
 * FilePreviewToolbar — 文件预览工具栏
 * 混淆名: (FilePreviewPage 中引用)
 */
export function FilePreviewToolbar({ filePath }) {
    return (
        <Toolbar>
            <ToolbarTitle>{filePath}</ToolbarTitle>
        </Toolbar>
    );
}

/**
 * FileContentRenderer — 文件内容渲染器 (含语法高亮)
 * 混淆名: (FilePreviewPage 中引用)
 */
export function FileContentRenderer({ filePath, readOnly = true }) {
    // 原始实现通过 IPC 读取文件内容并用 CodeMirror / Monaco 渲染
    return (
        <div className="file-content p-4 font-mono text-sm text-token-foreground bg-token-editor-background h-full">
            <div className="text-token-description-foreground text-xs mb-2">{filePath}</div>
            <div className="text-token-input-placeholder-foreground">
                File content loaded via IPC...
            </div>
        </div>
    );
}

// ===================== PlanSummaryPage 子组件 =====================

/**
 * PlanSummaryToolbar — Plan 摘要工具栏
 * 混淆名: (PlanSummaryPage 中引用)
 */
export function PlanSummaryToolbar() {
    return (
        <Toolbar>
            <ToolbarTitle>Plan Summary</ToolbarTitle>
        </Toolbar>
    );
}

/**
 * PlanRenderer — Plan 渲染器
 * 混淆名: (PlanSummaryPage 中引用)
 */
export function PlanRenderer({ conversationId }) {
    return (
        <div className="plan-renderer">
            <div className="text-sm text-token-description-foreground">
                Plan for conversation: <span className="font-mono">{conversationId || "—"}</span>
            </div>
            {/* 原始实现从 ConversationManager 获取 plan 数据并渲染步骤列表 */}
        </div>
    );
}

// ===================== OpenSourceLicensesPage 子组件 =====================

/**
 * LicenseList — 开源许可证列表
 * 混淆名: (OpenSourceLicensesPage 中引用)
 */
export function LicenseList() {
    // 原始实现从打包时嵌入的 license 数据渲染
    const knownDeps = [
        { name: "React", license: "MIT", url: "https://reactjs.org" },
        { name: "react-router-dom", license: "MIT", url: "https://reactrouter.com" },
        { name: "@tanstack/react-query", license: "MIT", url: "https://tanstack.com/query" },
        { name: "immer", license: "MIT", url: "https://immerjs.github.io/immer" },
        { name: "jotai", license: "MIT", url: "https://jotai.org" },
    ];

    return (
        <div className="license-list flex flex-col gap-4">
            {knownDeps.map((dep) => (
                <div key={dep.name} className="border-b border-token-border pb-3">
                    <div className="flex items-center gap-2">
                        <span className="text-sm font-medium text-token-foreground">{dep.name}</span>
                        <span className="text-xs text-token-description-foreground bg-token-bg-secondary px-1.5 py-0.5 rounded">{dep.license}</span>
                    </div>
                    <a href={dep.url} className="text-xs text-token-primary hover:underline" target="_blank" rel="noopener">
                        {dep.url}
                    </a>
                </div>
            ))}
        </div>
    );
}

// ===================== AnnouncementPage 子组件 =====================

/**
 * AnnouncementContent — 公告内容渲染
 * 混淆名: (AnnouncementPage 中引用)
 */
export function AnnouncementContent() {
    // 原始实现从服务端获取公告 Markdown 并渲染
    return (
        <div className="announcement-content text-sm text-token-foreground">
            <p className="text-token-description-foreground">No announcements at this time.</p>
        </div>
    );
}

// Note: PendingRunsList and InboxItemsList are defined inline in InboxAndSkillsPage.js
// with real data props. No stubs needed here.

// ===================== SelectWorkspacePage 子组件 =====================

/**
 * WorkspaceItem — 单个 workspace 条目
 * 混淆名: (SelectWorkspacePage 中引用)
 */
export function WorkspaceItem({ root, label, checked, onChange }) {
    return (
        <label className="workspace-item flex items-center gap-3 px-3 py-2 rounded-lg hover:bg-token-foreground/5 cursor-pointer transition-colors">
            <input
                type="checkbox"
                checked={checked}
                onChange={(e) => onChange(e.target.checked)}
                className="w-4 h-4 rounded border-token-border"
            />
            <div className="flex flex-col min-w-0">
                <span className="text-sm text-token-foreground truncate">{label}</span>
                <span className="text-xs text-token-description-foreground font-mono truncate">{root}</span>
            </div>
        </label>
    );
}
