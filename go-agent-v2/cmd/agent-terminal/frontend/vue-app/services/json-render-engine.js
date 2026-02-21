/**
 * json-render spec 解析引擎。
 * 从 markdown 文本中提取 ```json-render 代码块并解析为结构化 spec。
 */

const SPEC_RE = /```json-render\s*\n([\s\S]*?)```/g;

/**
 * 检查文本是否包含 json-render spec 代码块。
 * @param {string} text
 * @returns {boolean}
 */
export function hasJsonRenderSpec(text) {
    if (!text || typeof text !== 'string') return false;
    SPEC_RE.lastIndex = 0;
    return SPEC_RE.test(text);
}

/**
 * 将文本拆分为 text/spec 交替段落。
 * @param {string} text
 * @returns {Array<{ type: 'text'|'spec', content?: string, spec?: object }>}
 */
export function extractSpecBlocks(text) {
    if (!text || typeof text !== 'string') return [{ type: 'text', content: text || '' }];

    const blocks = [];
    let lastIndex = 0;
    let match;
    SPEC_RE.lastIndex = 0;

    while ((match = SPEC_RE.exec(text)) !== null) {
        if (match.index > lastIndex) {
            const before = text.slice(lastIndex, match.index).trim();
            if (before) blocks.push({ type: 'text', content: before });
        }
        try {
            const spec = JSON.parse(match[1]);
            blocks.push({ type: 'spec', spec });
        } catch {
            blocks.push({ type: 'text', content: match[0] });
        }
        lastIndex = match.index + match[0].length;
    }

    if (lastIndex < text.length) {
        const tail = text.slice(lastIndex).trim();
        if (tail) blocks.push({ type: 'text', content: tail });
    }

    return blocks.length > 0 ? blocks : [{ type: 'text', content: text }];
}
