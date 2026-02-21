// =============================================================================
// Codex.app — Button 通用按钮组件
// 从多个页面的 <Button> 使用模式推导
//
// Props: color, size, loading, disabled, className, onClick, children
// =============================================================================

/**
 * Button — 通用按钮
 *
 * @param {Object} props
 * @param {"primary"|"ghost"|"danger"|"secondary"} props.color - 按钮样式
 * @param {"default"|"sm"|"lg"} props.size - 按钮大小
 * @param {boolean} props.loading - 加载状态
 * @param {boolean} props.disabled
 * @param {string} props.className - 额外类名
 * @param {Function} props.onClick
 * @param {React.ReactNode} props.children
 */
export function Button({
    color = "primary",
    size = "default",
    loading = false,
    disabled = false,
    className = "",
    onClick,
    children,
}) {
    const colorClasses = {
        primary: "btn-primary bg-token-primary text-white hover:bg-token-primary-hover",
        ghost: "btn-ghost text-token-foreground hover:bg-token-foreground/10",
        danger: "btn-danger bg-red-500 text-white hover:bg-red-600",
        secondary: "btn-secondary border border-token-border text-token-foreground hover:bg-token-foreground/5",
    };

    const sizeClasses = {
        sm: "px-2 py-1 text-xs",
        default: "px-4 py-2 text-sm",
        lg: "px-6 py-3 text-base",
    };

    return (
        <button
            className={`btn inline-flex items-center justify-center gap-2 rounded-md font-medium transition-colors
                ${colorClasses[color] || colorClasses.primary}
                ${sizeClasses[size] || sizeClasses.default}
                ${disabled || loading ? "opacity-50 cursor-not-allowed" : "cursor-pointer"}
                ${className}`}
            onClick={onClick}
            disabled={disabled || loading}
        >
            {loading && <LoadingDots />}
            {children}
        </button>
    );
}

function LoadingDots() {
    return (
        <span className="loading-dots flex gap-0.5">
            <span className="dot animate-bounce w-1 h-1 rounded-full bg-current" />
            <span className="dot animate-bounce w-1 h-1 rounded-full bg-current" style={{ animationDelay: "0.1s" }} />
            <span className="dot animate-bounce w-1 h-1 rounded-full bg-current" style={{ animationDelay: "0.2s" }} />
        </span>
    );
}

export default Button;
