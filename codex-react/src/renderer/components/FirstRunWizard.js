// =============================================================================
// Codex.app — FirstRunWizard 首次运行引导向导
// 混淆名: TTn
// 提取自: index-formatted.js L326050 附近
//
// 功能: 多步骤新用户引导
//   - 终端动画背景
//   - 步骤导航
//   - ChatGPT / API Key 分支流程
// =============================================================================

import { useState } from "react";
import { Button } from "./Button";

/**
 * FirstRunWizard — 新用户引导向导
 * 混淆名: TTn
 *
 * @param {Object} props
 * @param {string} props.initialStep
 * @param {Function} props.onAccept
 * @param {boolean} props.hasCloudAccess
 * @param {boolean} props.isUsingCopilotAuth
 */
export function FirstRunWizard({ initialStep, onAccept, hasCloudAccess, isUsingCopilotAuth }) {
    const [currentStep, setCurrentStep] = useState(0);

    const steps = [
        {
            title: "Codex in your IDE",
            description: "Use Codex directly in your development environment. It integrates with your editor to help you write, review, and refactor code.",
        },
        {
            title: "Hand off to Codex in the cloud",
            description: hasCloudAccess
                ? "Send tasks to Codex in the cloud. It'll work on them in the background while you continue coding."
                : "With an API key, you can use Codex locally. Cloud features are available with a ChatGPT subscription.",
        },
        {
            title: "Turn TODOs into Codex tasks",
            description: "Highlight TODOs in your code, and Codex will break them down into actionable tasks and implement them for you.",
        },
    ];

    const isLastStep = currentStep === steps.length - 1;

    return (
        <div className="first-run-wizard h-full flex flex-col items-center justify-center relative">
            {/* 终端动画背景 */}
            <TerminalAnimation />

            {/* 步骤内容 */}
            <div className="relative z-10 flex flex-col items-center gap-6 max-w-[420px] px-6 text-center">
                <h2 className="text-[24px] font-semibold text-token-foreground animate-fadeIn">
                    {steps[currentStep].title}
                </h2>
                <p className="text-[15px] leading-6 text-token-description-foreground">
                    {steps[currentStep].description}
                </p>

                {/* 步骤指示器 */}
                <div className="flex gap-2">
                    {steps.map((_, i) => (
                        <span
                            key={i}
                            className={`w-2 h-2 rounded-full transition-colors ${i === currentStep ? "bg-token-primary" : "bg-token-border"}`}
                        />
                    ))}
                </div>

                {/* 导航按钮 */}
                <div className="flex gap-3">
                    {currentStep > 0 && (
                        <Button color="ghost" onClick={() => setCurrentStep(currentStep - 1)}>
                            Back
                        </Button>
                    )}
                    {isLastStep ? (
                        <Button color="primary" onClick={onAccept}>
                            Get started
                        </Button>
                    ) : (
                        <Button color="primary" onClick={() => setCurrentStep(currentStep + 1)}>
                            Next
                        </Button>
                    )}
                </div>
            </div>
        </div>
    );
}

/**
 * TerminalAnimation — 背景终端动画
 * 模拟代码行滚动效果
 */
function TerminalAnimation() {
    const lines = [
        "$ codex init",
        "Initializing workspace...",
        "✓ Found 3 git repositories",
        "✓ Indexed 1,245 files",
        "$ codex \"fix the login bug\"",
        "Analyzing codebase...",
        "Found issue in auth/handler.go:142",
        "Applying fix...",
        "✓ Changes applied successfully",
    ];

    return (
        <div className="absolute inset-0 overflow-hidden opacity-5">
            <div className="font-mono text-sm text-token-foreground p-4 animate-scroll">
                {lines.map((line, i) => (
                    <div key={i} className="py-0.5">{line}</div>
                ))}
            </div>
        </div>
    );
}

export default FirstRunWizard;
