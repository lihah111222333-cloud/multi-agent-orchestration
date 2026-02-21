// =============================================================================
// Codex.app — Checkbox 复选框组件
// 从 SelectWorkspacePage 的 <Checkbox> 使用推导
//
// Props: checked (true/false/"indeterminate"), onChange, label
// =============================================================================

/**
 * Checkbox — 复选框
 *
 * @param {Object} props
 * @param {boolean|"indeterminate"} props.checked
 * @param {Function} props.onChange - (checked: boolean) => void
 * @param {string} props.label
 */
export function Checkbox({ checked, onChange, label }) {
    const isIndeterminate = checked === "indeterminate";

    return (
        <label className="checkbox-wrapper flex items-center gap-2 cursor-pointer select-none">
            <span
                className={`checkbox-box w-4 h-4 rounded border flex items-center justify-center transition-colors
                    ${checked === true ? "bg-token-primary border-token-primary" : "border-token-border"}
                    ${isIndeterminate ? "bg-token-primary border-token-primary" : ""}`}
                onClick={() => onChange(!checked)}
            >
                {checked === true && (
                    <svg className="w-3 h-3 text-white" viewBox="0 0 12 12" fill="none">
                        <path d="M2.5 6L5 8.5L9.5 4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                )}
                {isIndeterminate && (
                    <svg className="w-3 h-3 text-white" viewBox="0 0 12 12" fill="none">
                        <path d="M3 6H9" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                    </svg>
                )}
            </span>
            {label && <span className="text-sm text-token-foreground">{label}</span>}
        </label>
    );
}

export default Checkbox;
