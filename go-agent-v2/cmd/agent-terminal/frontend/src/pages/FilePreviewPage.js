// =============================================================================
// Codex.app — 文件预览页面 (FilePreviewPage)
// 混淆名: pun
// 路由: /file-preview
// 提取自: index-formatted.js L192536
//
// 功能: 独立的文件预览页面
//   - 从 URL query params 获取文件路径
//   - 渲染文件内容 (只读)
//   - 语法高亮
//   - 不需要认证
// =============================================================================

import { useSearchParams } from "react-router-dom";
import { FilePreviewToolbar, FileContentRenderer } from "../components/PageSubComponents";

/**
 * FilePreviewPage — 文件预览页
 *
 * 通常由 ChatPage 的文件链接打开
 * URL params: ?path=<filePath>
 */
function FilePreviewPage() {
    const [searchParams] = useSearchParams();
    const filePath = searchParams.get("path");

    if (!filePath) {
        return (
            <div className="flex h-full items-center justify-center text-token-description-foreground">
                No file path specified
            </div>
        );
    }

    return (
        <div className="h-full flex flex-col">
            <FilePreviewToolbar filePath={filePath} />
            <div className="flex-1 overflow-auto">
                <FileContentRenderer filePath={filePath} readOnly />
            </div>
        </div>
    );
}

export { FilePreviewPage };
