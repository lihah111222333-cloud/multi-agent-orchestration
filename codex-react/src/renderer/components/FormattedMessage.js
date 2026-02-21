// =============================================================================
// Codex.app — FormattedMessage i18n 组件
// 从 WelcomePage, WorktreeInitPage, SelectWorkspacePage 的使用推导
//
// 功能: 国际化文本渲染
//   - 支持 {key} 变量插值 (字符串和 JSX 值)
//   - 原始实现使用 react-intl 的 FormattedMessage
// =============================================================================

import React from "react";

/**
 * FormattedMessage — 国际化文本组件
 *
 * @param {Object} props
 * @param {string} props.id - 消息 ID (i18n key)
 * @param {string} props.defaultMessage - 默认文本 (当 locale 缺失时使用)
 * @param {Object} props.values - 插值变量 (支持字符串和 React 元素)
 *
 * 用例:
 *   <FormattedMessage
 *       id="electron.onboarding.welcome.title"
 *       defaultMessage="Welcome!"
 *   />
 *
 *   <FormattedMessage
 *       id="greeting"
 *       defaultMessage="Hello, {name}!"
 *       values={{ name: "User" }}
 *   />
 *
 *   <FormattedMessage
 *       id="docs"
 *       defaultMessage="Read the {link}"
 *       values={{ link: <a href="/docs">docs</a> }}
 *   />
 */
export function FormattedMessage({ id, defaultMessage, values }) {
    const template = defaultMessage || id || "";

    // 无变量时直接返回
    if (!values || Object.keys(values).length === 0) {
        return <>{template}</>;
    }

    // 检查是否有 JSX 值 (React 元素)
    const hasJsxValues = Object.values(values).some(
        (v) => v !== null && typeof v === "object" && React.isValidElement(v)
    );

    if (!hasJsxValues) {
        // 纯字符串插值 — 简单替换
        let text = template;
        for (const [key, value] of Object.entries(values)) {
            text = text.replace(new RegExp(`\\{${key}\\}`, "g"), String(value));
        }
        return <>{text}</>;
    }

    // JSX 插值 — 拆分模板为片段数组
    const keys = Object.keys(values);
    const pattern = new RegExp(`(\\{(?:${keys.join("|")})\\})`, "g");
    const parts = template.split(pattern);

    return (
        <>
            {parts.map((part, i) => {
                const match = part.match(/^\{(\w+)\}$/);
                if (match && values[match[1]] !== undefined) {
                    const val = values[match[1]];
                    return React.isValidElement(val)
                        ? React.cloneElement(val, { key: i })
                        : <React.Fragment key={i}>{String(val)}</React.Fragment>;
                }
                return <React.Fragment key={i}>{part}</React.Fragment>;
            })}
        </>
    );
}

export default FormattedMessage;
