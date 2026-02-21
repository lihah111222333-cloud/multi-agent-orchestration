// =============================================================================
// Codex.app — MCP Settings 设置子页面
// 路由: /settings/mcp-settings
// Chunk: mcp-settings-*.js (lazy loaded)
//
// 功能: MCP (Model Context Protocol) 服务器管理
//   - 已配置的 MCP 服务器列表
//   - 添加/编辑/删除服务器
//   - 启用/禁用单个服务器
//   - 服务器状态 (connected / disconnected / error)
//   - OAuth 登录 (部分服务器需要)
//   - 工具列表查看
// =============================================================================

import { useState } from "react";
import { Button } from "../../components/Button";

/**
 * McpSettingsPage — MCP 服务器管理
 *
 * 配置来源:
 *   1. ~/.codex/config.toml → [mcp_servers] 全局配置
 *   2. .codex/config.toml → 项目级配置
 *
 * 服务器配置格式:
 *   {
 *     name: "server-name",
 *     command: "npx",
 *     args: ["-y", "@mcp/server-xyz"],
 *     env: { API_KEY: "..." },
 *     enabled: true,
 *     scope: "global" | "workspace",
 *     status: "connected" | "disconnected" | "error",
 *     tools: [{ name: "tool_name", description: "..." }],
 *   }
 */
export default function McpSettingsPage() {
    const [servers, setServers] = useState([]);
    const [showAddDialog, setShowAddDialog] = useState(false);

    return (
        <div className="mcp-settings flex flex-col gap-6">
            <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-token-foreground">MCP Servers</h2>
                <Button color="primary" size="sm" onClick={() => setShowAddDialog(true)}>
                    Add Server
                </Button>
            </div>

            <p className="text-sm text-token-description-foreground">
                Configure external MCP servers that extend Codex's capabilities with additional tools.
            </p>

            {/* 服务器列表 */}
            <div className="flex flex-col gap-3">
                {servers.length === 0 ? (
                    <div className="text-center py-8 text-token-description-foreground text-sm">
                        No MCP servers configured. Add a server to get started.
                    </div>
                ) : (
                    servers.map((server) => (
                        <McpServerCard key={server.name} server={server} />
                    ))
                )}
            </div>

            {/* 配置文件路径提示 */}
            <div className="text-xs text-token-description-foreground mt-4 border-t border-token-border pt-4">
                <p>Global config: <code className="font-mono">~/.codex/config.toml</code></p>
                <p>Workspace config: <code className="font-mono">.codex/config.toml</code></p>
            </div>
        </div>
    );
}

function McpServerCard({ server }) {
    return (
        <div className="mcp-server-card border border-token-border rounded-lg p-4">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <StatusDot status={server.status} />
                    <span className="text-sm font-medium text-token-foreground">{server.name}</span>
                    <span className="text-xs text-token-description-foreground px-1.5 py-0.5 bg-token-background-secondary rounded">
                        {server.scope}
                    </span>
                </div>
                <div className="flex items-center gap-2">
                    <Button color="ghost" size="sm">Edit</Button>
                    <Button color="ghost" size="sm">Remove</Button>
                </div>
            </div>
            {server.tools && server.tools.length > 0 && (
                <div className="mt-2 text-xs text-token-description-foreground">
                    {server.tools.length} tools: {server.tools.map(t => t.name).join(", ")}
                </div>
            )}
        </div>
    );
}

function StatusDot({ status }) {
    const colors = {
        connected: "bg-green-500",
        disconnected: "bg-gray-400",
        error: "bg-red-500",
    };
    return <span className={`w-2 h-2 rounded-full ${colors[status] || colors.disconnected}`} />;
}
