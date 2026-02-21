// =============================================================================
// Codex.app — Worktree 初始化页面 (WorktreeInitPage)
// 混淆名: zAn
// 路由: /worktree-init-v2/:pendingWorktreeId
// 提取自: index-formatted.js L331086 (zAn) 约 300 行
//
// 功能: Git Worktree 创建过程页面
//   - 显示 worktree 创建进度
//   - 流式输出 worktree 设置日志
//   - 支持: fork 现有对话 / 新建对话 两种模式
//   - 提供 "Work locally instead" 和 "Cancel" 操作
//   - 创建完成后自动导航到对应对话
// =============================================================================

import { useEffect, useRef } from "react";
import { useParams, useNavigate, Navigate } from "react-router-dom";
import { useConversationManager } from "../hooks/useConversationManager";
import { useGoHome } from "../hooks/useNavigation";
import { useToast } from "../hooks/useToast";
import { useIntl } from "../hooks/useIntl";
import { useWorktreeManager, usePendingWorktree } from "../hooks/useWorktree";
import { useMutation } from "../hooks/useAppQuery";
import { Toolbar, ToolbarTitle } from "../components/Toolbar";
import { Button } from "../components/Button";
import { FormattedMessage } from "../components/FormattedMessage";
import { AnsiRenderer } from "../components/AnsiRenderer";
import { CenteredLayout } from "../components/OnboardingContainer";
import { UserMessage } from "../components/ConversationPanel";
import { enrichStartParams } from "../utils";

/**
 * WorktreeInitPage — Worktree 创建进度页
 *
 * 路由参数: { pendingWorktreeId: string }
 *
 * 生命周期:
 *   1. 根据 pendingWorktreeId 获取 pending worktree 信息
 *   2. 如果已有 conversationId → 重定向到 /local/:conversationId
 *   3. 如果不存在 → 重定向到 /
 *   4. 显示创建进度:
 *      - "Creating worktree" 标题
 *      - 用户 prompt (只读)
 *      - 状态: queued → creating → worktree-ready / failed
 *      - 流式输出日志
 *   5. 提供操作:
 *      - "Work locally instead" → 取消 worktree, 本地启动对话
 *      - "Cancel" → 取消 worktree, 返回首页
 *
 * 内部函数:
 *   eGe() — 执行创建逻辑 (fork-conversation 或 start-conversation)
 *   HAn() — 更新 pending worktree 的 needsAttention 状态
 */
