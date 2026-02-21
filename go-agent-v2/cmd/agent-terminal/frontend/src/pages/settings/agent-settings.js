// =============================================================================
// Codex.app — Agent Settings 设置子页面
// 路由: /settings/agent-settings
// Chunk: agent-settings-*.js (lazy loaded)
//
// 功能: Agent 核心配置
//   - 模型选择 (o3, o4-mini, codex-mini 等)
//   - 审批策略 (suggest / auto-edit / full-auto)
//   - 沙箱策略 (sandbox / no-sandbox)
//   - 自定义指令 (developer instructions)
// =============================================================================

import { useState, useEffect } from "react";
import { useConfigValue, useConfigData } from "../../hooks/useConfig";
import { Button } from "../../components/Button";

/**
 * AgentSettingsPage — Agent 设置
 *
 * 配置项:
 *   1. model — 默认模型选择
 *   2. approval_policy — 审批策略
 *      - "suggest": 所有操作需要审批 (默认)
 *      - "auto-edit": 文件编辑自动批准, 命令执行需审批
 *      - "full-auto": 所有操作自动批准
 *   3. sandbox_policy — 沙箱策略
 *      - "sandbox": 在沙箱中执行命令 (macOS seatbelt)
 *      - "no-sandbox": 直接执行
 *   4. developer_instructions — 自定义指令文本
 */
export default function AgentSettingsPage() {
    const { data: currentModel, setData: setModel } = useConfigData("model");
    const { data: approvalPolicy, setData: setApprovalPolicy } = useConfigData("approval_policy");
    const { data: sandboxPolicy, setData: setSandboxPolicy } = useConfigData("sandbox_policy");
    const { data: instructions, setData: setInstructions } = useConfigData("developer_instructions");

    const [localInstructions, setLocalInstructions] = useState(instructions ?? "");

    useEffect(() => {
        if (instructions != null) setLocalInstructions(instructions);
    }, [instructions]);

    // 可用模型列表
    const models = [
        { id: "codex5.3", name: "codex5.3", description: "Default model" },
    ];

    const approvalOptions = [
        { value: "suggest", label: "Suggest", description: "All actions require approval" },
        { value: "auto-edit", label: "Auto-edit", description: "File edits auto-approved, commands need approval" },
        { value: "full-auto", label: "Full auto", description: "All actions auto-approved" },
    ];

    const sandboxOptions = [
        { value: "sandbox", label: "Sandbox", description: "Commands run in macOS seatbelt sandbox" },
        { value: "no-sandbox", label: "No sandbox", description: "Commands run directly (⚠️ less safe)" },
    ];

    return (
        <div className="agent-settings flex flex-col gap-8">
            <h2 className="text-lg font-semibold text-token-foreground">Agent Settings</h2>

            {/* 模型选择 */}
            <SettingsSection title="Default Model" description="The model used for new conversations">
                <div className="flex flex-col gap-2">
                    {models.map((m) => (
                        <RadioOption
                            key={m.id}
                            selected={currentModel === m.id}
                            label={m.name}
                            description={m.description}
                            onSelect={() => setModel(m.id)}
                        />
                    ))}
                </div>
            </SettingsSection>

            {/* 审批策略 */}
            <SettingsSection title="Approval Policy" description="How Codex handles tool use">
                <div className="flex flex-col gap-2">
                    {approvalOptions.map((opt) => (
                        <RadioOption
                            key={opt.value}
                            selected={(approvalPolicy ?? "suggest") === opt.value}
                            label={opt.label}
                            description={opt.description}
                            onSelect={() => setApprovalPolicy(opt.value)}
                        />
                    ))}
                </div>
            </SettingsSection>

            {/* 沙箱策略 */}
            <SettingsSection title="Sandbox Policy" description="Command execution environment">
                <div className="flex flex-col gap-2">
                    {sandboxOptions.map((opt) => (
                        <RadioOption
                            key={opt.value}
                            selected={(sandboxPolicy ?? "sandbox") === opt.value}
                            label={opt.label}
                            description={opt.description}
                            onSelect={() => setSandboxPolicy(opt.value)}
                        />
                    ))}
                </div>
            </SettingsSection>

            {/* 自定义指令 */}
            <SettingsSection title="Custom Instructions" description="Additional instructions for the AI agent">
                <textarea
                    className="w-full h-32 bg-token-bg-secondary rounded-lg p-3 text-sm text-token-foreground border border-token-border resize-y outline-none"
                    value={localInstructions}
                    onChange={(e) => setLocalInstructions(e.target.value)}
                    placeholder="e.g., Always write tests for new code..."
                />
                <Button
                    color="primary"
                    size="sm"
                    className="mt-2"
                    onClick={() => setInstructions(localInstructions)}
                >
                    Save
                </Button>
            </SettingsSection>
        </div>
    );
}

// ===================== 辅助组件 =====================

function SettingsSection({ title, description, children }) {
    return (
        <div className="settings-section">
            <h3 className="text-sm font-medium text-token-foreground">{title}</h3>
            {description && <p className="text-xs text-token-description-foreground mt-0.5 mb-3">{description}</p>}
            {children}
        </div>
    );
}

function RadioOption({ selected, label, description, onSelect }) {
    return (
        <button
            className={`flex items-start gap-3 p-3 rounded-lg border text-left transition-colors
                ${selected ? "border-token-primary bg-token-primary/5" : "border-token-border hover:bg-token-foreground/5"}`}
            onClick={onSelect}
        >
            <span className={`mt-0.5 w-4 h-4 rounded-full border-2 flex items-center justify-center flex-shrink-0
                ${selected ? "border-token-primary" : "border-token-border"}`}>
                {selected && <span className="w-2 h-2 rounded-full bg-token-primary" />}
            </span>
            <div>
                <span className="text-sm font-medium text-token-foreground">{label}</span>
                {description && <p className="text-xs text-token-description-foreground mt-0.5">{description}</p>}
            </div>
        </button>
    );
}
