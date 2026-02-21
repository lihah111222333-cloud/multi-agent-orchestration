// =============================================================================
// Codex.app — Plan 摘要页面 (PlanSummaryPage)
// 混淆名: AEn
// 路由: /plan-summary
// 提取自: index-formatted.js L318044
//
// 功能: 独立的 Plan 摘要查看页面
//   - 显示 AI 生成的任务计划 (Plan)
//   - 包含计划步骤列表
//   - 不需要认证
// =============================================================================

import { useSearchParams } from "react-router-dom";
import { PlanSummaryToolbar, PlanRenderer } from "../components/PageSubComponents";

/**
 * PlanSummaryPage — Plan 摘要页
 *
 * 通常由 ChatPage 的 Plan 组件 "View full plan" 操作打开
 */
function PlanSummaryPage() {
    const [searchParams] = useSearchParams();
    const conversationId = searchParams.get("conversationId");

    return (
        <div className="h-full flex flex-col">
            <PlanSummaryToolbar />
            <div className="flex-1 overflow-auto p-4">
                <PlanRenderer conversationId={conversationId} />
            </div>
        </div>
    );
}

export { PlanSummaryPage };
