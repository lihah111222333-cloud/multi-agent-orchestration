// =============================================================================
// Codex.app — Git Settings 设置子页面
// 路由: /settings/git-settings
// Chunk: git-settings-*.js (lazy loaded)
//
// 功能: Git 集成设置
//   - 自动 commit 开关
//   - Commit message 模板
//   - Branch 命名策略
//   - Worktree 默认配置
// =============================================================================

import { useConfigData } from "../../hooks/useConfig";

export default function GitSettingsPage() {
    const { data: autoCommit, setData: setAutoCommit } = useConfigData("git.auto_commit");
    const { data: branchStrategy, setData: setBranchStrategy } = useConfigData("git.branch_strategy");
    const { data: commitTemplate, setData: setCommitTemplate } = useConfigData("git.commit_template");

    return (
        <div className="git-settings flex flex-col gap-8">
            <h2 className="text-lg font-semibold text-token-foreground">Git Settings</h2>

            {/* 自动 Commit */}
            <SettingsToggle
                label="Auto-commit"
                description="Automatically commit changes after each turn completes"
                checked={autoCommit ?? false}
                onChange={setAutoCommit}
            />

            {/* Branch 策略 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground">Branch Strategy</h3>
                <p className="text-xs text-token-description-foreground mt-0.5 mb-3">
                    How Codex creates branches for new conversations
                </p>
                <select
                    className="bg-token-background-secondary border border-token-border rounded-md px-3 py-2 text-sm text-token-foreground"
                    value={branchStrategy ?? "auto"}
                    onChange={(e) => setBranchStrategy(e.target.value)}
                >
                    <option value="auto">Auto (create branch if needed)</option>
                    <option value="always">Always create new branch</option>
                    <option value="never">Never (commit to current branch)</option>
                </select>
            </div>

            {/* Commit 模板 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground">Commit Message Template</h3>
                <p className="text-xs text-token-description-foreground mt-0.5 mb-3">
                    Template for auto-generated commit messages. Use {"{{summary}}"} for AI summary.
                </p>
                <input
                    className="w-full bg-token-background-secondary border border-token-border rounded-md px-3 py-2 text-sm text-token-foreground font-mono"
                    value={commitTemplate ?? "codex: {{summary}}"}
                    onChange={(e) => setCommitTemplate(e.target.value)}
                    placeholder="codex: {{summary}}"
                />
            </div>
        </div>
    );
}

function SettingsToggle({ label, description, checked, onChange }) {
    return (
        <div className="flex items-center justify-between">
            <div>
                <h3 className="text-sm font-medium text-token-foreground">{label}</h3>
                {description && <p className="text-xs text-token-description-foreground mt-0.5">{description}</p>}
            </div>
            <button
                className={`relative w-10 h-5 rounded-full transition-colors ${checked ? "bg-token-primary" : "bg-token-border"}`}
                onClick={() => onChange(!checked)}
            >
                <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform ${checked ? "translate-x-5" : "translate-x-0.5"}`} />
            </button>
        </div>
    );
}
