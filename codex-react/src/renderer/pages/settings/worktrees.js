// =============================================================================
// Codex.app — Worktrees 设置子页面
// 路由: /settings/worktrees
// Chunk: worktrees-settings-page-*.js (lazy loaded)
//
// 功能: Git Worktree 管理
//   - 活跃 worktree 列表
//   - 清理/删除 worktree
//   - 默认 worktree 设置
// =============================================================================

import { useState, useCallback } from "react";
import { useQuery, useMutation } from "../../hooks/useAppQuery";
import { Button } from "../../components/Button";

export default function WorktreesSettingsPage() {
    const { data: worktrees, isLoading, refetch } = useQuery("worktrees/list", {
        placeholderData: [],
    });
    const cleanupMutation = useMutation("worktrees/cleanup");
    const removeMutation = useMutation("worktrees/remove");

    const handleCleanup = useCallback(async () => {
        await cleanupMutation.mutateAsync({});
        refetch();
    }, [cleanupMutation, refetch]);

    const handleRemove = useCallback(async (wt) => {
        await removeMutation.mutateAsync({ path: wt.path });
        refetch();
    }, [removeMutation, refetch]);

    return (
        <div className="worktrees-settings flex flex-col gap-6">
            <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-token-foreground">Worktrees</h2>
                <Button
                    color="ghost"
                    size="sm"
                    loading={cleanupMutation.isPending}
                    onClick={handleCleanup}
                >
                    Clean up
                </Button>
            </div>

            <p className="text-sm text-token-description-foreground">
                Git worktrees allow Codex to work on tasks in isolated branches without disrupting your current work.
            </p>

            {/* Worktree 列表 */}
            {isLoading ? (
                <div className="text-sm text-token-description-foreground">Loading...</div>
            ) : (worktrees ?? []).length === 0 ? (
                <div className="text-center py-8 text-token-description-foreground text-sm">
                    No active worktrees
                </div>
            ) : (
                <div className="flex flex-col gap-2">
                    {(worktrees ?? []).map((wt, i) => (
                        <div key={i} className="flex items-center justify-between p-3 border border-token-border rounded-lg">
                            <div>
                                <div className="text-sm font-medium text-token-foreground font-mono">{wt.path}</div>
                                <div className="text-xs text-token-description-foreground mt-0.5">{wt.branch || "detached"}</div>
                            </div>
                            <Button
                                color="ghost"
                                size="sm"
                                loading={removeMutation.isPending}
                                onClick={() => handleRemove(wt)}
                            >
                                Remove
                            </Button>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}
