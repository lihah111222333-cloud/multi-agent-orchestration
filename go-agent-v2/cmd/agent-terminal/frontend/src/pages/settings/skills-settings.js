// =============================================================================
// Codex.app — Skills Settings 设置子页面
// 路由: /settings/skills-settings
// Chunk: skills-settings-*.js (lazy loaded)
//
// 功能: 技能 (Skills) 配置
//   - 已安装技能列表
//   - 技能 scope (global / workspace)
//   - 技能搜索路径
//   - 技能启用/禁用
// =============================================================================

import { useQuery } from "../../hooks/useAppQuery";
import { Button } from "../../components/Button";

export default function SkillsSettingsPage() {
    const { data: skills, isLoading } = useQuery("skills/list", {
        placeholderData: { data: [] },
    });

    const skillsList = skills?.data ?? [];

    return (
        <div className="skills-settings flex flex-col gap-6">
            <h2 className="text-lg font-semibold text-token-foreground">Skills</h2>

            <p className="text-sm text-token-description-foreground">
                Skills extend Codex's capabilities by providing specialized workflows and context.
                Place skill files in <code className="font-mono">.codex/skills/</code> or <code className="font-mono">~/.codex/skills/</code>.
            </p>

            {/* 搜索路径 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground mb-2">Search Paths</h3>
                <div className="flex flex-col gap-1 text-xs text-token-description-foreground font-mono">
                    <div>~/.codex/skills/ (global)</div>
                    <div>.codex/skills/ (workspace)</div>
                </div>
            </div>

            {/* 技能列表 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground mb-2">Installed Skills</h3>
                {isLoading ? (
                    <div className="text-sm text-token-description-foreground">Loading...</div>
                ) : skillsList.length === 0 ? (
                    <div className="text-center py-6 text-token-description-foreground text-sm border border-dashed border-token-border rounded-lg">
                        No skills installed
                    </div>
                ) : (
                    <div className="flex flex-col gap-2">
                        {skillsList.map((skill, i) => (
                            <div key={i} className="flex items-center justify-between p-3 border border-token-border rounded-lg">
                                <div>
                                    <div className="text-sm font-medium text-token-foreground">{skill.name}</div>
                                    <div className="text-xs text-token-description-foreground mt-0.5">{skill.description || skill.path}</div>
                                </div>
                                <span className="text-xs text-token-description-foreground px-1.5 py-0.5 bg-token-bg-secondary rounded">
                                    {skill.scope ?? "workspace"}
                                </span>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}
