// =============================================================================
// Codex.app — 欢迎页面 (WelcomePage)
// 混淆名: yMn
// 路由: /welcome
// 提取自: index-formatted.js L321957
//
// 功能: 登录后的欢迎引导页
//   - 如果已有 workspaces → 直接跳转到 /select-workspace
//   - 如果是 Free/Go 计划 → 显示免费使用提示
//   - 如果有双倍限额 → 显示双倍限额庆祝提示
//   - 其他计划 → 直接跳转到 /select-workspace
// =============================================================================

import { useNavigate } from "react-router-dom";
import { useQuery } from "../hooks/useAppQuery";
import { useAuth } from "../hooks/useAuth";
import { useAnalytics } from "../hooks/useAnalytics";
import { useSetAtom, postLoginWelcomeAtom } from "../hooks/useIntl";
import { FormattedMessage } from "../components/FormattedMessage";
import { AnimatedEmoji } from "../components/AnimatedEmoji";
import { OnboardingContainer } from "../components/OnboardingContainer";
import { Button } from "../components/Button";
import { DOUBLE_LIMITS_PLANS } from "../utils";

/**
 * WelcomePage — 登录后欢迎页
 *
 * 显示条件:
 *   1. 用户刚登录完成 (postLoginWelcomePending = true)
 *   2. 且没有现有 workspace (否则跳过)
 *   3. 且用户计划满足显示条件
 *
 * 内容:
 *   - 动画表情 (hb animation="hello")
 *   - "Welcome!" 标题
 *   - 计划相关描述 (Free/Go 免费提示 或 双倍限额提示)
 *   - "Continue" 按钮 → /select-workspace
 */
function WelcomePage() {
    const { data: workspaceData } = useQuery("workspace-root-options", {
        placeholderData: { roots: [], labels: {} },
    });
    const trackEvent = useAnalytics();
    const navigate = useNavigate();
    const setPostLoginWelcome = useSetAtom(postLoginWelcomeAtom);
    const { planAtLogin } = useAuth();
    const workspaceCount = workspaceData?.roots.length ?? 0;

    const handleContinue = () => {
        trackEvent({
            eventName: "codex_onboarding_welcome_continue_clicked",
            metadata: { workspaces_count: workspaceCount },
        });
        setPostLoginWelcome(false);
        navigate("/select-workspace", { replace: true });
    };

    // 如果已有 workspaces, 跳过欢迎页
    if (workspaceCount > 0) {
        setPostLoginWelcome(false);
        navigate("/select-workspace", { replace: true });
        return null;
    }

    // 根据计划类型选择描述文案
    let description;
    if (planAtLogin === "free" || planAtLogin === "go") {
        description = (
            <FormattedMessage
                id="electron.onboarding.welcome.freego.description"
                defaultMessage="Codex is included in your plan for free through March 2nd – let's start building together."
            />
        );
    } else if (planAtLogin && DOUBLE_LIMITS_PLANS.includes(planAtLogin)) {
        description = (
            <FormattedMessage
                id="electron.onboarding.welcome.doubleLimits.description"
                defaultMessage="In celebration of the Codex app launch, you have 2× rate limits through April 2."
            />
        );
    } else {
        // 没有特殊提示, 跳过欢迎页
        setPostLoginWelcome(false);
        navigate("/select-workspace", { replace: true });
        return null;
    }

    return (
        <OnboardingContainer>
            <div className="flex w-full max-w-[360px] flex-col items-center gap-6">
                {/* 动画表情 */}
                <AnimatedEmoji animation="hello" size={52} />

                {/* 标题和描述 */}
                <div className="flex w-full flex-col items-center gap-3 px-6 pt-2 text-center">
                    <span className="text-[24px] font-semibold text-token-foreground">
                        <FormattedMessage
                            id="electron.onboarding.welcome.new.title.anon"
                            defaultMessage="Welcome!"
                        />
                    </span>
                    <span className="max-w-[290px] text-[15px] leading-6 text-token-description-foreground">
                        {description}
                    </span>
                </div>

                {/* 继续按钮 */}
                <Button
                    className="w-[168px] justify-center px-[16px] py-[8px] text-[13px] font-medium leading-6"
                    color="primary"
                    size="default"
                    onClick={handleContinue}
                >
                    <FormattedMessage
                        id="electron.onboarding.welcome.continue"
                        defaultMessage="Continue"
                    />
                </Button>
            </div>
        </OnboardingContainer>
    );
}

export { WelcomePage };
