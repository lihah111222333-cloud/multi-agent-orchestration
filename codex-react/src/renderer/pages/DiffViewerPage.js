// =============================================================================
// Codex.app — Diff 查看器页面 (DiffViewerPage)
// 混淆名: aun
// 路由: /diff
// 提取自: index-formatted.js L192160
//
// 功能: 独立的 Diff 查看页面
//   - 从 URL query params 获取 diff 数据
//   - 渲染文件修改的 unified diff 视图
//   - 支持 side-by-side 和 inline 两种模式
//   - 不需要认证
// =============================================================================

import { useSearchParams } from "react-router-dom";
import { DiffToolbar } from "../components/Toolbar";
import { DiffRenderer } from "../components/DiffRenderer";

/**
 * DiffViewerPage — Diff 独立查看页
 *
 * 通常由 ChatPage 的 "Open in new tab" 操作打开
 * URL params: ?path=<filePath>&conversationId=<id>
 *
 * 组件树:
 *   DiffViewerPage
 *   ├── DiffToolbar (文件路径 + 模式切换)
 *   └── DiffRenderer (unified / split 模式)
 */
function DiffViewerPage() {
    // 从 URL 获取参数
    const [searchParams] = useSearchParams();
    const filePath = searchParams.get("path");
    const conversationId = searchParams.get("conversationId");

    // 如果缺少参数, 显示错误
    if (!filePath || !conversationId) {
        return (
            <div className="flex h-full items-center justify-center text-token-description-foreground">
                Missing required parameters
            </div>
        );
    }

    return (
        <div className="h-full flex flex-col">
            <DiffToolbar filePath={filePath} />
            <div className="flex-1 overflow-auto">
                <DiffRenderer
                    filePath={filePath}
                    conversationId={conversationId}
                />
            </div>
        </div>
    );
}

export { DiffViewerPage };
