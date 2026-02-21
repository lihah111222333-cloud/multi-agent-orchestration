// =============================================================================
// Codex.app — 选择 Workspace 页面 (SelectWorkspacePage)
// 混淆名: rMn
// 路由: /select-workspace
// 提取自: index-formatted.js L320971
//
// 功能: Onboarding 阶段的 Workspace 选择
//   - 展示可用 workspace 列表 (来自 fs 扫描 + git origins + codex home)
//   - 支持全选/多选/手动添加
//   - 显示 git origin 信息
//   - 完成后进入主应用
// =============================================================================

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../hooks/useAuth";
import { useAnalytics } from "../hooks/useAnalytics";
import { useIntl } from "../hooks/useIntl";
import { useQuery } from "../hooks/useAppQuery";
import { useConfigValue } from "../hooks/useConfig";
import { useRemoteTasks } from "../hooks/useRemoteTasks";
import { FormattedMessage } from "../components/FormattedMessage";
import { OnboardingContainer } from "../components/OnboardingContainer";
import { Button } from "../components/Button";
import { Checkbox } from "../components/Checkbox";
import { WorkspaceItem } from "../components/PageSubComponents";
import { extractLastPathComponent, extractWorkspacePath } from "../utils";
import { bridge } from "../bridge";

// 辅助函数
function deduplicate(arr) { return [...new Set(arr.filter(Boolean))]; }
function mergeWorkspaceSources({ tasks, gitOrigins, codexHome }) {
    return deduplicate([...(tasks ?? []), ...(gitOrigins ?? []), codexHome].filter(Boolean));
}

/**
 * submitSelectedWorkspaces — 提交选中的 workspace 路径
 * 通过 bridge.callAPI("config/write") 写入后端配置
 *
 * @param {string[]} paths - 选中的 workspace 根目录路径
 * @returns {Promise<void>}
 */
async function submitSelectedWorkspaces(paths) {
    await bridge.callAPI("config/value/write", { key: "workspace_roots", value: paths });
}
const emptyOrigins = { origins: [] };
const uniqueSortedPaths = [];

/**
 * SelectWorkspacePage — Workspace 选择页
 *
 * 数据来源:
 *   1. useQuery("workspace-root-options") — 系统扫描到的 workspace 列表
 *   2. useQuery("git-origins") — 每个目录的 git origin URL
 *   3. useQuery("codex-home") — Codex home 目录
 *   4. eN() — 远程任务列表 (提取 workspace paths)
 *
 * 功能:
 *   - Checkbox 选择/取消 workspace
 *   - "Select All" 全选按钮
 *   - 手动添加新 workspace 路径
 *   - "Continue" 提交选择, 完成 onboarding
 *   - "Skip" 跳过选择
 *
 * Analytics:
 *   - codex_onboarding_workspace_selection_changed (toggle_root / select_all / deselect_all)
 *   - codex_onboarding_workspace_continue_clicked
 */
function SelectWorkspacePage() {
    const navigate = useNavigate();
    const intl = useIntl();
    const trackEvent = useAnalytics();

    // 数据查询
    const { data: tasks, isFetching: tasksFetching } = useRemoteTasks(); // eN()
    const taskPaths = (tasks === undefined ? [] : tasks)?.map(extractWorkspacePath);

    const { data: workspaceData, isFetching: workspacesFetching } = useQuery("workspace-root-options", {
        placeholderData: { roots: [], labels: {} },
    });
    const { data: gitOrigins, isFetching: originsFetching } = useQuery("git-origins", {
        params: { dirs: uniqueSortedPaths },
        placeholderData: emptyOrigins,
        queryConfig: { enabled: uniqueSortedPaths.length > 0 },
    });
    const { data: codexHome, isFetching: homeFetching } = useQuery("codex-home");

    const [hostConfig] = useConfigValue("host_config");
    const isRemote = hostConfig?.kind != null ? hostConfig.kind !== "local" : false;

    // 状态
    const [manuallyAdded, setManuallyAdded] = useState([]);
    const [selectedMap, setSelectedMap] = useState({});
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [error, setError] = useState(null);

    // 合并所有来源的 workspace 列表
    const systemRoots = workspaceData?.roots ?? [];
    const mergedRoots = mergeWorkspaceSources({
        tasks: taskPaths,
        gitOrigins: gitOrigins?.origins,
        codexHome: codexHome?.codexHome,
    });
    const allRoots = deduplicate([...systemRoots, ...mergedRoots, ...manuallyAdded]);

    // 为每个 root 创建 label
    const labeledRoots = allRoots.map((root) => {
        const label = workspaceData?.labels?.[root]?.trim();
        return { root, label: label || extractLastPathComponent(root) };
    });

    // 选择逻辑
    const allPaths = labeledRoots.map((r) => r.root);
    const selectedPaths = allPaths.filter((p) => !!selectedMap[p]);
    const selectAllState = allPaths.length > 0 && selectedPaths.length === allPaths.length
        ? true
        : selectedPaths.length > 0
            ? "indeterminate"
            : false;

    // 提交
    const handleContinue = async () => {
        setIsSubmitting(true);
        try {
            await submitSelectedWorkspaces(selectedPaths);
            trackEvent({
                eventName: "codex_onboarding_workspace_continue_clicked",
                metadata: {
                    selected_workspaces_count: selectedPaths.length,
                    total_workspaces_count: allPaths.length,
                },
            });
            navigate("/");
        } catch (err) {
            setError(err.message ?? intl.formatMessage({
                id: "electron.onboarding.workspace.skip.error.unknown",
                defaultMessage: "Unknown error",
            }));
        } finally {
            setIsSubmitting(false);
        }
    };

    return (
        <OnboardingContainer>
            {/* 页面标题 */}
            <div className="text-[24px] font-semibold text-token-foreground">
                <FormattedMessage
                    id="electron.onboarding.workspace.title"
                    defaultMessage="Select your workspaces"
                />
            </div>

            {/* Workspace 列表 */}
            <div className="workspace-list flex flex-col gap-2">
                {/* Select All */}
                <Checkbox
                    checked={selectAllState}
                    onChange={(checked) => {
                        const newMap = {};
                        allPaths.forEach((p) => { newMap[p] = checked; });
                        setSelectedMap(newMap);
                    }}
                    label="Select All"
                />

                {/* 各 workspace 条目 */}
                {labeledRoots.map((item) => (
                    <WorkspaceItem
                        key={item.root}
                        root={item.root}
                        label={item.label}
                        checked={!!selectedMap[item.root]}
                        onChange={(checked) => {
                            setSelectedMap({ ...selectedMap, [item.root]: checked });
                        }}
                    />
                ))}
            </div>

            {/* 错误信息 */}
            {error && <div className="text-red-500 text-sm">{error}</div>}

            {/* 操作按钮 */}
            <div className="flex gap-3">
                <Button color="ghost" onClick={() => navigate("/")}>
                    Skip
                </Button>
                <Button color="primary" onClick={handleContinue} loading={isSubmitting}>
                    Continue
                </Button>
            </div>
        </OnboardingContainer>
    );
}

export { SelectWorkspacePage };
