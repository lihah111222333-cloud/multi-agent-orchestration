// =============================================================================
// Codex.app — 远程任务页面 (RemoteTaskPage)
// 混淆名: fTn
// 路由: /remote/:taskId
// 提取自: index-formatted.js L325289
//
// 功能: 查看远程 (云端) 任务详情
//   - 显示远程任务的对话历史
//   - 支持查看任务状态 (running / completed / failed)
//   - 支持从远程任务 fork 到本地
//   - 使用 ConversationPanel 渲染对话内容
// =============================================================================

import { useParams, useNavigate } from "react-router-dom";
import { useRemoteTask } from "../hooks/useRemoteTasks";
import { RemoteTaskToolbar } from "../components/Toolbar";
import { ConversationPanel } from "../components/ConversationPanel";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { bridge } from "../bridge";

/**
 * RemoteTaskPage — 远程任务详情页
 *
 * 与 ChatPage 类似, 但数据来源为远程任务
 * 不支持发送新消息, 只支持查看和 fork
 */
function RemoteTaskPage() {
    const { taskId } = useParams();
    const navigate = useNavigate();

    // 获取远程任务数据
    const { data: task, isLoading, error } = useRemoteTask(taskId);

    if (isLoading) {
        return <LoadingSpinner />;
    }

    if (error || !task) {
        return (
            <div className="flex h-full items-center justify-center text-token-description-foreground">
                Task not found
            </div>
        );
    }

    return (
        <div className="h-full flex flex-col">
            {/* 远程任务工具栏 */}
            <RemoteTaskToolbar
                taskId={taskId}
                title={task.title}
                status={task.status}
                onForkLocally={async () => {
                    try {
                        const result = await bridge.callAPI("thread/fork", {
                            remoteTaskId: taskId,
                        });
                        if (result?.threadId) {
                            navigate(`/local/${result.threadId}`);
                        }
                    } catch (err) {
                        console.error("[RemoteTaskPage] Fork failed:", err);
                    }
                }}
            />

            {/* 对话渲染 (只读) */}
            <ConversationPanel
                conversationId={task.conversationId}
                shouldResume={false}
                isLightweight={false}
            />
        </div>
    );
}

export { RemoteTaskPage };
