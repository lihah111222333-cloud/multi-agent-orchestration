// =============================================================================
// Codex.app — Local Environments 设置子页面
// 路由: /settings/local-environments
// Chunk: local-environments-settings-page-*.js (lazy loaded)
//
// 功能: 本地环境管理
//   - Shell 设置 (bash / zsh / fish)
//   - 环境变量配置
//   - PATH 管理
//   - 自定义 setup 脚本
// =============================================================================

import { useConfigData } from "../../hooks/useConfig";
import { Button } from "../../components/Button";

export default function LocalEnvironmentsPage() {
    const { data: shellConfig, setData: setShellConfig } = useConfigData("environment.shell");
    const { data: envVars, setData: setEnvVars } = useConfigData("environment.env_vars");
    const { data: setupScript, setData: setSetupScript } = useConfigData("environment.setup_script");

    return (
        <div className="local-environments flex flex-col gap-8">
            <h2 className="text-lg font-semibold text-token-foreground">Local Environments</h2>

            {/* Shell 设置 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground">Default Shell</h3>
                <p className="text-xs text-token-description-foreground mt-0.5 mb-3">
                    Shell used for command execution
                </p>
                <select
                    className="bg-token-background-secondary border border-token-border rounded-md px-3 py-2 text-sm text-token-foreground"
                    value={shellConfig ?? "auto"}
                    onChange={(e) => setShellConfig(e.target.value)}
                >
                    <option value="auto">Auto-detect</option>
                    <option value="/bin/zsh">zsh</option>
                    <option value="/bin/bash">bash</option>
                    <option value="/usr/bin/fish">fish</option>
                </select>
            </div>

            {/* 环境变量 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground">Environment Variables</h3>
                <p className="text-xs text-token-description-foreground mt-0.5 mb-3">
                    Additional environment variables passed to commands
                </p>
                <textarea
                    className="w-full h-32 bg-token-background-secondary border border-token-border rounded-lg p-3 text-sm text-token-foreground font-mono resize-y outline-none"
                    placeholder={"KEY=value\nANOTHER_KEY=another_value"}
                    value={envVars ?? ""}
                    onChange={(e) => setEnvVars(e.target.value)}
                />
            </div>

            {/* Setup 脚本 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground">Setup Script</h3>
                <p className="text-xs text-token-description-foreground mt-0.5 mb-3">
                    Script to run before each command (e.g., activate virtualenv)
                </p>
                <textarea
                    className="w-full h-24 bg-token-background-secondary border border-token-border rounded-lg p-3 text-sm text-token-foreground font-mono resize-y outline-none"
                    placeholder="# e.g., source ~/.nvm/nvm.sh && nvm use 20"
                    value={setupScript ?? ""}
                    onChange={(e) => setSetupScript(e.target.value)}
                />
                <Button color="primary" size="sm" className="mt-2" onClick={() => setSetupScript(setupScript)}>
                    Save
                </Button>
            </div>
        </div>
    );
}
