// =============================================================================
// Codex.app — useTerminal Hook
// 与 TerminalManager.js 配合使用
//
// 功能: 在 React 组件中管理终端会话
// =============================================================================

import { useState, useEffect, useCallback } from "react";
import { terminalManager } from "../core/TerminalManager";

/**
 * useTerminalSession — 订阅单个终端会话
 *
 * @param {string} terminalId - 终端 ID
 * @returns {{ status, exitCode, error, onData }}
 *
 * @example
 *   const { status, onData } = useTerminalSession("term-1");
 *   useEffect(() => onData((chunk) => xterm.write(chunk)), [onData]);
 */
export function useTerminalSession(terminalId) {
    const [status, setStatus] = useState("initializing");
    const [exitCode, setExitCode] = useState(null);
    const [error, setError] = useState(null);

    useEffect(() => {
        if (!terminalId) return;
        const session = terminalManager.getOrCreateSession(terminalId);

        // 初始化状态
        setStatus(session.status);
        setExitCode(session.exitCode);
        setError(session.error);

        // 订阅状态变化
        return session.onStatusChange(({ status: s, exitCode: ec, error: err }) => {
            setStatus(s);
            setExitCode(ec);
            setError(err);
        });
    }, [terminalId]);

    const onData = useCallback((callback) => {
        if (!terminalId) return () => { };
        const session = terminalManager.getOrCreateSession(terminalId);
        return session.onData(callback);
    }, [terminalId]);

    return { status, exitCode, error, onData };
}

/**
 * useTerminalSessions — 订阅终端会话列表变化
 *
 * @returns {TerminalSession[]}
 */
export function useTerminalSessions() {
    const [sessions, setSessions] = useState(() => terminalManager.getAllSessions());

    useEffect(() => {
        return terminalManager.onSessionListChange(setSessions);
    }, []);

    return sessions;
}

/**
 * useTerminalManager — 获取终端管理器实例
 *
 * @returns {TerminalManager}
 */
export function useTerminalManager() {
    return terminalManager;
}
