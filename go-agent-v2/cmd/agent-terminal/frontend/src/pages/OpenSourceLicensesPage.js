// =============================================================================
// Codex.app — 开源许可页面 (OpenSourceLicensesPage)
// 混淆名: pTn
// 路由: /settings/open-source-licenses
// 提取自: index-formatted.js L325429
//
// 功能: 展示应用使用的开源软件许可证
//   - 列出所有第三方依赖的 license 信息
//   - 通常从打包时生成的 license 文件读取
// =============================================================================

import { LicenseList } from "../components/PageSubComponents";

/**
 * OpenSourceLicensesPage — 开源许可证列表
 */
function OpenSourceLicensesPage() {
    return (
        <div className="h-full overflow-auto p-6">
            <h1 className="text-xl font-semibold mb-4">Open Source Licenses</h1>
            <div className="prose prose-sm text-token-foreground">
                <LicenseList />
            </div>
        </div>
    );
}

export { OpenSourceLicensesPage };
