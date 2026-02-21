// =============================================================================
// Codex.app — Data Controls 设置子页面
// 路由: /settings/data-controls
// Chunk: data-controls-*.js (lazy loaded)
//
// 功能: 数据隐私和控制
//   - 使用数据共享开关
//   - 对话日志存储位置
//   - 清除对话历史
//   - 导出数据
// =============================================================================

import { useState, useCallback } from "react";
import { useConfigData } from "../../hooks/useConfig";
import { useMutation } from "../../hooks/useAppQuery";
import { Button } from "../../components/Button";
import { bridge } from "../../bridge";

export default function DataControlsPage() {
    const { data: dataSharing, setData: setDataSharing } = useConfigData("privacy.data_sharing");
    const { data: storePath, setData: setStorePath } = useConfigData("privacy.store_path");
    const [isClearing, setIsClearing] = useState(false);

    const handleClearHistory = useCallback(async () => {
        setIsClearing(true);
        try {
            await bridge.callAPI("thread/archive", { all: true });
        } catch (err) {
            console.error("[DataControls] Failed to clear history:", err);
        } finally {
            setIsClearing(false);
        }
    }, []);

    const handleChangeStorePath = useCallback(async () => {
        try {
            const result = await bridge.callAPI("ui/selectProjectDir", {});
            const path = result?.path || "";
            if (path) {
                setStorePath(path);
            }
        } catch (err) {
            console.error("[DataControls] Failed to open directory picker:", err);
        }
    }, [setStorePath]);

    return (
        <div className="data-controls flex flex-col gap-8">
            <h2 className="text-lg font-semibold text-token-foreground">Data Controls</h2>

            {/* 数据共享 */}
            <div className="flex items-center justify-between">
                <div>
                    <h3 className="text-sm font-medium text-token-foreground">Usage Data Sharing</h3>
                    <p className="text-xs text-token-description-foreground mt-0.5">
                        Help improve Codex by sharing anonymous usage data
                    </p>
                </div>
                <button
                    className={`relative w-10 h-5 rounded-full transition-colors ${dataSharing ? "bg-token-primary" : "bg-token-border"}`}
                    onClick={() => setDataSharing(!dataSharing)}
                >
                    <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform ${dataSharing ? "translate-x-5" : "translate-x-0.5"}`} />
                </button>
            </div>

            {/* 存储位置 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground">Conversation Storage</h3>
                <p className="text-xs text-token-description-foreground mt-0.5 mb-3">
                    Where conversation logs are stored locally
                </p>
                <div className="flex items-center gap-2">
                    <input
                        className="flex-1 bg-token-bg-secondary border border-token-border rounded-md px-3 py-2 text-sm text-token-foreground font-mono"
                        value={storePath ?? "~/.codex/conversations/"}
                        readOnly
                    />
                    <Button color="ghost" size="sm" onClick={handleChangeStorePath}>
                        Change
                    </Button>
                </div>
            </div>

            {/* 清除数据 */}
            <div className="settings-section border-t border-token-border pt-6">
                <h3 className="text-sm font-medium text-token-foreground text-red-500">Danger Zone</h3>

                <div className="flex items-center justify-between mt-3 p-3 border border-red-200 rounded-lg">
                    <div>
                        <div className="text-sm text-token-foreground">Clear conversation history</div>
                        <div className="text-xs text-token-description-foreground">
                            This action cannot be undone
                        </div>
                    </div>
                    <Button
                        color="danger"
                        size="sm"
                        loading={isClearing}
                        onClick={handleClearHistory}
                    >
                        Clear All
                    </Button>
                </div>
            </div>
        </div>
    );
}
