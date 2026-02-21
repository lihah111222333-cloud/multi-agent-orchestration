// =============================================================================
// Codex.app — 个性化设置 (PersonalizationSettings)
// Chunk: personalization-settings-BdP4UtGK.js (452 行格式化)
// 路由: /settings → personalization (lazy loaded)
//
// 功能:
//   - Personality 选择 (Friendly / Pragmatic)
//   - Custom Instructions 编辑 (agents.md)
// =============================================================================

import { useState } from "react";
import { useIntl } from "../../hooks/useIntl";
import { useToast } from "../../hooks/useToast";
import { useConfigData } from "../../hooks/useConfig";
import { useQuery, useMutation } from "../../hooks/useAppQuery";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "../../components/Button";
import { FormattedMessage } from "../../components/FormattedMessage";

// 常量
const AGENTS_DOCS_URL = "https://codex.openai.com/docs/custom-instructions";

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
    const labels = { personalization: "Personalization" };
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

function ExternalLinkIcon({ className }) {
    return (
        <svg className={className} width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M5 1H2a1 1 0 00-1 1v8a1 1 0 001 1h8a1 1 0 001-1V7" />
            <path d="M7 1h4v4M11 1L5 7" />
        </svg>
    );
}

function Spinner({ className }) {
    return <span className={`inline-block animate-spin rounded-full border-2 border-token-border border-t-token-primary ${className ?? "w-4 h-4"}`} />;
}

