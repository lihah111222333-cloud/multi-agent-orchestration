// =============================================================================
// Codex.app — useWindowType Hook
// 混淆名: Sr
// 提取自: index-formatted.js (WindowTypeContext 消费)
//
// 功能: 获取当前窗口类型 (Electron vs Web)
// =============================================================================

import { useContext } from "react";
import { WindowTypeContext } from "../contexts";

/**
 * useWindowType — 获取窗口运行环境
 * 混淆名: Sr()
 *
 * @returns {"electron"|"web"}
 */
export function useWindowType() {
    return useContext(WindowTypeContext);
}
