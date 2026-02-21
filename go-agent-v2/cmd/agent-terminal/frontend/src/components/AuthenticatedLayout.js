// =============================================================================
// Codex.app — AuthenticatedLayout 已认证布局
// 混淆名: kPt
// 提取自: index-formatted.js L107580 附近
//
// 功能: 已认证后的主布局
//   - 左侧 Sidebar (导航)
//   - 右侧 Outlet (内容)
//   - 全局快捷键
// =============================================================================

import { Outlet, useNavigate, useLocation } from "react-router-dom";
import { Sidebar } from "../components/Sidebar";

/**
 * AuthenticatedLayout — 已认证后的主应用布局
 * 混淆名: kPt
 *
 * 结构:
 *   AuthenticatedLayout
 *   ├── Sidebar (左侧导航)
 *   │   ├── HomeButton (/)
 *   │   ├── InboxButton (/inbox)
 *   │   ├── SettingsButton (/settings)
 *   │   └── ThreadList (最近对话)
 *   └── Outlet (右侧内容)
 */
export function AuthenticatedLayout() {
    return (
        <div className="authenticated-layout flex h-full bg-token-bg-primary text-token-foreground">
            <Sidebar />
            <main className="flex-1 min-w-0 flex flex-col">
                <Outlet />
            </main>
        </div>
    );
}

export default AuthenticatedLayout;
