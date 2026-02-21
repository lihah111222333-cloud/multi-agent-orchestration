// =============================================================================
// Codex.app — OnboardingContainer 组件
// 从 WelcomePage, SelectWorkspacePage 的 <OnboardingContainer> 使用推导
//
// 功能: Onboarding 页面的居中容器布局
// =============================================================================

/**
 * OnboardingContainer — Onboarding 居中容器
 *
 * 提供:
 *   - 全屏居中布局
 *   - 最大宽度约束
 *   - 一致的间距
 *
 * @param {Object} props
 * @param {React.ReactNode} props.children
 */
export function OnboardingContainer({ children }) {
    return (
        <div className="onboarding-container h-full flex items-center justify-center bg-token-background">
            <div className="flex w-full max-w-[480px] flex-col items-center gap-6 px-6">
                {children}
            </div>
        </div>
    );
}

/**
 * CenteredLayout — 通用居中布局
 * 从 WorktreeInitPage 中的 <CenteredLayout> 使用推导
 *
 * @param {Object} props
 * @param {React.ReactNode} props.header - 顶部区域 (如 Toolbar)
 * @param {React.ReactNode} props.children
 */
export function CenteredLayout({ header, children }) {
    return (
        <div className="centered-layout h-full flex flex-col">
            {header}
            <div className="flex-1 flex items-start justify-center overflow-y-auto py-8">
                <div className="w-full max-w-[600px] px-6">
                    {children}
                </div>
            </div>
        </div>
    );
}

export default OnboardingContainer;