function PersonalityDropdown({ options, selected, onSelect, disabled }) {
    return (
        <select
            className="bg-token-bg-secondary text-token-foreground rounded-md border border-token-border px-2.5 py-1.5 text-sm outline-none"
            value={selected?.value}
            onChange={(e) => onSelect(e.target.value)}
            disabled={disabled}
        >
            {options.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
        </select>
    );
}

/**
 * useFeatureFlag — 简化的 feature flag hook
 * 原始实现使用 Statsig
 */
function useFeatureFlag(flagId) {
    // 默认启用所有 feature flags
    return true;
}

/**
 * usePersonality — 个性选择 hook
 */
function usePersonality() {
    const { data: personality, setData: setPersonality } = useConfigData("personality");
    return { personality: personality ?? "friendly", setPersonality };
}

/**
 * queryKey — 生成 react-query key
 */
function queryKey(method) {
    return ["codex-rpc", method];
}

// ===================== PersonalizationSettings 主组件 =====================

/**
 * PersonalizationSettings — 个性化配置页
 */
function PersonalizationSettings() {
    const intl = useIntl();
    const toast = useToast();
    const queryClient = useQueryClient();
    const personalityEnabled = useFeatureFlag("1444479692");
    const { personality, setPersonality } = usePersonality();

    // agents.md 编辑
    const [localContent, setLocalContent] = useState(null);
    const { data: agentsMd, error: loadError, isFetching: isLoading, refetch } = useQuery("codex-agents-md");
    const savedContent = agentsMd?.contents ?? "";
    const displayContent = localContent ?? savedContent;
    const isDirty = localContent != null && localContent !== savedContent;
    const isReady = agentsMd != null;
    const isInitialLoad = !isReady && isLoading;
    const hasLoadError = loadError != null && agentsMd == null;

    const saveMutation = useMutation("codex-agents-md-save", {
        onSuccess: (data, variables) => {
            queryClient.setQueryData(queryKey("codex-agents-md"), { path: data?.path, contents: variables.contents });
            setLocalContent(null);
            toast.success(intl.formatMessage({ id: "settings.personalization.agents.save.success", defaultMessage: "Saved agents.md" }));
        },
        onError: () => {
            toast.danger(intl.formatMessage({ id: "settings.personalization.agents.save.error", defaultMessage: "Unable to save agents.md" }));
        },
    });

    const handleSave = () => {
        if (isReady && isDirty && !saveMutation.isPending) {
            saveMutation.mutate({ contents: displayContent });
        }
    };

    // Personality 选项
    const personalityOptions = [
        { value: "friendly", label: intl.formatMessage({ id: "composer.personalitySlashCommand.label.friendly", defaultMessage: "Friendly" }), description: intl.formatMessage({ id: "composer.personalitySlashCommand.description.friendly", defaultMessage: "Warm, collaborative, and helpful" }) },
        { value: "pragmatic", label: intl.formatMessage({ id: "composer.personalitySlashCommand.label.pragmatic", defaultMessage: "Pragmatic" }), description: intl.formatMessage({ id: "composer.personalitySlashCommand.description.pragmatic", defaultMessage: "Concise, task-focused, and direct" }) },
    ];
    const selectedPersonality = personalityOptions.find((p) => p.value === personality) ?? personalityOptions[0];

    return (
        <SettingsPage title={<SettingsSlug slug="personalization" />}>
            {/* Personality 选择 (feature flagged) */}
            {personalityEnabled && (
                <Card className="gap-2">
                    <Card.Content>
                        <SettingsList>
                            <SettingsRow
                                label={<FormattedMessage id="settings.personalization.personality.label" defaultMessage="Personality" />}
                                description={<FormattedMessage id="settings.personalization.personality.description" defaultMessage="Choose a default tone for Codex responses" />}
                                control={
                                    <PersonalityDropdown
                                        options={personalityOptions}
                                        selected={selectedPersonality}
                                        onSelect={(val) => setPersonality(val)}
                                        disabled={!isReady || saveMutation.isPending}
                                    />
                                }
                            />
                        </SettingsList>
                    </Card.Content>
                </Card>
            )}

            {/* Custom Instructions (agents.md) */}
            <Card className="gap-2">
                <Card.Header
                    title={<FormattedMessage id="settings.personalization.agents.title" defaultMessage="Custom instructions" />}
                    subtitle={
                        <FormattedMessage
                            id="settings.personalization.agents.description"
                            defaultMessage="Edit instructions that tailor Codex to you."
                        />
                    }
                />
                <Card.Content>
                    {hasLoadError ? (
                        <div className="flex items-center justify-between gap-3">
                            <div className="text-token-description-foreground text-sm">
                                <FormattedMessage id="settings.personalization.agents.loadError" defaultMessage="Unable to load agents.md." />
                            </div>
                            <Button className="flex-shrink-0" color="secondary" onClick={refetch} size="sm">
                                <FormattedMessage id="settings.personalization.agents.retry" defaultMessage="Retry" />
                            </Button>
                        </div>
                    ) : (
                        <div className="flex flex-col gap-3">
                            {isInitialLoad ? (
                                <div className="text-token-description-foreground flex items-center gap-2 text-sm">
                                    <Spinner className="w-3 h-3" />
                                    <FormattedMessage id="settings.personalization.agents.loading" defaultMessage="Loading agents.md…" />
                                </div>
                            ) : (
                                <textarea
                                    id="personal-agents-editor"
                                    className="bg-token-bg-secondary text-token-foreground font-mono w-full rounded-md border border-token-border px-2.5 py-2 text-sm outline-none resize-y"
                                    disabled={!isReady || saveMutation.isPending}
                                    placeholder={intl.formatMessage({ id: "settings.personalization.agents.placeholder", defaultMessage: "Add your custom instructions…" })}
                                    rows={12}
                                    value={displayContent}
                                    onChange={(e) => {
                                        const val = e.target.value;
                                        setLocalContent(val === savedContent ? null : val);
                                    }}
                                />
                            )}
                            <div className="flex items-center justify-end gap-2">
                                <Button color="primary" disabled={!isDirty || !isReady} loading={saveMutation.isPending} onClick={handleSave} size="sm">
                                    <FormattedMessage id="settings.personalization.agents.save" defaultMessage="Save" />
                                </Button>
                            </div>
                        </div>
                    )}
                </Card.Content>
            </Card>
        </SettingsPage>
    );
}

export { PersonalizationSettings };
