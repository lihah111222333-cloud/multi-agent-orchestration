// =============================================================================
// Codex.app — 调试页面 (DebugPage)
// 混淆名: zPt
// 路由: /debug (无需认证)
// 提取自: index-formatted.js L108295
//
// 功能: 应用调试信息展示
//   - 动态调试条目 (通过 BroadcastChannel 实时更新)
//   - 用户认证信息 (Auth Method, User ID, Account ID, Email)
//   - Sentry 诊断 (App Session ID, 日志导出)
//   - Debug sections 使用可折叠面板
// =============================================================================

import { useState, useEffect, useCallback } from "react";
import { useAuth } from "../hooks/useAuth";
import { CollapsibleSection, DebugRow, DebugLines, LogExportButton } from "../components/CollapsibleSection";
import { McpDebugSection, ConfigDebugSection, BuildInfoSection, DatadogSection, debugBroadcastChannel } from "../components/DebugSections";
import { logger } from "../logger";

/**
 * DebugPage — 诊断信息页面
 *
 * 该页面不需要认证, 直接挂载在 /debug 路由
 *
 * 结构:
 *   DebugPage
 *   ├── Toolbar ("Debug" 标题, 可拖拽)
 *   ├── CollapsibleSection[] (动态调试条目, 通过 BroadcastChannel 接收)
 *   ├── MPt / OPt / BPt (内置调试组件)
 *   ├── User Section
 *   │   ├── Auth Method
 *   │   ├── User ID
 *   │   ├── Account ID
 *   │   └── Email
 *   ├── Diagnostics Section (仅 Electron)
 *   │   ├── App Session ID
 *   │   ├── Log Export (all / codex-cli / app-server)
 *   │   └── Trigger Sentry Test Error
 *   └── DatadogSection (if available)
 */
function DebugPage() {
    const auth = useAuth(); // k1()
    const [entries, setEntries] = useState([]);
    const [isTestingError, setIsTestingError] = useState(false);
    const authMethod = auth.authMethod ?? "none";
    const [isExporting, setIsExporting] = useState(false);

    // 监听 BroadcastChannel 接收动态调试条目
    useEffect(() => {
        const channel = debugBroadcastChannel; // p8
        if (!channel) return;
        const handler = (event) => {
            const data = event.data;
            if (data?.kind === "add") {
                setEntries((prev) => {
                    const filtered = prev.filter((e) => e.id !== data.id);
                    filtered.push(data.entry);
                    return filtered;
                });
            } else if (data?.kind === "remove") {
                setEntries((prev) => prev.filter((e) => e.id !== data.id));
            } else if (data?.kind === "clear") {
                setEntries([]);
            }
        };
        channel.addEventListener("message", handler);
        channel.postMessage({ kind: "request-sync" });
        return () => channel.removeEventListener("message", handler);
    }, [setEntries]);

    const hasSentry = false; // Sentry 不可用 (Wails 环境)
    const canExportLogs = false; // 日志导出走 Go 后端
    const sessionId = undefined;

    const handleTriggerTestError = useCallback(async () => {
        // Sentry 测试错误: Wails 环境不支持
        logger.warn("Sentry test error not available in Wails");
    }, []);

    const handleExportLogs = useCallback(async (scope) => {
        if (isExporting) return;
        setIsExporting(true);
        try {
            // 可通过 bridge.callAPI 导出日志 (待实现)
            logger.info("Log export requested", { safe: { scope } });
        } catch (err) {
            logger.error("Failed to export logs", { safe: { scope }, sensitive: { error: err } });
        } finally {
            setIsExporting(false);
        }
    }, [isExporting]);

    return (
        <div className="fixed inset-0 text-sm">
            {/* 顶部标题栏 (可拖拽) */}
            <div className="h-toolbar-sm draggable text-token-description-foreground fixed left-0 right-0 flex items-center justify-center font-medium">
                Debug
            </div>

            <div className="top-toolbar-sm fixed inset-0 flex flex-col gap-px overflow-scroll pb-4">
                {/* 动态调试条目 */}
                {entries.map((entry) => (
                    <CollapsibleSection
                        key={entry.id}
                        title={entry.titleText || "Debug entry"}
                        storageKey={`debug-entry-${entry.titleText}`}
                        variant="selection"
                    >
                        <DebugLines lines={entry.lines} />
                    </CollapsibleSection>
                ))}

                {/* 内置调试组件 */}
                <McpDebugSection />
                <ConfigDebugSection />
                <BuildInfoSection />

                {/* 用户信息 */}
                <CollapsibleSection storageKey="debug-user-section" title="User" variant="global">
                    <div className="flex flex-col py-1.5">
                        <DebugRow label="Auth Method" value={authMethod} />
                        <DebugRow label="User ID" value={auth.userId ?? "Unavailable"} />
                        <DebugRow label="Account ID" value={auth.accountId ?? "Unavailable"} />
                        <DebugRow label="Email" value={auth.email ?? "Unavailable"} />
                    </div>
                </CollapsibleSection>

                {/* Sentry 诊断 (仅 Electron) */}
                {hasSentry && (
                    <CollapsibleSection storageKey="debug-sentry-section" title="Diagnostics" variant="global">
                        <div className="flex flex-col py-1.5">
                            <DebugRow label="App session ID" value={sessionId ?? "Unavailable"} />
                        </div>
                        <div className="flex flex-col gap-3 py-1.5">
                            {/* 日志导出: all / codex-cli / app-server */}
                            <LogExportButton
                                disabled={!canExportLogs}
                                isExporting={isExporting}
                                onExport={handleExportLogs}
                            />
                            {/* Sentry 测试错误按钮 */}
                            <button
                                onClick={handleTriggerTestError}
                                disabled={isTestingError}
                            >
                                Trigger Sentry Test Error
                            </button>
                        </div>
                    </CollapsibleSection>
                )}
            </div>
        </div>
    );
}

export { DebugPage };
