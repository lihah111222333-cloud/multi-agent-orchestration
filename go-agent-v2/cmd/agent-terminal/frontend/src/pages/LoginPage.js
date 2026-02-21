// =============================================================================
// Codex.app — 登录页面 (LoginPage)
// 混淆名: nMn
// 路由: /login
// 提取自: index-formatted.js L320952
//
// 功能: 用户登录入口
//   - Electron 模式: 使用 ElectronLoginPage (tMn) — OAuth 流程
//   - Web 模式: 使用 WebLoginPage ($En)
// =============================================================================

import { useWindowType } from "../hooks/useWindowType";
import { ElectronLoginPage, WebLoginPage } from "../components/LoginViews";

/**
 * LoginPage — 登录页面路由分发
 *
 * 根据 windowType 决定渲染哪个登录视图:
 *   - electron → ElectronLoginPage (tMn)
 *     - ChatGPT OAuth 登录 (打开外部浏览器)
 *     - API Key 登录 (本地输入)
 *   - web → WebLoginPage ($En)
 *     - 内联登录表单
 */
function LoginPage() {
    const windowType = useWindowType(); // Sr()

    if (windowType === "electron") {
        return <ElectronLoginPage />;   // tMn
    }

    return <WebLoginPage />;            // $En
}

export { LoginPage };
