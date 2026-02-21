// =============================================================================
// Codex.app — 本地环境设置 (LocalEnvironmentsSettings)
// Chunk: local-environments-settings-page-BgGzLnZS.js (4921 行格式化)
// 路由: /settings → local-environments (lazy loaded)
//
// 功能: (最大的设置 chunk, 4921 行)
//   - 本地开发环境配置和管理
//   - 环境变量设置
//   - 运行时配置 (Node, Python, etc.)
//   - 沙箱/容器设置
//   - CLI 路径配置
// =============================================================================

import { useState } from "react";
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
    const labels = { "local-environments": "Local Environments" };
    return <span>{labels[slug] ?? slug}</span>;
}

// ===================== EnvironmentsList =====================

function EnvironmentsList() {
    const { data: environments, isLoading } = useQuery("local-environments/list", {
        placeholderData: [],
    });

    if (isLoading) {
        return <div className="text-sm text-token-description-foreground">Loading environments...</div>;
    }

    const envs = environments ?? [];

    if (envs.length === 0) {
        return (
            <div className="border border-token-border rounded-lg p-4">
                <div className="text-center py-8 text-token-description-foreground text-sm">
                    No local environments configured
                </div>
            </div>
        );
    }

    return (
        <div className="flex flex-col gap-3">
            {envs.map((env, i) => (
                <div key={env.id ?? i} className="border border-token-border rounded-lg p-4">
                    <div className="flex items-center justify-between">
                        <div>
                            <div className="text-sm font-medium text-token-foreground">{env.name ?? "Default"}</div>
                            <div className="text-xs text-token-description-foreground mt-0.5">{env.runtime ?? "System default"}</div>
                        </div>
                        <Button color="ghost" size="sm">Edit</Button>
                    </div>
                    {env.envVars && Object.keys(env.envVars).length > 0 && (
                        <div className="mt-2 text-xs text-token-description-foreground font-mono">
                            {Object.keys(env.envVars).length} environment variables
                        </div>
                    )}
                </div>
            ))}
        </div>
    );
}

// ===================== EnvironmentConfig =====================

function EnvironmentConfig() {
    const { data: sandboxPolicy, setData: setSandboxPolicy } = useConfigData("sandbox_policy");
    const { data: cliPath, setData: setCliPath } = useConfigData("codex_cli_path");
    const [localCliPath, setLocalCliPath] = useState(null);

    return (
        <div className="border border-token-border rounded-lg p-4 flex flex-col gap-4">
            <h3 className="text-sm font-medium text-token-foreground">Configuration</h3>

            {/* Sandbox policy */}
            <div className="flex items-start justify-between gap-4">
                <div>
                    <div className="text-sm text-token-foreground">Sandbox mode</div>
                    <div className="text-xs text-token-description-foreground mt-0.5">Run commands in a sandboxed environment</div>
                </div>
                <select
                    className="bg-token-bg-secondary text-token-foreground rounded-md border border-token-border px-2.5 py-1.5 text-sm outline-none"
                    value={sandboxPolicy ?? "sandbox"}
                    onChange={(e) => setSandboxPolicy(e.target.value)}
                >
                    <option value="sandbox">Sandbox (macOS seatbelt)</option>
                    <option value="no-sandbox">No sandbox</option>
                </select>
            </div>

            {/* CLI path */}
            <div className="flex items-start justify-between gap-4">
                <div>
                    <div className="text-sm text-token-foreground">CLI path</div>
                    <div className="text-xs text-token-description-foreground mt-0.5">Custom path to the Codex CLI binary</div>
                </div>
                <input
                    className="bg-token-bg-secondary text-token-foreground w-56 rounded-md border border-token-border px-2.5 py-1.5 text-sm outline-none font-mono"
                    value={localCliPath ?? cliPath ?? ""}
                    onChange={(e) => setLocalCliPath(e.target.value)}
                    onBlur={() => localCliPath != null && setCliPath(localCliPath)}
                    placeholder="auto-detect"
                />
            </div>
        </div>
    );
}

// ===================== LocalEnvironmentsSettings 主组件 =====================

/**
 * LocalEnvironmentsSettings — 本地环境配置页
 */
function LocalEnvironmentsSettings() {
    return (
        <SettingsPage title={<SettingsSlug slug="local-environments" />}>
            <EnvironmentsList />
            <EnvironmentConfig />
        </SettingsPage>
    );
}

export { LocalEnvironmentsSettings };
