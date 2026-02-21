// =============================================================================
// Codex.app — DiffRenderer Diff 渲染组件
// 从 DiffViewerPage 的 <DiffRenderer> 引用推导
//
// 功能: 渲染 unified diff 为彩色 HTML
//   - 支持直接传入 diff 文本
//   - 支持通过 conversationId + filePath 从后端查询 diff
// =============================================================================

import { useQuery } from "../hooks/useAppQuery";

/**
 * DiffRenderer — Unified Diff 渲染器
 *
 * @param {Object} props
 * @param {string} props.filePath - 文件路径
 * @param {string} props.conversationId - 对话 ID (用于查询 diff 数据)
 * @param {string} props.diff - 直接传入 diff 文本 (可选)
 */
export function DiffRenderer({ filePath, conversationId, diff }) {
    // 如果没有直接传入 diff, 则从后端查询
    const { data: fetchedDiff } = useQuery("diff/get", {
        params: { filePath, conversationId },
        placeholderData: null,
        queryConfig: {
            enabled: !diff && !!filePath && !!conversationId,
        },
    });

    const diffText = diff || fetchedDiff?.diff || "";

    if (!diffText) {
        return (
            <div className="diff-empty flex items-center justify-center h-full text-token-description-foreground text-sm">
                No changes to display
            </div>
        );
    }

    const lines = diffText.split("\n");

    return (
        <div className="diff-renderer font-mono text-sm">
            {lines.map((line, i) => {
                let lineClass = "diff-line px-4 py-0.5 whitespace-pre";
                if (line.startsWith("+")) lineClass += " bg-green-500/10 text-green-600";
                else if (line.startsWith("-")) lineClass += " bg-red-500/10 text-red-600";
                else if (line.startsWith("@@")) lineClass += " text-token-primary bg-token-primary/5";
                else if (line.startsWith("diff ") || line.startsWith("index ")) lineClass += " text-token-description-foreground font-bold";
                else lineClass += " text-token-foreground";

                return (
                    <div key={i} className={lineClass}>
                        {line}
                    </div>
                );
            })}
        </div>
    );
}

export default DiffRenderer;
