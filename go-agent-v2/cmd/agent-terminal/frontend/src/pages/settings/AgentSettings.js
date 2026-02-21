// =============================================================================
// Codex.app — Agent 设置 (AgentSettings)
// Chunk: agent-settings-C10jU9a3.js (231 行格式化)
// 路由: /settings → agent-settings (lazy loaded)
//
// 功能:
//   - 打开 config.toml 编辑器 (自定义 agent 行为)
//   - "Restart Codex after editing to apply changes" 提示
//   - 外部链接到 Docs
//   - Open Source Licenses 入口 → /settings/open-source-licenses
// =============================================================================

import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useConfigData } from "../../hooks/useConfig";
import { useQuery, useMutation } from "../../hooks/useAppQuery";
import { useWindowType } from "../../hooks/useWindowType";
import { Button } from "../../components/Button";
import { FormattedMessage } from "../../components/FormattedMessage";

// 常量
const CONFIG_DOCS_URL = "https://codex.openai.com/docs/configuration";
const StaleTime = { ONE_MINUTE: 60_000 };
const ConfigKeys = {
    RUN_CODEX_IN_WSL: "run_codex_in_wsl",
};
const messages = {
    openConfigToml: { id: "settings.agent.openConfig", defaultMessage: "Open config.toml" },
    openConfigTomlWsl: { id: "settings.agent.openConfig.wsl", defaultMessage: "Open config.toml (WSL)" },
};

// ===================== 辅助组件 =====================

function SettingsPage({ title, subtitle, children }) {
    return (
        <div className="settings-page flex flex-col gap-4">
            <div>
                <h2 className="text-lg font-semibold text-token-foreground">{title}</h2>
                {subtitle && <p className="text-sm text-token-description-foreground mt-1">{subtitle}</p>}
            </div>
            {children}
        </div>
    );
}

function SettingsSlug({ slug }) {
    const labels = { agent: "Agent" };
    return <span>{labels[slug] ?? slug}</span>;
}

function Card({ children }) {
    return <div className="border border-token-border rounded-lg">{children}</div>;
}
Card.Content = function CardContent({ children }) {
    return <div className="p-4">{children}</div>;
};

function SettingsList({ children }) {
    return <div className="flex flex-col gap-4">{children}</div>;
}

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

/**
 * usePlatformInfo — 简化的平台信息 hook
 */
function usePlatformInfo() {
    return useQuery("platform-info", {
        placeholderData: { platform: process?.platform ?? "darwin", hasWsl: false },
    });
}

// ===================== AgentSettings 主组件 =====================

/**
 * AgentSettings — Agent 配置页
 *
 * 结构:
 *   AgentSettings
 *   ├── SettingsPage (title="Agent", subtitle="...")
 *   │   └── Card > Card.Content > SettingsList
 *   │       ├── SettingsRow (label="config.toml", description="Edit your config to customize agent behavior")
 *   │       │   └── OpenConfigButton (打开 config.toml, 支持 WSL)
 *   │       └── OpenSourceLicensesRow (→ /settings/open-source-licenses)
 */
function AgentSettings() {
    const navigate = useNavigate();
    const { data: platform } = usePlatformInfo();
    const { data: wslEnabled } = useConfigData(ConfigKeys.RUN_CODEX_IN_WSL);
    const { data: codexHome } = useQuery("codex-home");
    const windowType = useWindowType();

    const isElectron = windowType === "electron";
    const { data: openTargets } = useQuery("open-in-targets", {
        params: { cwd: null },
        queryConfig: { enabled: isElectron, staleTime: StaleTime.ONE_MINUTE },
    });

    const openFileMutation = useMutation("open-file");
    const isWSL = platform?.platform === "win32" && platform?.hasWsl && wslEnabled;
    const configPath = codexHome?.codexHome ? `${codexHome.codexHome}/config.toml` : null;
    const preferredTarget = openTargets?.preferredTarget ?? undefined;

    const handleOpenConfig = useCallback(() => {
        if (configPath) {
            openFileMutation.mutate({ path: configPath, cwd: null, target: preferredTarget });
        }
    }, [configPath, preferredTarget, openFileMutation]);

    return (
        <SettingsPage
            title={<SettingsSlug slug="agent" />}
            subtitle={
                <FormattedMessage
                    id="settings.agent.configuration.subtitle"
                    defaultMessage="These settings apply to anywhere Codex is used"
                />
            }
        >
            <Card>
                <Card.Content>
                    <SettingsList>
                        {/* config.toml 编辑器 */}
                        <SettingsRow
                            label={<FormattedMessage id="settings.agent.configuration.configToml" defaultMessage="config.toml" />}
                            description={
                                <>
                                    <FormattedMessage id="settings.agent.configuration.configToml.description" defaultMessage="Edit your config to customize agent behavior" />
                                    {" "}
                                    <FormattedMessage id="settings.agent.configuration.configToml.restartNote" defaultMessage="Restart Codex after editing to apply changes" />
                                    {" "}
                                    <a className="text-token-primary inline-flex items-center gap-1" href={CONFIG_DOCS_URL} target="_blank" rel="noreferrer">
                                        <FormattedMessage id="settings.agent.configuration.configToml.docs" defaultMessage="Docs" />
                                        <ExternalLinkIcon className="w-3 h-3" />
                                    </a>
                                </>
                            }
                            control={
                                <Button color="secondary" size="sm" onClick={handleOpenConfig} disabled={!configPath}>
                                    {isWSL
                                        ? <FormattedMessage {...messages.openConfigTomlWsl} />
                                        : <FormattedMessage {...messages.openConfigToml} />
                                    }
                                </Button>
                            }
                        />

                        {/* Open Source Licenses */}
                        <SettingsRow
                            label={<FormattedMessage id="settings.openSourceLicenses.rowLabel" defaultMessage="Open source licenses" />}
                            description={<FormattedMessage id="settings.openSourceLicenses.rowDescription" defaultMessage="Third-party notices for bundled dependencies" />}
                            control={
                                <Button color="secondary" size="sm" onClick={() => navigate("/settings/open-source-licenses")}>
                                    <FormattedMessage id="settings.openSourceLicenses.view" defaultMessage="View" />
                                </Button>
                            }
                        />
                    </SettingsList>
                </Card.Content>
            </Card>
        </SettingsPage>
    );
}

export { AgentSettings };
