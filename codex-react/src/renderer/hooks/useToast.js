// =============================================================================
// Codex.app — useToast Hook
// 从 WorktreeInitPage 中的 useToast() 调用推导
//
// 功能: Toast 通知
// =============================================================================

import { useContext } from "react";
import { ToastContext } from "../contexts";

/**
 * useToast — 获取 Toast 通知方法
 * 混淆名: (WorktreeInitPage 中引用)
 *
 * @returns {{ success: (msg: string) => void, danger: (msg: string) => void, info: (msg: string) => void }}
 *
 * 用例:
 *   const toast = useToast();
 *   toast.danger("Error starting conversation");
 */
export function useToast() {
    return useContext(ToastContext);
}
