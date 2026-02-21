// =============================================================================
// Codex.app — 公告页面 (AnnouncementPage)
// 混淆名: F5t
// 路由: /announcement
// 提取自: index-formatted.js L54100
//
// 功能: 应用内公告/通知页面
//   - 显示系统公告和重要通知
//   - 通常用于发布说明、功能更新等
// =============================================================================

import { AnnouncementToolbar } from "../components/Toolbar";
import { AnnouncementContent } from "../components/PageSubComponents";

/**
 * AnnouncementPage — 公告页
 *
 * 从远程或本地配置加载公告内容并渲染
 */
function AnnouncementPage() {
    // 公告内容由服务端推送或从 config 读取
    // 渲染 Markdown 格式的公告

    return (
        <div className="h-full flex flex-col">
            <AnnouncementToolbar />
            <div className="flex-1 overflow-auto p-6">
                <AnnouncementContent />
            </div>
        </div>
    );
}

export { AnnouncementPage };
