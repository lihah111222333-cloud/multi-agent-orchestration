// =============================================================================
// Codex.app — Git 设置 (GitSettings)
// Chunk: git-settings-Ctlnozog.js (609 行格式化)
// 路由: /settings → git-settings (lazy loaded)
//
// 功能:
//   - Branch prefix (文本输入, 如 "codex/")
//   - Always force push 开关 (--force-with-lease)
//   - Commit instructions (textarea, 附加到 commit message 生成)
//   - Pull request instructions (textarea, 附加到 PR body)
// =============================================================================

import { useState } from "react";
import { useIntl } from "../../hooks/useIntl";
import { useToast } from "../../hooks/useToast";
import { useConfigData } from "../../hooks/useConfig";
import { Button } from "../../components/Button";
import { FormattedMessage } from "../../components/FormattedMessage";

// 常量
const ConfigKeys = {
    GIT_BRANCH_PREFIX: "git_branch_prefix",
    GIT_ALWAYS_FORCE_PUSH: "git_always_force_push",
    GIT_COMMIT_INSTRUCTIONS: "git_commit_instructions",
    GIT_PR_INSTRUCTIONS: "git_pr_instructions",
};

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
    const labels = { "git-settings": "Git Settings" };
    return <span>{labels[slug] ?? slug}</span>;
}

function Card({ className, children }) {
    return <div className={`border border-token-border rounded-lg ${className ?? ""}`}>{children}</div>;
}
Card.Content = function CardContent({ children }) { return <div className="p-4">{children}</div>; };
Card.Header = function CardHeader({ title, subtitle, actions }) {
    return (
        <div className="flex items-start justify-between p-4 border-b border-token-border">
            <div>
                <div className="text-sm font-medium text-token-foreground">{title}</div>
                {subtitle && <div className="text-xs text-token-description-foreground mt-1">{subtitle}</div>}
            </div>
            {actions && <div className="flex-shrink-0">{actions}</div>}
        </div>
    );
};

function SettingsList({ children }) { return <div className="flex flex-col gap-4">{children}</div>; }

function SettingsRow({ label, description, control }) {
    return (
        <div className="flex items-start justify-between gap-4">
            <div className="flex-1">
                <div className="text-sm font-medium text-token-foreground">{label}</div>
                {description && <div className="text-xs text-token-description-foreground mt-1">{description}</div>}
            </div>
            {control && <div className="flex-shrink-0">{control}</div>}
        </div>
    );
}

function Toggle({ checked, disabled, onChange }) {
    return (
        <button
            className={`relative inline-flex h-6 w-10 items-center rounded-full transition-colors ${checked ? "bg-token-primary" : "bg-gray-400"} ${disabled ? "opacity-50 cursor-not-allowed" : "cursor-pointer"}`}
            onClick={() => !disabled && onChange(!checked)}
            disabled={disabled}
        >
            <span className={`inline-block h-4 w-4 rounded-full bg-white transition-transform ${checked ? "translate-x-5" : "translate-x-0.5"}`} />
        </button>
    );
}

// ===================== GitSettings 主组件 =====================

/**
 * GitSettings — Git 配置页
 */