function WorktreeInitPage() {
    const navigate = useNavigate();
    const mcpManager = useConversationManager();
    const goHome = useGoHome();
    const toast = useToast();
    const intl = useIntl();
    const { pendingWorktreeId } = useParams();
    const outputRef = useRef(null);
    const { updatePendingWorktree, abortAndRemovePendingWorktree } = useWorktreeManager();
    const pendingWorktree = usePendingWorktree(pendingWorktreeId);

    const mutation = useMutation({
        mutationFn: async ({ continueLocally }) => {
            if (!pendingWorktree) return;
            abortAndRemovePendingWorktree(pendingWorktree.id);
            if (continueLocally) {
                try {
                    const conversationId = await createFromEntry({
                        entry: pendingWorktree,
                        mcpManager,
                        workspaceRoot: pendingWorktree.sourceWorkspaceRoot,
                    });
                    navigate(`/local/${conversationId}`);
                } catch (err) {
                    abortAndRemovePendingWorktree(pendingWorktree.id);
                    toast.danger(intl.formatMessage({
                        id: "composer.localTaskError",
                        defaultMessage: "Error starting conversation",
                    }));
                    throw err;
                }
            } else {
                goHome({ prefillPrompt: pendingWorktree.prompt.trim() });
            }
        },
    });

    // 标记为已查看
    useEffect(() => {
        if (pendingWorktreeId) {
            updatePendingWorktree(pendingWorktreeId, (wt) => {
                if (wt.needsAttention) wt.needsAttention = false;
            });
        }
    }, [pendingWorktreeId]);

    // 自动滚动到底部
    useEffect(() => {
        const el = outputRef.current;
        if (el && pendingWorktree) {
            el.scrollTop = el.scrollHeight;
        }
    }, [pendingWorktree]);

    // 加载中或已完成 → loading / redirect
    if (mutation.isPending || mutation.isSuccess) return null;

    if (pendingWorktree) {
        // 如果已创建对话, 直接跳转
        if (pendingWorktree.conversationId) {
            return <Navigate to={`/local/${pendingWorktree.conversationId}`} replace />;
        }
    } else {
        return <Navigate to="/" replace />;
    }

    const isInProgress = pendingWorktree.phase === "queued" || pendingWorktree.phase === "creating";
    const isStableWorktree = pendingWorktree.launchMode === "create-stable-worktree";

    return (
        <CenteredLayout
            header={
                <Toolbar electron>
                    <ToolbarTitle>
                        <FormattedMessage
                            id="worktreeInitV2.title"
                            defaultMessage="Creating worktree"
                        />
                    </ToolbarTitle>
                </Toolbar>
            }
        >
            <div className="flex flex-col gap-4">
                {/* 用户 prompt */}
                <UserMessage message={pendingWorktree.prompt} alwaysShowActions />

                {/* 状态 + 操作 */}
                <div className="flex items-center justify-between gap-3">
                    <div className="text-token-description-foreground text-sm">
                        {(pendingWorktree.phase === "worktree-ready" || pendingWorktree.phase === "conversation-starting") && (
                            <FormattedMessage id="worktreeInitV2.status.success" defaultMessage="Worktree ready." />
                        )}
                        {pendingWorktree.phase === "failed" && (
                            <FormattedMessage id="worktreeInitV2.status.error" defaultMessage="Worktree setup failed." />
                        )}
                        {isInProgress && (
                            pendingWorktree.launchMode === "fork-conversation"
                                ? <FormattedMessage id="worktreeInitV2.status.runningFork" defaultMessage="Creating a worktree to fork this conversation." />
                                : <FormattedMessage id="worktreeInitV2.status.running" defaultMessage="Creating a worktree and running setup." />
                        )}
                    </div>
                    {isInProgress && (
                        <div className="flex items-center gap-2">
                            {!isStableWorktree && (
                                <Button
                                    color="ghost"
                                    loading={mutation.isPending}
                                    onClick={() => mutation.mutate({ continueLocally: true })}
                                >
                                    <FormattedMessage id="worktreeInitV2.workLocallyInstead" defaultMessage="Work locally instead" />
                                </Button>
                            )}
                            <Button
                                color="ghost"
                                loading={mutation.isPending}
                                onClick={() => mutation.mutate({ continueLocally: false })}
                            >
                                <FormattedMessage id="worktreeInitV2.cancel" defaultMessage="Cancel" />
                            </Button>
                        </div>
                    )}
                </div>

                {/* 错误信息 */}
                {pendingWorktree.errorMessage && (
                    <div className="text-token-error-foreground text-sm">
                        {pendingWorktree.errorMessage}
                    </div>
                )}

                {/* 输出日志 */}
                <div
                    ref={outputRef}
                    className="vertical-scroll-fade-mask min-h-[500px] max-h-[500px] bg-token-editor-background text-token-input-placeholder-foreground text-size-code flex flex-1 flex-col overflow-x-auto overflow-y-auto whitespace-pre rounded-lg border border-token-border p-3 font-mono text-sm"
                >
                    {pendingWorktree.outputText.length > 0
                        ? <AnsiRenderer className="text-sm">{pendingWorktree.outputText}</AnsiRenderer>
                        : <span className="text-token-input-placeholder-foreground">
                            <FormattedMessage id="worktreeInitV2.output.empty" defaultMessage="Waiting for output…" />
                        </span>
                    }
                </div>
            </div>
        </CenteredLayout>
    );
}

/**
 * createFromEntry — 从 pending worktree entry 创建对话
 * 混淆名: eGe
 */
async function createFromEntry({ entry, mcpManager, workspaceRoot }) {
    if (entry.launchMode === "fork-conversation") {
        return mcpManager.forkConversationFromLatest({
            sourceConversationId: entry.sourceConversationId,
            cwd: workspaceRoot,
            workspaceRoots: [workspaceRoot],
            collaborationMode: entry.sourceCollaborationMode,
        });
    }
    if (entry.launchMode !== "start-conversation") {
        throw new Error(`Unsupported launch mode: ${entry.launchMode}`);
    }
    return mcpManager.startConversation(
        enrichStartParams({
            ...entry.startConversationParamsInput,
            workspaceRoots: [workspaceRoot],
            cwd: workspaceRoot,
        }),
    );
}

export { WorktreeInitPage };
