// =============================================================================
// Codex.app — Worktrees 设置 (WorktreesSettingsPage)
// Chunk: worktrees-settings-page-CnWJAezi.js (554 行格式化)
// 路由: /settings → worktrees-settings-page (lazy loaded)
//
// 功能:
//   - Git worktree 管理和配置
//   - 查看现有 worktrees 列表及状态
//   - Worktree 设置 (如 stable worktree 模式)
//   - 清理/删除 worktrees
// =============================================================================

import { useQuery } from "../../hooks/useAppQuery";
import { useConfigData } from "../../hooks/useConfig";
import { Button } from "../../components/Button";
import { FormattedMessage } from "../../components/FormattedMessage";

// ===================== 辅助组件 =====================

function SettingsPage({ title, children }) {
    return (
        <div className="settings-page flex flex-col gap-4">
            <h2 className="text-lg font-semibold text-token-foreground">{title}</h2>
            {children}
        </div>
    );
}

function SettingsSlug({ slug }) {
    const labels = { worktrees: "Worktrees" };
    return <span>{labels[slug] ?? slug}</span>;
}

// ===================== WorktreesList =====================

function WorktreesList() {
    const { data: worktrees, isLoading } = useQuery("worktrees/list", {
        placeholderData: [],
    });

    if (isLoading) {
        return <div className="text-sm text-token-description-foreground">Loading worktrees...</div>;
    }

    const wts = worktrees ?? [];

    if (wts.length === 0) {
        return (
            <div className="border border-token-border rounded-lg p-4">
                <div className="text-center py-8 text-token-description-foreground text-sm">
                    No active worktrees
                </div>
            </div>
        );
    }

    return (
        <div className="flex flex-col gap-2">
            {wts.map((wt, i) => (
                <div key={i} className="flex items-center justify-between p-3 border border-token-border rounded-lg">
                    <div>
                        <div className="text-sm font-medium text-token-foreground font-mono">{wt.path}</div>
                        <div className="text-xs text-token-description-foreground mt-0.5">
                            {wt.branch || "detached"}
                            {wt.conversationId && <span className="ml-2 text-token-primary">• Active task</span>}
                        </div>
                    </div>
                    <Button color="ghost" size="sm">Remove</Button>
                </div>
            ))}
        </div>
    );
}

// ===================== WorktreeConfig =====================

function WorktreeConfig() {
    const { data: stableWorktree, setData: setStableWorktree } = useConfigData("stable_worktree");

    return (
        <div className="border border-token-border rounded-lg p-4 flex flex-col gap-4">
            <h3 className="text-sm font-medium text-token-foreground">Settings</h3>

            <div className="flex items-start justify-between gap-4">
                <div>
                    <div className="text-sm text-token-foreground">Stable worktree mode</div>
                    <div className="text-xs text-token-description-foreground mt-0.5">
                        Reuse a single worktree for all tasks instead of creating new ones
                    </div>
                </div>
                <button
                    className={`relative inline-flex h-6 w-10 items-center rounded-full transition-colors ${stableWorktree ? "bg-token-primary" : "bg-gray-400"} cursor-pointer`}
                    onClick={() => setStableWorktree(!stableWorktree)}
                >
                    <span className={`inline-block h-4 w-4 rounded-full bg-white transition-transform ${stableWorktree ? "translate-x-5" : "translate-x-0.5"}`} />
                </button>
            </div>

            <div className="flex gap-2">
                <Button color="secondary" size="sm">
                    Clean up all worktrees
                </Button>
            </div>
        </div>
    );
}

// ===================== WorktreesSettingsPage 主组件 =====================

/**
 * WorktreesSettingsPage — Worktree 管理页
 */
function WorktreesSettingsPage() {
    return (
        <SettingsPage title={<SettingsSlug slug="worktrees" />}>
            <WorktreesList />
            <WorktreeConfig />
        </SettingsPage>
    );
}

export { WorktreesSettingsPage };