function GitSettings() {
    const intl = useIntl();
    const toast = useToast();

    // 状态管理: 每个设置项使用 useConfigData
    const { data: branchPrefix, setData: setBranchPrefix, isLoading: prefixLoading } = useConfigData(ConfigKeys.GIT_BRANCH_PREFIX);
    const { data: forcePush, setData: setForcePush, isLoading: forcePushLoading } = useConfigData(ConfigKeys.GIT_ALWAYS_FORCE_PUSH);
    const { data: commitInstructions, setData: setCommitInstructions, isLoading: commitLoading } = useConfigData(ConfigKeys.GIT_COMMIT_INSTRUCTIONS);
    const { data: prInstructions, setData: setPrInstructions, isLoading: prLoading } = useConfigData(ConfigKeys.GIT_PR_INSTRUCTIONS);

    // Branch prefix: input + onBlur save
    const [localBranchPrefix, setLocalBranchPrefix] = useState(null);
    const displayBranchPrefix = localBranchPrefix ?? branchPrefix;
    const branchPrefixDirty = localBranchPrefix != null && localBranchPrefix !== branchPrefix;

    // Commit instructions: textarea + Save button
    const [localCommit, setLocalCommit] = useState(null);
    const displayCommit = localCommit ?? (commitInstructions ?? "");
    const commitDirty = localCommit != null && localCommit !== (commitInstructions ?? "");

    // PR instructions: textarea + Save button
    const [localPR, setLocalPR] = useState(null);
    const displayPR = localPR ?? (prInstructions ?? "");
    const prDirty = localPR != null && localPR !== (prInstructions ?? "");

    return (
        <SettingsPage title={<SettingsSlug slug="git-settings" />}>
            {/* Card 1: Branch prefix + Force push */}
            <Card>
                <Card.Content>
                    <SettingsList>
                        <SettingsRow
                            label={<FormattedMessage id="settings.git.branchPrefix.label" defaultMessage="Branch prefix" />}
                            description={<FormattedMessage id="settings.git.branchPrefix.description" defaultMessage="Prefix used when creating new branches in Codex" />}
                            control={
                                <input
                                    className="bg-token-background-secondary text-token-foreground w-56 rounded-md border border-token-border px-2.5 py-1.5 text-sm outline-none"
                                    value={displayBranchPrefix ?? ""}
                                    onChange={(e) => setLocalBranchPrefix(e.target.value === branchPrefix ? null : e.target.value)}
                                    onBlur={() => branchPrefixDirty && setBranchPrefix(displayBranchPrefix)}
                                    placeholder={intl.formatMessage({ id: "settings.git.branchPrefix.placeholder", defaultMessage: "codex/" })}
                                    disabled={prefixLoading}
                                />
                            }
                        />
                        <SettingsRow
                            label={<FormattedMessage id="settings.git.forcePush.label" defaultMessage="Always force push" />}
                            description={<FormattedMessage id="settings.git.forcePush.description" defaultMessage="Use --force-with-lease when pushing from Codex" />}
                            control={
                                <Toggle
                                    checked={!!forcePush}
                                    disabled={forcePushLoading}
                                    onChange={(val) => setForcePush(val)}
                                />
                            }
                        />
                    </SettingsList>
                </Card.Content>
            </Card>

            {/* Card 2: Commit Instructions */}
            <Card>
                <Card.Header
                    title={<FormattedMessage id="settings.git.commitInstructions.label" defaultMessage="Commit instructions" />}
                    subtitle={<FormattedMessage id="settings.git.commitInstructions.description" defaultMessage="Added to commit message generation prompts" />}
                    actions={
                        <Button color="secondary" size="sm" disabled={!commitDirty} loading={commitLoading} onClick={() => commitDirty && setCommitInstructions(displayCommit)}>
                            <FormattedMessage id="settings.git.commitInstructions.save" defaultMessage="Save" />
                        </Button>
                    }
                />
                <Card.Content>
                    <textarea
                        className="w-full bg-token-background-secondary text-token-foreground rounded-md border border-token-border px-2.5 py-2 text-sm outline-none resize-y"
                        value={displayCommit}
                        onChange={(e) => setLocalCommit(e.target.value === (commitInstructions ?? "") ? null : e.target.value)}
                        placeholder={intl.formatMessage({ id: "settings.git.commitInstructions.placeholder", defaultMessage: "Add commit message guidance…" })}
                        disabled={commitLoading}
                        rows={6}
                    />
                </Card.Content>
            </Card>

            {/* Card 3: PR Instructions */}
            <Card>
                <Card.Header
                    title={<FormattedMessage id="settings.git.prInstructions.label" defaultMessage="Pull request instructions" />}
                    subtitle={<FormattedMessage id="settings.git.prInstructions.description" defaultMessage="Appended to pull request bodies created by Codex" />}
                    actions={
                        <Button color="secondary" size="sm" disabled={!prDirty} loading={prLoading} onClick={() => prDirty && setPrInstructions(displayPR)}>
                            <FormattedMessage id="settings.git.prInstructions.save" defaultMessage="Save" />
                        </Button>
                    }
                />
                <Card.Content>
                    <textarea
                        className="w-full bg-token-background-secondary text-token-foreground rounded-md border border-token-border px-2.5 py-2 text-sm outline-none resize-y"
                        value={displayPR}
                        onChange={(e) => setLocalPR(e.target.value === (prInstructions ?? "") ? null : e.target.value)}
                        placeholder={intl.formatMessage({ id: "settings.git.prInstructions.placeholder", defaultMessage: "Add pull request guidance…" })}
                        disabled={prLoading}
                        rows={6}
                    />
                </Card.Content>
            </Card>
        </SettingsPage>
    );
}

export { GitSettings };
