// =============================================================================
// Codex.app — Skills 设置 (SkillsSettings)
// Chunk: skills-settings-CE76M8Bp.js (13 行格式化)
// 路由: /settings → skills-settings (lazy loaded)
//
// 功能:
//   - 极简 wrapper: 直接渲染主 bundle 中的 SkillsSettingsContent (i)
//   - 实际 UI 和逻辑在主 bundle 中实现
//   - chunk 仅负责 lazy-loading 入口
// =============================================================================

import { useQuery } from "../../hooks/useAppQuery";
import { Button } from "../../components/Button";

/**
 * SkillsSettingsContent — Skills 管理内容
 *
 * Skills 管理功能:
 *   - 查看已安装的 Skills 列表
 *   - 启用/禁用 Skills
 *   - Skill 详情和配置
 */
function SkillsSettingsContent() {
    const { data: skillsData, isLoading } = useQuery("skills/list", {
        placeholderData: { data: [] },
    });
    const skills = skillsData?.data ?? [];

    return (
        <div className="skills-settings flex flex-col gap-6">
            <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-token-foreground">Skills</h2>
            </div>

            <p className="text-sm text-token-description-foreground">
                Skills extend Codex with additional capabilities and custom workflows.
            </p>

            {isLoading ? (
                <div className="text-sm text-token-description-foreground">Loading skills...</div>
            ) : skills.length === 0 ? (
                <div className="text-center py-8 text-token-description-foreground text-sm border border-token-border rounded-lg">
                    No skills available
                </div>
            ) : (
                <div className="flex flex-col gap-3">
                    {skills.map((skill) => (
                        <div key={skill.id ?? skill.name} className="border border-token-border rounded-lg p-4">
                            <div className="flex items-center justify-between">
                                <div>
                                    <div className="text-sm font-medium text-token-foreground">{skill.name}</div>
                                    <div className="text-xs text-token-description-foreground mt-0.5">{skill.description}</div>
                                </div>
                                <Button color="ghost" size="sm">
                                    {skill.installed ? "Disable" : "Enable"}
                                </Button>
                            </div>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}

/**
 * SkillsSettings — Skills 配置页
 */
function SkillsSettings() {
    return <SkillsSettingsContent />;
}

export { SkillsSettings };
