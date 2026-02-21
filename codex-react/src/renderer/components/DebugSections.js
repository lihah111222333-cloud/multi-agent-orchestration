// =============================================================================
// Codex.app — DebugPage 子组件
// 混淆名: MPt, OPt, BPt
// 提取自: index-formatted.js L108295 附近
//
// 功能: DebugPage 中使用的调试子组件
//   - McpDebugSection (MPt): MCP 连接状态
//   - ConfigDebugSection (OPt): 当前配置快照
//   - BuildInfoSection (BPt): 构建信息
//   - DatadogSection: Datadog APM 信息
// =============================================================================

import { CollapsibleSection, DebugRow } from "./CollapsibleSection";
import { bridge } from "../bridge";
import { useQuery } from "../hooks/useAppQuery";

/**
 * McpDebugSection — MCP 调试面板
 * 混淆名: MPt
 *
 * 显示:
 *   - MCP 连接状态 (connected / disconnected)
 *   - 活跃的 MCP 服务器列表
 *   - 最近的 JSON-RPC 消息
 */
export function McpDebugSection() {
    const { data: mcpStatus } = useQuery("mcp/status", {
        placeholderData: null,
        queryConfig: { refetchInterval: 3000 },
    });

    const status = mcpStatus?.connected ? "connected" : "disconnected";
    const servers = mcpStatus?.servers ?? [];

    return (
        <CollapsibleSection storageKey="debug-mcp" title="MCP" variant="global">
            <div className="flex flex-col py-1.5 text-xs">
                <DebugRow label="Status" value={status} />
                <DebugRow label="Servers" value={servers.length > 0 ? servers.map(s => s.name ?? s.id).join(", ") : "none"} />
                <DebugRow label="Transport" value={mcpStatus?.transport ?? "—"} />
            </div>
        </CollapsibleSection>
    );
}

/**
 * ConfigDebugSection — 当前配置调试面板
 * 混淆名: OPt
 */
export function ConfigDebugSection() {
    const { data: configSnapshot } = useQuery("config/read-all", {
        placeholderData: null,
        queryConfig: { refetchInterval: 5000 },
    });

    const config = configSnapshot ?? {};

    return (
        <CollapsibleSection storageKey="debug-config" title="Config" variant="global">
            <div className="flex flex-col py-1.5 text-xs">
                <DebugRow label="Model" value={config.model ?? "—"} />
                <DebugRow label="Approval Policy" value={config.approval_policy ?? "—"} />
                <DebugRow label="Sandbox Policy" value={config.sandbox_policy ?? "—"} />
                <DebugRow label="Workspace Roots" value={Array.isArray(config.workspace_roots) ? config.workspace_roots.join(", ") : "—"} />
            </div>
        </CollapsibleSection>
    );
}

/**
 * BuildInfoSection — 构建信息面板
 * 混淆名: BPt
 */
export function BuildInfoSection() {
    const flavor = bridge.getBuildFlavor();
    const sentryOpts = bridge.getSentryInitOptions();

    return (
        <CollapsibleSection storageKey="debug-build" title="Build" variant="global">
            <div className="flex flex-col py-1.5 text-xs">
                <DebugRow label="Version" value="260206.1448" />
                <DebugRow label="Build" value="565" />
                <DebugRow label="Flavor" value={flavor ?? "dev"} />
                <DebugRow label="Platform" value={typeof navigator !== "undefined" ? navigator.platform : "—"} />
                <DebugRow label="Session ID" value={sentryOpts?.codexAppSessionId ?? "—"} />
                <DebugRow label="Electron" value={bridge.isElectron() ? "yes" : "no"} />
            </div>
        </CollapsibleSection>
    );
}

/**
 * DatadogSection — Datadog APM 面板
 * 仅在 Datadog SDK 可用时显示
 */
export function DatadogSection() {
    // Datadog RUM SDK 不在提取范围内
    // 原始实现: 显示 DD session ID, traces, RUM state
    return null;
}

/**
 * debugBroadcastChannel — 调试广播通道
 * 用于 DebugPage 接收动态调试条目
 */
export const debugBroadcastChannel = (() => {
    try {
        return new BroadcastChannel("codex-debug");
    } catch {
        return null;
    }
})();
