// =============================================================================
// Codex.app — 数据控制设置 (DataControlsSettings)
// Chunk: data-controls-DD0wZiLa.js (478 行格式化)
// 路由: /settings → data-controls (lazy loaded)
//
// 功能:
//   - 已归档对话管理
//   - 查看已归档对话列表
//   - 取消归档操作 (unarchive)
//   - 点击查看已归档对话内容
// =============================================================================

import { useConversationManager } from "../../hooks/useConversation";
import { useQuery } from "../../hooks/useAppQuery";
import { Button } from "../../components/Button";

// 常量
const StaleTime = { FIVE_SECONDS: 5000 };

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
    const labels = { "data-controls": "Data Controls" };
    return <span>{labels[slug] ?? slug}</span>;
}

function Card({ className, children }) {
    return <div className={`border border-token-border rounded-lg ${className ?? ""}`}>{children}</div>;
}
Card.Content = function CardContent({ children }) { return <div className="p-4">{children}</div>; };

function ArchivedThreadsList({ archivedThreads, isLoading, isError, onUnarchive }) {
    if (isLoading) {
        return <div className="text-sm text-token-description-foreground py-4 text-center">Loading…</div>;
    }
    if (isError) {
        return <div className="text-sm text-token-error-foreground py-4 text-center">Error loading archived threads</div>;
    }
    if (archivedThreads.length === 0) {
        return <div className="text-sm text-token-description-foreground py-8 text-center">No archived chats.</div>;
    }
    return (
        <div className="flex flex-col gap-2">
            {archivedThreads.map((t, i) => (
                <div key={t.id ?? i} className="flex items-center justify-between p-3 border border-token-border rounded-lg">
                    <div>
                        <div className="text-sm font-medium text-token-foreground">{t.title || "Untitled chat"}</div>
                        {t.createdAt && (
                            <div className="text-xs text-token-description-foreground mt-0.5">
                                {new Date(t.createdAt).toLocaleDateString()}
                                {t.repoName && <span className="ml-2">• {t.repoName}</span>}
                            </div>
                        )}
                    </div>
                    {onUnarchive && (
                        <Button color="ghost" size="sm" onClick={() => onUnarchive(t)}>
                            Unarchive
                        </Button>
                    )}
                </div>
            ))}
        </div>
    );
}

// ===================== DataControlsSettings 主组件 =====================

/**
 * DataControlsSettings — 数据控制页
 *
 * 数据:
 *   - useQuery(["archived-threads"]) → conversationManager.listArchivedThreads()
 *   - useMutation: unarchiveConversation (optimistic update)
 */
function DataControlsSettings() {
    const conversationManager = useConversationManager();

    const { data: archivedThreads, isLoading, isError, refetch } = useQuery({
        queryKey: ["archived-threads"],
        queryFn: () => conversationManager.listArchivedThreads(),
        staleTime: StaleTime.FIVE_SECONDS,
    });

    const threads = archivedThreads === undefined ? [] : archivedThreads;

    const handleUnarchive = async (thread) => {
        try {
            await conversationManager.unarchiveThread(thread.id);
            refetch();
        } catch (err) {
            console.error("[DataControlsSettings] Unarchive failed:", err);
        }
    };

    return (
        <SettingsPage title={<SettingsSlug slug="data-controls" />}>
            <Card className="gap-2">
                <Card.Content>
                    <ArchivedThreadsList
                        archivedThreads={threads}
                        isLoading={isLoading}
                        isError={isError}
                        onUnarchive={handleUnarchive}
                    />
                </Card.Content>
            </Card>
        </SettingsPage>
    );
}

export { DataControlsSettings };
