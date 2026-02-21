// =============================================================================
// Codex.app — Personalization 设置子页面
// 路由: /settings/personalization
// Chunk: personalization-settings-*.js (lazy loaded)
//
// 功能: 外观和个性化设置
//   - 主题 (light / dark / system)
//   - 字体大小
//   - 光标样式
//   - Liquid Glass 效果 (macOS)
// =============================================================================

import { useConfigData } from "../../hooks/useConfig";

export default function PersonalizationPage() {
    const { data: theme, setData: setTheme } = useConfigData("appearance.theme");
    const { data: fontSize, setData: setFontSize } = useConfigData("appearance.font_size");
    const { data: liquidGlass, setData: setLiquidGlass } = useConfigData("appearance.liquid_glass");

    return (
        <div className="personalization-settings flex flex-col gap-8">
            <h2 className="text-lg font-semibold text-token-foreground">Personalization</h2>

            {/* 主题 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground">Theme</h3>
                <p className="text-xs text-token-description-foreground mt-0.5 mb-3">Choose your preferred color scheme</p>
                <div className="flex gap-3">
                    {["light", "dark", "system"].map((t) => (
                        <ThemeCard
                            key={t}
                            theme={t}
                            selected={(theme ?? "system") === t}
                            onSelect={() => setTheme(t)}
                        />
                    ))}
                </div>
            </div>

            {/* 字体大小 */}
            <div className="settings-section">
                <h3 className="text-sm font-medium text-token-foreground">Font Size</h3>
                <p className="text-xs text-token-description-foreground mt-0.5 mb-3">Adjust the text size</p>
                <div className="flex items-center gap-4">
                    <input
                        type="range"
                        min="12"
                        max="20"
                        value={fontSize ?? 14}
                        onChange={(e) => setFontSize(Number(e.target.value))}
                        className="flex-1"
                    />
                    <span className="text-sm text-token-foreground w-12 text-right font-mono">{fontSize ?? 14}px</span>
                </div>
            </div>

            {/* Liquid Glass */}
            <div className="flex items-center justify-between">
                <div>
                    <h3 className="text-sm font-medium text-token-foreground">Liquid Glass</h3>
                    <p className="text-xs text-token-description-foreground mt-0.5">Enable macOS Liquid Glass effect (requires macOS 26+)</p>
                </div>
                <button
                    className={`relative w-10 h-5 rounded-full transition-colors ${liquidGlass ? "bg-token-primary" : "bg-token-border"}`}
                    onClick={() => setLiquidGlass(!liquidGlass)}
                >
                    <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform ${liquidGlass ? "translate-x-5" : "translate-x-0.5"}`} />
                </button>
            </div>
        </div>
    );
}

function ThemeCard({ theme, selected, onSelect }) {
    const labels = { light: "Light", dark: "Dark", system: "System" };
    const previews = {
        light: "bg-white border-gray-200",
        dark: "bg-gray-900 border-gray-700",
        system: "bg-gradient-to-r from-white to-gray-900 border-gray-400",
    };
    return (
        <button
            className={`flex flex-col items-center gap-2 p-3 rounded-lg border transition-colors
                ${selected ? "border-token-primary ring-2 ring-token-primary/20" : "border-token-border hover:bg-token-foreground/5"}`}
            onClick={onSelect}
        >
            <div className={`w-16 h-10 rounded ${previews[theme]}`} />
            <span className="text-xs text-token-foreground">{labels[theme]}</span>
        </button>
    );
}
