// =============================================================================
// Codex.app — 首次运行引导页 (FirstRunPage)
// 混淆名: MTn
// 路由: /first-run
// 提取自: index-formatted.js L326050
//
// 功能: 新用户引导 (NUX — New User Experience)
//   - 多步骤引导: 介绍 Codex 核心功能
//   - Step 1: "Codex in your IDE" (介绍 IDE 集成)
//   - Step 2: "Hand off to Codex in the cloud" (云端任务)
//   - Step 3: "Turn TODOs into Codex tasks" (TODO → 任务)
//   - 支持 ChatGPT Auth 和 API Key Auth 两种引导流程
//   - 完成后标记 NUX 已完成 (持久化到 config)
//   - 包含终端动画演示背景
// =============================================================================

import { useNavigate } from "react-router-dom";
import { useAuth } from "../hooks/useAuth";
import { useNuxVariant } from "../hooks/useIntl";
import { useConfigData } from "../hooks/useConfig";
import { FirstRunWizard } from "../components/FirstRunWizard";
import { NuxKeys, STEP_CHATGPT_INTRO, STEP_APIKEY_INTRO } from "../utils";

/**
 * FirstRunPage — 新用户引导
 *
 * 内部结构:
 *   FirstRunPage
 *   └── FirstRunWizard (TTn) — 多步引导组件
 *       ├── TerminalAnimation (背景动画)
 *       ├── StepTitle (标题动画)
 *       ├── StepDescription (描述动画)
 *       ├── StepNavigation (前进/后退按钮)
 *       └── APIKeyInputDialog (API Key 输入弹窗, 仅 API Key 流程)
 *
 * 参数决策:
 *   - authMethod === "chatgpt" → initialStep = M6 (ChatGPT 引导流程)
 *   - authMethod === "copilot" → isUsingCopilotAuth = true
 *   - 否则 → initialStep = b4 (API Key 引导流程)
 *
 * 完成回调 (onAccept):
 *   1. 标记 NUX_2025_09_15 已完成
 *   2. 根据流程类型标记对应 NUX 子标记
 *   3. 导航到 "/"
 */
function FirstRunPage() {
    const nuxVariant = useNuxVariant(); // XVe()
    const { authMethod } = useAuth();
    const isChatGPT = authMethod === "chatgpt";
    const isCopilot = authMethod === "copilot";

    // 根据 NUX 变体和 auth 方式决定初始步骤
    let initialStep;
    switch (nuxVariant) {
        case "2025-09-15-full-chatgpt-auth":
            initialStep = STEP_CHATGPT_INTRO; // M6
            break;
        case "2025-09-15-apikey-auth":
            initialStep = STEP_APIKEY_INTRO; // b4
            break;
        default:
            initialStep = isChatGPT ? STEP_CHATGPT_INTRO : STEP_APIKEY_INTRO;
    }

    const navigate = useNavigate();
    const { setData: setNuxDone } = useConfigData(NuxKeys.NUX_2025_09_15);
    const { setData: setChatGPTViewed } = useConfigData(NuxKeys.NUX_2025_09_15_FULL_CHATGPT_AUTH_VIEWED);
    const { setData: setApiKeyViewed } = useConfigData(NuxKeys.NUX_2025_09_15_APIKEY_AUTH_VIEWED);

    const handleAccept = async () => {
        await setNuxDone(true);
        if (nuxVariant === "2025-09-15-full-chatgpt-auth") {
            await setChatGPTViewed(true);
        } else if (nuxVariant === "2025-09-15-apikey-auth") {
            await setApiKeyViewed(true);
        }
        navigate("/");
    };

    return (
        <FirstRunWizard
            initialStep={initialStep}
            onAccept={handleAccept}
            hasCloudAccess={isChatGPT}
            isUsingCopilotAuth={isCopilot}
        />
    );
}

export { FirstRunPage };
