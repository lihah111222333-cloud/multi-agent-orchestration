// =============================================================================
// Codex.app — LoadingSpinner 加载指示器
// 从 SettingsPage, RemoteTaskPage 的 <LoadingSpinner> 引用推导
// =============================================================================

/**
 * LoadingSpinner — 旋转加载指示器
 *
 * @param {Object} props
 * @param {string} props.size - "sm"|"md"|"lg"
 * @param {string} props.className
 */
export function LoadingSpinner({ size = "md", className = "" }) {
    const sizeClasses = {
        sm: "w-4 h-4",
        md: "w-6 h-6",
        lg: "w-8 h-8",
    };

    return (
        <div className={`loading-spinner flex items-center justify-center ${className}`}>
            <svg
                className={`animate-spin ${sizeClasses[size] || sizeClasses.md} text-token-primary`}
                viewBox="0 0 24 24"
                fill="none"
            >
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.37 0 0 5.37 0 12h4z" />
            </svg>
        </div>
    );
}

export default LoadingSpinner;
