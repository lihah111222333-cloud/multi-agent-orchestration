// =============================================================================
// Codex.app — AnimatedEmoji 动画表情组件
// 从 WelcomePage 中的 <AnimatedEmoji animation="hello" size={52}> 推导
//
// 原始实现使用 Lottie 动画
// 此实现使用 CSS @keyframes 模拟各动画效果
// =============================================================================

/**
 * AnimatedEmoji — 动画表情
 *
 * @param {Object} props
 * @param {string} props.animation - "hello"|"thinking"|"celebrate"|"wave"
 * @param {number} props.size - 尺寸 (px)
 */
export function AnimatedEmoji({ animation = "hello", size = 48 }) {
    const config = ANIMATION_CONFIG[animation] ?? ANIMATION_CONFIG.hello;

    return (
        <>
            <span
                className={`animated-emoji inline-block ${config.className}`}
                style={{ fontSize: `${size}px`, lineHeight: 1 }}
                role="img"
                aria-label={animation}
            >
                {config.emoji}
            </span>
            {/* 内联动画定义 — 仅在首次渲染时注入 */}
            <style>{ANIMATION_CSS}</style>
        </>
    );
}

const ANIMATION_CONFIG = {
    hello: { emoji: "👋", className: "anim-wave" },
    wave: { emoji: "👋", className: "anim-wave" },
    thinking: { emoji: "🤔", className: "anim-thinking" },
    celebrate: { emoji: "🎉", className: "anim-celebrate" },
};

const ANIMATION_CSS = `
    @keyframes emoji-wave {
        0%, 100% { transform: rotate(0deg); }
        10% { transform: rotate(14deg); }
        20% { transform: rotate(-8deg); }
        30% { transform: rotate(14deg); }
        40% { transform: rotate(-4deg); }
        50% { transform: rotate(10deg); }
        60%, 100% { transform: rotate(0deg); }
    }
    @keyframes emoji-thinking {
        0%, 100% { transform: translateY(0); }
        25% { transform: translateY(-4px); }
        50% { transform: translateY(0); }
        75% { transform: translateY(-2px); }
    }
    @keyframes emoji-celebrate {
        0% { transform: scale(0.5) rotate(-15deg); opacity: 0; }
        50% { transform: scale(1.2) rotate(5deg); opacity: 1; }
        70% { transform: scale(0.95) rotate(-3deg); }
        100% { transform: scale(1) rotate(0deg); opacity: 1; }
    }
    .anim-wave {
        animation: emoji-wave 2s ease-in-out;
        transform-origin: 70% 70%;
        display: inline-block;
    }
    .anim-thinking {
        animation: emoji-thinking 2s ease-in-out infinite;
        display: inline-block;
    }
    .anim-celebrate {
        animation: emoji-celebrate 0.6s ease-out;
        display: inline-block;
    }
`;

export default AnimatedEmoji;
