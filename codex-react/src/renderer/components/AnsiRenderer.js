// =============================================================================
// Codex.app — AnsiRenderer 终端输出渲染
// 从 WorktreeInitPage 中的 <AnsiRenderer> 引用推导
//
// 功能: 将 ANSI 转义序列转换为彩色 HTML
// =============================================================================

/**
 * AnsiRenderer — ANSI → HTML 渲染器
 *
 * 支持的 ANSI codes:
 *   - 颜色 (30-37, 90-97: 前景色; 40-47, 100-107: 背景色)
 *   - 加粗 (1)
 *   - 重置 (0)
 *
 * @param {Object} props
 * @param {React.ReactNode} props.children - 含 ANSI 序列的文本
 * @param {string} props.className
 */
export function AnsiRenderer({ children, className = "" }) {
    const text = typeof children === "string" ? children : "";

    if (!text) return null;

    const html = ansiToHtml(text);

    return (
        <span
            className={`ansi-renderer font-mono ${className}`}
            dangerouslySetInnerHTML={{ __html: html }}
        />
    );
}

// ANSI 颜色映射
const ANSI_COLORS = {
    30: "#1e1e1e", 31: "#e74c3c", 32: "#2ecc71", 33: "#f1c40f",
    34: "#3498db", 35: "#9b59b6", 36: "#1abc9c", 37: "#ecf0f1",
    90: "#7f8c8d", 91: "#e74c3c", 92: "#2ecc71", 93: "#f1c40f",
    94: "#3498db", 95: "#9b59b6", 96: "#1abc9c", 97: "#ffffff",
};

function ansiToHtml(text) {
    let result = "";
    let i = 0;
    let openSpan = false;

    while (i < text.length) {
        // ESC [ ... m
        if (text[i] === "\x1b" && text[i + 1] === "[") {
            const end = text.indexOf("m", i + 2);
            if (end === -1) { i++; continue; }

            const codes = text.slice(i + 2, end).split(";").map(Number);
            i = end + 1;

            if (openSpan) {
                result += "</span>";
                openSpan = false;
            }

            const styles = [];
            for (const code of codes) {
                if (code === 0) continue; // reset
                if (code === 1) styles.push("font-weight:bold");
                if (ANSI_COLORS[code]) styles.push(`color:${ANSI_COLORS[code]}`);
                if (code >= 40 && code <= 47) styles.push(`background-color:${ANSI_COLORS[code - 10]}`);
            }

            if (styles.length > 0) {
                result += `<span style="${styles.join(";")}">`
                openSpan = true;
            }
        } else {
            // 普通字符
            const ch = text[i];
            if (ch === "<") result += "&lt;";
            else if (ch === ">") result += "&gt;";
            else if (ch === "&") result += "&amp;";
            else if (ch === "\n") result += "<br/>";
            else result += ch;
            i++;
        }
    }

    if (openSpan) result += "</span>";
    return result;
}

export default AnsiRenderer;
