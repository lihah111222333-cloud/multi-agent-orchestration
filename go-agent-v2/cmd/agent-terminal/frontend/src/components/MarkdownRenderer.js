// =============================================================================
// Codex.app — MarkdownRenderer Markdown 渲染组件
// 混淆名: QZ
// 提取自: index-formatted.js L~160000 附近
//
// 功能: 将 AI 回复文本渲染为 Markdown
//   - 代码块语法高亮
//   - 链接处理
//   - 图片渲染
//   - 列表 / 表格
//   - 内联代码
// =============================================================================

import { useMemo } from "react";

/**
 * MarkdownRenderer — Markdown 渲染器
 * 混淆名: QZ
 *
 * 原始实现使用自定义 Markdown parser (非 react-markdown)
 * 支持增量渲染 (AI 流式输出时逐步追加)
 *
 * @param {Object} props
 * @param {React.ReactNode} props.children - Markdown 文本
 * @param {string} props.className
 */
export function MarkdownRenderer({ children, className = "" }) {
    const text = typeof children === "string" ? children : "";

    const html = useMemo(() => {
        if (!text) return "";
        return parseMarkdown(text);
    }, [text]);

    if (!html) return null;

    return (
        <div
            className={`markdown-renderer prose prose-sm max-w-none text-token-foreground ${className}`}
            dangerouslySetInnerHTML={{ __html: html }}
        />
    );
}

/**
 * parseMarkdown — Markdown → HTML 解析
 *
 * 处理顺序 (防止转义冲突):
 *   1. 提取代码块 → 占位符
 *   2. 提取内联代码 → 占位符
 *   3. 转义 HTML
 *   4. 解析块级元素 (标题, 列表, 分割线)
 *   5. 解析内联元素 (粗体, 斜体, 链接, 图片)
 *   6. 恢复代码块和内联代码
 *   7. 处理段落和换行
 */
function parseMarkdown(text) {
    // 1. 提取代码块 (在 HTML 转义前, 保留原始内容)
    const codeBlocks = [];
    let processed = text.replace(/```(\w*)\n([\s\S]*?)```/g, (_, lang, code) => {
        const idx = codeBlocks.length;
        codeBlocks.push(
            `<pre class="code-block bg-token-editor-background rounded-lg p-3 my-2 overflow-x-auto"><code class="language-${lang || "text"} text-sm font-mono">${escapeHtml(code)}</code></pre>`
        );
        return `\x00CB${idx}\x00`;
    });

    // 2. 提取内联代码
    const inlineCodes = [];
    processed = processed.replace(/`([^`\n]+)`/g, (_, code) => {
        const idx = inlineCodes.length;
        inlineCodes.push(
            `<code class="inline-code px-1 py-0.5 rounded bg-token-editor-background text-sm font-mono">${escapeHtml(code)}</code>`
        );
        return `\x00IC${idx}\x00`;
    });

    // 3. 转义 HTML (代码块/内联代码已安全提取)
    processed = escapeHtml(processed);

    // 4. 块级元素
    // 标题
    processed = processed.replace(/^### (.+)$/gm, '<h3 class="text-base font-semibold mt-4 mb-1">$1</h3>');
    processed = processed.replace(/^## (.+)$/gm, '<h2 class="text-lg font-semibold mt-4 mb-2">$1</h2>');
    processed = processed.replace(/^# (.+)$/gm, '<h1 class="text-xl font-bold mt-4 mb-2">$1</h1>');

    // 分割线
    processed = processed.replace(/^---$/gm, '<hr class="border-token-border my-4"/>');

    // 无序列表
    processed = processed.replace(/^- (.+)$/gm, '<li class="ml-4">$1</li>');
    processed = processed.replace(/(<li[^>]*>.*<\/li>\n?)+/g, '<ul class="list-disc my-2">$&</ul>');

    // 有序列表
    processed = processed.replace(/^\d+\. (.+)$/gm, '<li class="ml-4">$1</li>');

    // 5. 内联元素
    // 图片
    processed = processed.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img src="$2" alt="$1" class="max-w-full rounded-lg my-2"/>');

    // 链接
    processed = processed.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" class="text-token-primary hover:underline" target="_blank" rel="noopener">$1</a>');

    // 粗体 + 斜体
    processed = processed.replace(/\*\*\*(.+?)\*\*\*/g, '<strong><em>$1</em></strong>');
    processed = processed.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    processed = processed.replace(/\*(.+?)\*/g, '<em>$1</em>');

    // 6. 恢复占位符
    processed = processed.replace(/\x00CB(\d+)\x00/g, (_, idx) => codeBlocks[Number(idx)]);
    processed = processed.replace(/\x00IC(\d+)\x00/g, (_, idx) => inlineCodes[Number(idx)]);

    // 7. 段落和换行
    processed = processed.replace(/\n\n/g, '</p><p class="my-2">');
    processed = processed.replace(/\n/g, '<br/>');
    processed = `<p class="my-2">${processed}</p>`;

    // 清理空段落
    processed = processed.replace(/<p class="my-2"><\/p>/g, '');

    return processed;
}

function escapeHtml(text) {
    return text
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;");
}

export default MarkdownRenderer;
