// =============================================================================
// Codex.app — MCP 设置 (McpSettings)
// Chunk: mcp-settings-BYVIkKyW.js (2231 行格式化)
// 路由: /settings → mcp-settings (lazy loaded)
//
// 功能:
//   - MCP Server 管理 (添加/删除/编辑)
//   - 预置 MCP Server 推荐 (Linear, Notion, Figma, Playwright)
//   - Server 运行状态监控 (connected / disconnected / error)
//   - Server 配置编辑 (URL / Command+Args / Env vars)
//   - Tool 列表查看
// =============================================================================

import { useState, useCallback } from "react";
import { useQuery, useMutation } from "../../hooks/useAppQuery";
import { useConfigData } from "../../hooks/useConfig";
import { Button } from "../../components/Button";
import { FormattedMessage } from "../../components/FormattedMessage";
import { bridge } from "../../bridge";

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
    const labels = {
        "mcp-settings": "MCP Servers",
    };
    return <span>{labels[slug] ?? slug}</span>;
}

function Card({ className, children }) {
    return <div className={`border border-token-border rounded-lg ${className ?? ""}`}>{children}</div>;
}
Card.Content = function CardContent({ children }) {
    return <div className="p-4">{children}</div>;
};

function StatusDot({ status }) {
    const colorMap = {
        connected: "bg-green-500",
        connecting: "bg-yellow-500 animate-pulse",
        disconnected: "bg-gray-400",
        error: "bg-red-500",
    };
    return <span className={`inline-block w-2 h-2 rounded-full ${colorMap[status] ?? colorMap.disconnected}`} />;
}

// ===================== 预置推荐 MCP Servers =====================

const RECOMMENDED_SERVERS = [
    {
        id: "linear",
        label: "Linear",
        vendor: "Linear",
        description: "Issue tracking and project management",
        config: { keyPath: "mcp_servers.linear", value: { url: "https://mcp.linear.app/mcp" } },
    },
    {
        id: "notion",
        label: "Notion",
        vendor: "Notion",
        description: "Documents and knowledge base",
        config: { keyPath: "mcp_servers.notion", value: { url: "https://mcp.notion.com/mcp" } },
    },
    {
        id: "figma",
        label: "Figma",
        vendor: "Figma",
        description: "Design collaboration and assets",
        config: { keyPath: "mcp_servers.figma", value: { url: "https://mcp.figma.com/mcp" } },
    },
    {
        id: "playwright",
        label: "Playwright",
        vendor: "Microsoft",
        description: "Browser testing and automation",
        config: { keyPath: "mcp_servers.playwright", value: { command: "npx", args: ["@playwright/mcp@latest"] } },
    },
];

// ===================== 推荐 Servers 列表 =====================

function RecommendedServers({ servers, configuredIds, onInstall }) {
    const available = servers.filter((s) => !configuredIds.has(s.id));
    if (available.length === 0) return null;

    return (
        <Card>
            <Card.Content>
                <h3 className="text-sm font-medium text-token-foreground mb-3">Recommended</h3>
                <div className="flex flex-col gap-2">
                    {available.map((server) => (
                        <div key={server.id} className="flex items-center justify-between p-3 rounded-lg bg-token-background-secondary">
                            <div>
                                <span className="text-sm font-medium text-token-foreground">{server.label}</span>
                                <span className="text-xs text-token-description-foreground ml-2">{server.vendor}</span>
                                <p className="text-xs text-token-description-foreground mt-0.5">{server.description}</p>
                            </div>
                            <Button
                                color="secondary"
                                size="sm"
                                onClick={() => onInstall(server)}
                            >
                                Add
                            </Button>
                        </div>
                    ))}
                </div>
            </Card.Content>
        </Card>
    );
}

// ===================== 已配置 Servers 列表 =====================

function ConfiguredServers({ servers, onRemove, onToggle }) {
    if (servers.length === 0) {
        return (
            <Card>
                <Card.Content>
                    <div className="text-center py-8 text-token-description-foreground text-sm">
                        No MCP servers configured. Add a server to get started.
                    </div>
                </Card.Content>
            </Card>
        );
    }

    return (
        <Card>
            <Card.Content>
                <div className="flex flex-col gap-3">
                    {servers.map((server) => (
                        <div key={server.name ?? server.keyPath} className="flex items-center justify-between p-3 rounded-lg border border-token-border">
                            <div className="flex items-center gap-2">
                                <StatusDot status={server.status ?? "disconnected"} />
                                <span className="text-sm font-medium text-token-foreground">{server.name ?? server.label}</span>
                                {server.scope && (
                                    <span className="text-xs text-token-description-foreground px-1.5 py-0.5 bg-token-background-secondary rounded">
                                        {server.scope}
                                    </span>
                                )}
                            </div>
                            <div className="flex items-center gap-2">
                                {server.tools && server.tools.length > 0 && (
                                    <span className="text-xs text-token-description-foreground">{server.tools.length} tools</span>
                                )}
                                <Button color="ghost" size="sm" onClick={() => onRemove(server)}>
                                    Remove
                                </Button>
                            </div>
                        </div>
                    ))}
                </div>
            </Card.Content>
        </Card>
    );
}

// ===================== 添加 Server 表单 =====================

function AddServerForm({ onAdd, onCancel, isVisible }) {
    const [name, setName] = useState("");
    const [serverType, setServerType] = useState("url"); // "url" | "stdio"
    const [url, setUrl] = useState("");
    const [command, setCommand] = useState("");
    const [args, setArgs] = useState("");

    if (!isVisible) return null;

    const handleSubmit = () => {
        const config = serverType === "url"
            ? { url }
            : { command, args: args.split(/\s+/).filter(Boolean) };
        onAdd({ name, ...config });
        setName(""); setUrl(""); setCommand(""); setArgs("");
    };

    return (
        <Card>
            <Card.Content>
                <h3 className="text-sm font-medium text-token-foreground mb-3">Add MCP Server</h3>
                <div className="flex flex-col gap-3">
                    <input
                        className="w-full bg-token-background-secondary rounded-lg px-3 py-2 text-sm text-token-foreground border border-token-border outline-none"
                        placeholder="Server name"
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                    />

                    {/* Type selector */}
                    <div className="flex gap-2">
                        <button
                            className={`px-3 py-1 text-xs rounded-md transition-colors ${serverType === "url" ? "bg-token-primary text-white" : "bg-token-background-secondary text-token-foreground"}`}
                            onClick={() => setServerType("url")}
                        >
                            URL (SSE)
                        </button>
                        <button
                            className={`px-3 py-1 text-xs rounded-md transition-colors ${serverType === "stdio" ? "bg-token-primary text-white" : "bg-token-background-secondary text-token-foreground"}`}
                            onClick={() => setServerType("stdio")}
                        >
                            stdio (Command)
                        </button>
                    </div>

                    {serverType === "url" ? (
                        <input
                            className="w-full bg-token-background-secondary rounded-lg px-3 py-2 text-sm text-token-foreground border border-token-border outline-none"
                            placeholder="https://mcp.example.com/mcp"
                            value={url}
                            onChange={(e) => setUrl(e.target.value)}
                        />
                    ) : (
                        <>
                            <input
                                className="w-full bg-token-background-secondary rounded-lg px-3 py-2 text-sm text-token-foreground border border-token-border outline-none"
                                placeholder="Command (e.g., npx)"
                                value={command}
                                onChange={(e) => setCommand(e.target.value)}
                            />
                            <input
                                className="w-full bg-token-background-secondary rounded-lg px-3 py-2 text-sm text-token-foreground border border-token-border outline-none"
                                placeholder="Arguments (space-separated)"
                                value={args}
                                onChange={(e) => setArgs(e.target.value)}
                            />
                        </>
                    )}

                    <div className="flex gap-2">
                        <Button color="primary" size="sm" onClick={handleSubmit} disabled={!name}>
                            Add Server
                        </Button>
                        <Button color="ghost" size="sm" onClick={onCancel}>
                            Cancel
                        </Button>
                    </div>
                </div>
            </Card.Content>
        </Card>
    );
}

// ===================== McpSettings 主组件 =====================

/**
 * McpSettings — MCP Server 管理页
 *
 * 数据:
 *   - useQuery("mcp-servers") → 已配置的 MCP 服务器列表
 *   - useMutation 添加/删除服务器
 */
function McpSettings() {
    const { data: serversData } = useQuery("mcp-servers", {
        placeholderData: { servers: [] },
    });
    const servers = serversData?.servers ?? [];
    const configuredIds = new Set(servers.map((s) => s.id ?? s.name));

    const [showAddForm, setShowAddForm] = useState(false);

    const handleInstallRecommended = useCallback((recommended) => {
        // 通过 MCP config/write 添加推荐服务器
        const id = `config/write:mcp:${crypto.randomUUID()}`;
        bridge.sendRequest({
            id,
            method: "config/write",
            params: {
                key: recommended.config.keyPath,
                value: recommended.config.value,
            },
        });
    }, []);

    const handleAddServer = useCallback((serverConfig) => {
        const id = `config/write:mcp:${crypto.randomUUID()}`;
        bridge.sendRequest({
            id,
            method: "config/write",
            params: {
                key: `mcp_servers.${serverConfig.name}`,
                value: serverConfig,
            },
        });
        setShowAddForm(false);
    }, []);

    const handleRemoveServer = useCallback((server) => {
        const id = `config/write:mcp:${crypto.randomUUID()}`;
        bridge.sendRequest({
            id,
            method: "config/write",
            params: {
                key: `mcp_servers.${server.name}`,
                value: null,
            },
        });
    }, []);

    return (
        <SettingsPage
            title={<SettingsSlug slug="mcp-settings" />}
            subtitle="Configure external MCP servers that extend Codex's capabilities with additional tools."
        >
            {/* 推荐 Servers */}
            <RecommendedServers
                servers={RECOMMENDED_SERVERS}
                configuredIds={configuredIds}
                onInstall={handleInstallRecommended}
            />

            {/* 已配置 Servers */}
            <ConfiguredServers
                servers={servers}
                onRemove={handleRemoveServer}
            />

            {/* 添加 Server */}
            {!showAddForm && (
                <Button color="secondary" size="sm" onClick={() => setShowAddForm(true)}>
                    Add Custom Server
                </Button>
            )}

            <AddServerForm
                isVisible={showAddForm}
                onAdd={handleAddServer}
                onCancel={() => setShowAddForm(false)}
            />

            {/* 配置文件路径提示 */}
            <div className="text-xs text-token-description-foreground mt-4 border-t border-token-border pt-4">
                <p>Global config: <code className="font-mono">~/.codex/config.toml</code></p>
                <p>Workspace config: <code className="font-mono">.codex/config.toml</code></p>
            </div>
        </SettingsPage>
    );
}

export { McpSettings };
