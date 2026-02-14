"""iTerm Agent I/O 桥接层

提供统一能力：
- 列出已启动的 Agent 会话
- 向一个/多个 Agent 会话发送输入
- 读取一个/多个 Agent 会话最近输出

注意：
- 依赖 iTerm Python API (`iterm2`)
- 会话映射默认读取 `data/iterm_launch_state.json`
"""

from __future__ import annotations

import json
import logging
import os
import re
import subprocess
import sys
import time as _time_mod
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

_perf_log = logging.getLogger("iterm_bridge.perf")

ROOT_DIR = Path(__file__).resolve().parents[1]
DEFAULT_STATE_FILE = ROOT_DIR / "data" / "iterm_launch_state.json"
DIRECT_MODE_ENV = "ITERM_IO_BRIDGE_DIRECT"
_SHORT_AGENT_ID_RE = re.compile(r"^a(\d{2})$", re.IGNORECASE)
_CANONICAL_AGENT_ID_RE = re.compile(r"^agent_(\d{2})$", re.IGNORECASE)
_AUTH_401_RE = re.compile(r"(http\s*401|status\s*code\s*401|invalidstatuscode\s*\(?401\)?)", re.IGNORECASE)

# iTerm API / subprocess 调用最大等待秒数
_ITERM_TIMEOUT_SEC = int(os.getenv("ITERM_TIMEOUT_SEC", "60"))
_ITERM_AUTH_RETRY_MAX = max(0, int(os.getenv("ITERM_AUTH_RETRY_MAX", "1")))
_ITERM_AUTH_RETRY_SLEEP_SEC = max(0.0, float(os.getenv("ITERM_AUTH_RETRY_SLEEP_SEC", "0.35")))


def _sync_tui_binding_warning(rebind_error: str = "", *, warning_suppressed: bool = False) -> None:
    """将 iTerm 回绑异常同步到 TUI 绑定告警（best-effort）。"""
    try:
        from orchestration_tui_bus import publish_binding_warning

        text = str(rebind_error or "").strip()
        if warning_suppressed:
            publish_binding_warning(None, source="iterm_bridge")
            return
        if not text:
            publish_binding_warning(None, source="iterm_bridge")
            return
        if text.lower().startswith("state rebind skipped:"):
            publish_binding_warning(None, source="iterm_bridge")
            return
        publish_binding_warning(text[:300], source="iterm_bridge")
    except Exception:
        pass


def _iterm_run_with_timeout(coroutine_fn, timeout: float = 0) -> None:
    """在独立线程中执行 iterm2.run_until_complete，超时则放弃。

    关键安全措施：
    - 连接前清除旧 ITERM2_COOKIE/KEY（防止 one-time-use cookie 过期导致 401）
    - 401 时自动重试 1 次（重新获取 cookie）
    - 超时后通过 cancel_futures 取消挂起的 Future
    - 工作线程中创建的 event loop 在 finally 中被强制关闭
    - 避免 fd 泄漏到主进程（websocket / unix socket）
    """
    import asyncio
    import concurrent.futures
    import iterm2

    effective_timeout = timeout if timeout > 0 else _ITERM_TIMEOUT_SEC
    _t0 = _time_mod.monotonic()
    _perf_log.info("[iterm-perf] _iterm_run_with_timeout START  timeout=%.1fs  coro=%s", effective_timeout, getattr(coroutine_fn, "__qualname__", repr(coroutine_fn)))

    _MAX_AUTH_RETRIES = 2  # 最多尝试 2 次（首次 + 1 次重试）

    def _force_close_loop():
        """强制关闭当前线程的 event loop，带超时保护。"""
        try:
            loop = asyncio.get_event_loop()
        except RuntimeError:
            return
        if loop is None or loop.is_closed():
            return
        # 先取消所有 pending 任务
        try:
            pending = asyncio.all_tasks(loop)
            for t in pending:
                t.cancel()
            # 限时等待取消完成（防止 websocket 清理卡死）
            if pending:
                loop.run_until_complete(
                    asyncio.wait_for(
                        asyncio.gather(*pending, return_exceptions=True),
                        timeout=3.0,
                    )
                )
        except Exception:
            pass
        try:
            loop.run_until_complete(loop.shutdown_asyncgens())
        except Exception:
            pass
        try:
            loop.close()
        except Exception:
            pass

    def _guarded_run(coro_fn):
        """在工作线程中运行 iterm2，结束后关闭 event loop。"""
        _t_thread = _time_mod.monotonic()
        _perf_log.info("[iterm-perf]   _guarded_run entered  thread_setup=%.3fs", _t_thread - _t0)
        last_err = None
        try:
            for attempt in range(_MAX_AUTH_RETRIES):
                # 每次连接前清除可能过期的 cookie，强制重新获取
                os.environ.pop("ITERM2_COOKIE", None)
                os.environ.pop("ITERM2_KEY", None)
                # 401 重试前需要新 event loop（旧的已被 iterm2 关闭或损坏）
                if attempt > 0:
                    _force_close_loop()
                    # iterm2 库会自行创建新 loop，无需手动 new_event_loop
                try:
                    iterm2.run_until_complete(coro_fn)
                    return  # 成功
                except Exception as e:
                    err_str = str(e)
                    _perf_log.info("[iterm-perf]   _guarded_run attempt=%d  error=%s", attempt + 1, err_str)
                    # 只对 401 auth 错误重试
                    if "401" in err_str and attempt < _MAX_AUTH_RETRIES - 1:
                        last_err = e
                        _perf_log.info("[iterm-perf]   _guarded_run retrying after 401...")
                        continue
                    raise
            if last_err:
                raise last_err
        finally:
            # 所有重试结束后统一清理 event loop（仅执行一次，不会阻塞重试）
            _force_close_loop()
        _perf_log.info("[iterm-perf]   _guarded_run done     elapsed=%.3fs", _time_mod.monotonic() - _t_thread)

    pool = concurrent.futures.ThreadPoolExecutor(max_workers=1)
    future = pool.submit(_guarded_run, coroutine_fn)
    try:
        future.result(timeout=effective_timeout)
    except concurrent.futures.TimeoutError:
        future.cancel()
        _perf_log.warning("[iterm-perf] _iterm_run_with_timeout TIMEOUT  elapsed=%.3fs", _time_mod.monotonic() - _t0)
        raise TimeoutError(f"iTerm API 调用超时 ({effective_timeout}s)")
    finally:
        _perf_log.info("[iterm-perf] _iterm_run_with_timeout END  elapsed=%.3fs", _time_mod.monotonic() - _t0)
        pool.shutdown(wait=False, cancel_futures=True)


@dataclass
class AgentSession:
    index: int
    agent_id: str
    agent_name: str
    session_id: str


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _normalize_state_file(state_file: str = "") -> Path:
    path = Path(state_file).expanduser() if state_file else DEFAULT_STATE_FILE
    if not path.is_absolute():
        path = ROOT_DIR / path
    return path


def _canonical_agent_id(value: Any, default_index: int = 0) -> str:
    text = str(value or "").strip()
    if not text and default_index > 0:
        return f"agent_{default_index:02d}"
    if not text:
        return ""

    canonical_match = _CANONICAL_AGENT_ID_RE.fullmatch(text)
    if canonical_match:
        return f"agent_{int(canonical_match.group(1)):02d}"

    short_match = _SHORT_AGENT_ID_RE.fullmatch(text)
    if short_match:
        return f"agent_{int(short_match.group(1)):02d}"

    return text


def _load_state(path: Path) -> dict[str, Any]:
    if not path.exists():
        raise FileNotFoundError(f"state 文件不存在: {path}")
    return json.loads(path.read_text(encoding="utf-8"))


def _save_state(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    content = json.dumps(payload, ensure_ascii=False, indent=2)
    temp_path = path.with_name(f".{path.name}.tmp-{os.getpid()}")
    temp_path.write_text(content, encoding="utf-8")
    os.replace(temp_path, path)


def _default_agent_row(index: int) -> dict[str, Any]:
    agent_id = f"agent_{index:02d}"
    agent_name = f"Runtime Agent {index:02d}"
    return {
        "index": index,
        "agent_id": agent_id,
        "agent_name": agent_name,
        "session_label": f"{agent_id} | {agent_name}",
        "badge": f"A{index:02d}",
        "session_id": "",
    }


def _build_agent_sessions(state: dict[str, Any]) -> list[AgentSession]:
    rows: list[AgentSession] = []
    agents = state.get("agents", [])
    session_ids = state.get("session_ids", [])

    for index, agent in enumerate(agents, start=1):
        if not isinstance(agent, dict):
            continue

        session_id = str(agent.get("session_id", "") or "").strip()
        if not session_id and index - 1 < len(session_ids):
            session_id = str(session_ids[index - 1]).strip()

        rows.append(
            AgentSession(
                index=int(agent.get("index", index) or index),
                agent_id=_canonical_agent_id(agent.get("agent_id", ""), default_index=index),
                agent_name=str(agent.get("agent_name", "") or f"Runtime Agent {index:02d}").strip(),
                session_id=session_id,
            )
        )

    if not rows and session_ids:
        for index, session_id in enumerate(session_ids, start=1):
            rows.append(
                AgentSession(
                    index=index,
                    agent_id=f"agent_{index:02d}",
                    agent_name=f"Runtime Agent {index:02d}",
                    session_id=str(session_id),
                )
            )

    return rows


def _parse_agent_ids(agent_id: str) -> list[str]:
    text = str(agent_id or "").strip()
    if not text:
        return []
    parts = text.replace(";", ",").split(",")
    normalized: list[str] = []
    seen: set[str] = set()
    for part in parts:
        canonical = _canonical_agent_id(part).strip()
        if not canonical:
            continue
        lowered = canonical.lower()
        if lowered in seen:
            continue
        seen.add(lowered)
        normalized.append(canonical)
    return normalized


def _resolve_targets(rows: list[AgentSession], target_agent_ids: list[str], all_agents: bool) -> list[AgentSession]:
    rows = [row for row in rows if row.session_id]
    if all_agents:
        return rows

    if not target_agent_ids:
        raise ValueError("请传 agent_id 或 all_agents=true")

    wanted = set(target_agent_ids)
    selected = [row for row in rows if row.agent_id in wanted]
    missing = [agent for agent in target_agent_ids if agent not in {row.agent_id for row in selected}]
    if missing:
        raise ValueError(f"未找到以下 agent 的 session: {', '.join(missing)}")

    return selected


def _result_header(state_path: Path, targets: list[AgentSession]) -> dict[str, Any]:
    return {
        "ts": _now_iso(),
        "state_file": str(state_path),
        "target_count": len(targets),
        "targets": [
            {
                "index": item.index,
                "agent_id": item.agent_id,
                "agent_name": item.agent_name,
                "session_id": item.session_id,
            }
            for item in targets
        ],
    }


def _is_direct_mode_enabled() -> bool:
    # ACP-BUS 主进程绝不走 direct mode（防止 iterm2 fd 泄漏杀死服务）
    if os.getenv("_ACP_BUS_PROCESS") == "1":
        return False
    # 显式设置的 ITERM_IO_BRIDGE_DIRECT 优先级最高
    value = str(os.getenv(DIRECT_MODE_ENV, "") or "").strip().lower()
    if value:
        return value in {"1", "true", "yes", "on"}
    # Tests patch internal helpers and expect in-process execution path.
    if os.getenv("PYTEST_CURRENT_TEST"):
        return True
    return False


def _contains_http_401(value: Any) -> bool:
    text = str(value or "").strip()
    if not text:
        return False
    return bool(_AUTH_401_RE.search(text))


def _rows_have_http_401(rows: list[dict[str, Any]]) -> bool:
    for row in rows:
        if _contains_http_401(row.get("error")):
            return True
    return False


def _result_has_http_401(result: dict[str, Any]) -> bool:
    if _contains_http_401(result.get("error")):
        return True
    rows = result.get("results")
    if isinstance(rows, list):
        for row in rows:
            if isinstance(row, dict) and _contains_http_401(row.get("error")):
                return True
    return False


def _run_subprocess_with_http_401_retry(
    *,
    action: str,
    text: str | None,
    agent_id: str,
    all_agents: bool,
    wait_sec: float,
    read_lines: int,
    state_file: str,
) -> tuple[dict[str, Any], int]:
    """调用 subprocess I/O，遇到 iTerm 401 间歇错误时自动重试。"""
    attempts = 0
    result = _run_io_via_subprocess(
        action=action,
        text=text,
        agent_id=agent_id,
        all_agents=all_agents,
        wait_sec=wait_sec,
        read_lines=read_lines,
        state_file=state_file,
    )

    while attempts < _ITERM_AUTH_RETRY_MAX and _result_has_http_401(result):
        attempts += 1
        _perf_log.warning(
            "[iterm-perf] subprocess %s hit HTTP401, retry=%d/%d",
            action,
            attempts,
            _ITERM_AUTH_RETRY_MAX,
        )
        _time_mod.sleep(_ITERM_AUTH_RETRY_SLEEP_SEC * attempts)
        result = _run_io_via_subprocess(
            action=action,
            text=text,
            agent_id=agent_id,
            all_agents=all_agents,
            wait_sec=wait_sec,
            read_lines=read_lines,
            state_file=state_file,
        )

    return result, attempts


def _subprocess_agent_args(agent_id: str, all_agents: bool) -> list[str]:
    if all_agents:
        return ["--all"]

    args: list[str] = []
    for item in _parse_agent_ids(agent_id):
        args.extend(["--agent", item])
    return args


def _run_io_via_subprocess(
    action: str,
    *,
    text: str | None,
    agent_id: str,
    all_agents: bool,
    wait_sec: float,
    read_lines: int,
    state_file: str,
) -> dict[str, Any]:
    _t0 = _time_mod.monotonic()
    script = ROOT_DIR / "scripts" / "iterm_agent_io.py"
    cmd = [
        sys.executable,
        str(script),
        "--action",
        str(action),
        "--lines",
        str(max(0, int(read_lines))),
    ]

    cmd.extend(_subprocess_agent_args(agent_id=agent_id, all_agents=all_agents))

    normalized_state_file = str(state_file or "").strip()
    if normalized_state_file:
        cmd.extend(["--state-file", normalized_state_file])

    if action == "send":
        cmd.extend(["--text", str(text or ""), "--wait-sec", str(max(0.0, float(wait_sec)))])

    child_env = os.environ.copy()
    child_env[DIRECT_MODE_ENV] = "1"
    # 关键：移除 _ACP_BUS_PROCESS，否则子进程也会认为自己是 ACP 主进程，
    # 导致 _is_direct_mode_enabled() 返回 False → 递归生成子进程 → 无限挂起。
    child_env.pop("_ACP_BUS_PROCESS", None)

    # subprocess 超时必须小于 MCP 工具超时（默认 90s），否则 MCP tool timeout
    # 后线程仍在等待 subprocess，造成资源泄漏和服务假死。
    _mcp_tool_timeout = int(os.environ.get("ACP_BUS_TOOL_TIMEOUT_SEC", "90"))
    subprocess_timeout = min(
        _ITERM_TIMEOUT_SEC + max(0.0, float(wait_sec)) + 10,
        max(30, _mcp_tool_timeout - 15),  # 留 15s 余量给 MCP wrapper
    )

    _perf_log.info("[iterm-perf] _run_io_via_subprocess START  action=%s  cmd=%s", action, " ".join(cmd[-4:]))
    _perf_log.info("[iterm-perf]   subprocess python=%s  timeout=%.0fs", sys.executable, subprocess_timeout)

    try:
        completed = subprocess.run(
            cmd,
            cwd=str(ROOT_DIR),
            capture_output=True,
            text=True,
            check=False,
            env=child_env,
            timeout=subprocess_timeout,
        )
    except subprocess.TimeoutExpired:
        _perf_log.warning("[iterm-perf] _run_io_via_subprocess TIMEOUT  elapsed=%.3fs", _time_mod.monotonic() - _t0)
        return {
            "ok": False,
            "ts": _now_iso(),
            "action": action,
            "error": f"subprocess 超时 ({subprocess_timeout:.0f}s)",
        }

    _elapsed = _time_mod.monotonic() - _t0
    stdout = (completed.stdout or "").strip()
    stderr = (completed.stderr or "").strip()
    _perf_log.info("[iterm-perf] _run_io_via_subprocess END  elapsed=%.3fs  rc=%d  stdout_len=%d  stderr_len=%d", _elapsed, completed.returncode, len(stdout), len(stderr))
    if stderr:
        _perf_log.info("[iterm-perf]   subprocess stderr:\n%s", stderr)

    if not stdout:
        return {
            "ok": False,
            "ts": _now_iso(),
            "action": action,
            "error": f"subprocess 无输出 (rc={completed.returncode})",
            "stderr": stderr,
        }

    try:
        payload = json.loads(stdout)
    except json.JSONDecodeError:
        return {
            "ok": False,
            "ts": _now_iso(),
            "action": action,
            "error": f"subprocess 输出不是 JSON (rc={completed.returncode})",
            "stdout": stdout,
            "stderr": stderr,
        }

    if stderr and isinstance(payload, dict) and "stderr" not in payload:
        payload["stderr"] = stderr
    return payload


def _sanitize_screen_line(text: Any) -> str:
    value = str(text or "")
    if not value:
        return ""

    value = value.replace("\x00", "")
    value = "".join(ch for ch in value if ch == "\t" or ord(ch) >= 32)
    return value.rstrip("\r\n")


def _coalesce_wrapped_lines(raw_lines: list[tuple[str, bool]], max_lines: int) -> list[str]:
    """Merge terminal soft-wrapped rows into logical lines."""
    if max_lines <= 0:
        return []

    merged: list[str] = []
    current = ""

    for raw_text, hard_eol in raw_lines:
        text = _sanitize_screen_line(raw_text)
        if not text.strip():
            if hard_eol and current.strip():
                merged.append(current)
                current = ""
            continue

        if current:
            current += text
        else:
            current = text

        if hard_eol:
            if current.strip():
                merged.append(current)
            current = ""

    if current.strip():
        merged.append(current)

    return merged[-max_lines:]


async def _read_tail_lines(session: Any, lines: int) -> list[str]:
    if lines <= 0:
        return []

    screen = await session.async_get_screen_contents()
    total = int(getattr(screen, "number_of_lines", 0) or 0)
    # Read extra physical rows so we can rebuild wrapped logical lines.
    scan_rows = max(lines * 4, lines + 20)
    start = max(0, total - scan_rows)

    raw_lines: list[tuple[str, bool]] = []
    for index in range(start, total):
        try:
            line_obj = screen.line(index)
            text = str(getattr(line_obj, "string", ""))
            hard_eol = bool(getattr(line_obj, "hard_eol", True))
        except Exception:
            continue
        raw_lines.append((text, hard_eol))

    return _coalesce_wrapped_lines(raw_lines, max_lines=lines)


def _run_iterm_io(
    targets: list[AgentSession],
    text: str | None,
    append_enter: bool,
    wait_sec: float,
    read_lines: int,
) -> list[dict[str, Any]]:
    import asyncio
    import iterm2

    rows: list[dict[str, Any]] = []
    _t0 = _time_mod.monotonic()
    _action = "send" if text is not None else "read"
    _perf_log.info("[iterm-perf] _run_iterm_io START  action=%s  targets=%d  wait=%.1fs  lines=%d", _action, len(targets), wait_sec, read_lines)

    async def main(connection):
        _tc = _time_mod.monotonic()
        app = await iterm2.async_get_app(connection)
        _perf_log.info("[iterm-perf]   _run_iterm_io connect+get_app  elapsed=%.3fs", _time_mod.monotonic() - _tc)
        session_rows: list[tuple[Any, dict[str, Any]]] = []

        for target in targets:
            row = {
                "agent_id": target.agent_id,
                "agent_name": target.agent_name,
                "session_id": target.session_id,
                "sent": False,
                "read": False,
                "output": [],
                "error": "",
            }

            session = app.get_session_by_id(target.session_id)
            if session is None:
                row["error"] = "session not found in iTerm"
                rows.append(row)
                continue

            rows.append(row)
            session_rows.append((session, row))

        if text is not None:
            _ts = _time_mod.monotonic()
            for session, row in session_rows:
                try:
                    await session.async_send_text(text)
                    row["sent"] = True
                except Exception as e:
                    row["error"] = f"send failed: {e}"
            _perf_log.info("[iterm-perf]   _run_iterm_io send_text  elapsed=%.3fs  targets=%d", _time_mod.monotonic() - _ts, len(session_rows))

            if append_enter:
                await asyncio.sleep(0.2)
                _te = _time_mod.monotonic()
                for session, row in session_rows:
                    if row["error"]:
                        continue
                    try:
                        await session.async_send_text("\r")
                    except Exception as e:
                        row["error"] = f"submit failed: {e}"
                _perf_log.info("[iterm-perf]   _run_iterm_io send_enter  elapsed=%.3fs", _time_mod.monotonic() - _te)

        if text is not None and read_lines > 0 and wait_sec > 0:
            _perf_log.info("[iterm-perf]   _run_iterm_io wait_start  wait_sec=%.1fs", wait_sec)
            await asyncio.sleep(wait_sec)
            _perf_log.info("[iterm-perf]   _run_iterm_io wait_done")

        if read_lines > 0:
            _tr = _time_mod.monotonic()
            for session, row in session_rows:
                if row["error"]:
                    continue
                try:
                    row["output"] = await _read_tail_lines(session, read_lines)
                    row["read"] = True
                except Exception as e:
                    row["error"] = f"read failed: {e}"
            _perf_log.info("[iterm-perf]   _run_iterm_io read_output  elapsed=%.3fs  targets=%d", _time_mod.monotonic() - _tr, len(session_rows))

    try:
        _iterm_run_with_timeout(main)
    except TimeoutError:
        for row in rows:
            if not row.get("error"):
                row["error"] = f"iTerm API 超时 ({_ITERM_TIMEOUT_SEC}s)"
    _perf_log.info("[iterm-perf] _run_iterm_io END  action=%s  total_elapsed=%.3fs", _action, _time_mod.monotonic() - _t0)
    return rows


def _iter_tab_sessions(tab: Any) -> list[Any]:
    all_sessions = getattr(tab, "all_sessions", None)
    if isinstance(all_sessions, list) and all_sessions:
        return [item for item in all_sessions if item is not None]

    sessions = getattr(tab, "sessions", None)
    if isinstance(sessions, list) and sessions:
        return [item for item in sessions if item is not None]

    root = getattr(tab, "root", None)
    root_sessions = getattr(root, "sessions", None)
    if isinstance(root_sessions, list) and root_sessions:
        return [item for item in root_sessions if item is not None]

    current = getattr(tab, "current_session", None)
    if current is not None:
        return [current]
    return []


def _list_live_sessions(window_id: str = "") -> tuple[str, list[dict[str, str]]]:
    import iterm2

    _t0 = _time_mod.monotonic()
    _perf_log.info("[iterm-perf] _list_live_sessions START  window_id=%r", window_id)
    selected_window_id = ""
    sessions: list[dict[str, str]] = []

    async def main(connection):
        nonlocal selected_window_id, sessions

        _ta = _time_mod.monotonic()
        app = await iterm2.async_get_app(connection)
        _perf_log.info("[iterm-perf]   async_get_app          elapsed=%.3fs", _time_mod.monotonic() - _ta)
        windows = list(getattr(app, "terminal_windows", []) or [])
        _perf_log.info("[iterm-perf]   windows_count=%d", len(windows))

        normalized_window_id = str(window_id or "").strip()
        if normalized_window_id:
            target_windows = []
            for window in windows:
                if str(getattr(window, "window_id", "") or "") == normalized_window_id:
                    target_windows = [window]
                    break
            if not target_windows:
                target_windows = windows
        else:
            target_windows = windows

        if not target_windows:
            return

        selected_window_id = str(getattr(target_windows[0], "window_id", "") or "")

        seen: set[str] = set()
        _session_count = 0
        _var_total = 0.0
        for target_window in target_windows:
            for tab in list(getattr(target_window, "tabs", []) or []):
                for session in _iter_tab_sessions(tab):
                    session_id = str(getattr(session, "session_id", "") or "").strip()
                    if not session_id or session_id in seen:
                        continue
                    seen.add(session_id)
                    _session_count += 1

                    badge = ""
                    agent_id = ""
                    agent_name = ""
                    agent_label = ""
                    get_variable = getattr(session, "async_get_variable", None)
                    if get_variable is not None:
                        _tv = _time_mod.monotonic()
                        try:
                            badge_value = await get_variable("user.badge")
                            badge = str(badge_value or "").strip()
                        except Exception:
                            badge = ""

                        try:
                            agent_id_value = await get_variable("user.agent_id")
                            agent_id = _canonical_agent_id(agent_id_value)
                        except Exception:
                            agent_id = ""

                        try:
                            agent_name_value = await get_variable("user.agent_name")
                            agent_name = str(agent_name_value or "").strip()
                        except Exception:
                            agent_name = ""

                        try:
                            agent_label_value = await get_variable("user.agent_label")
                            agent_label = str(agent_label_value or "").strip()
                        except Exception:
                            agent_label = ""
                        _var_elapsed = _time_mod.monotonic() - _tv
                        _var_total += _var_elapsed
                        _perf_log.info("[iterm-perf]   session %s vars  elapsed=%.3fs", session_id, _var_elapsed)

                    session_name = str(getattr(session, "name", "") or "").strip()
                    resolved_name = agent_label or agent_name or session_name
                    sessions.append(
                        {
                            "session_id": session_id,
                            "badge": badge,
                            "agent_id": agent_id,
                            "agent_name": agent_name,
                            "agent_label": agent_label,
                            "name": resolved_name,
                            "session_name": session_name,
                        }
                    )
        _perf_log.info("[iterm-perf]   sessions_found=%d  vars_total=%.3fs", _session_count, _var_total)

    try:
        _iterm_run_with_timeout(main)
    except TimeoutError:
        pass  # 返回已收集到的 sessions（可能为空）
    _perf_log.info("[iterm-perf] _list_live_sessions END  elapsed=%.3fs  sessions=%d", _time_mod.monotonic() - _t0, len(sessions))
    return selected_window_id, sessions


def _list_live_session_ids(window_id: str = "") -> tuple[str, list[str]]:
    selected_window_id, sessions = _list_live_sessions(window_id)
    session_ids = [
        str(item.get("session_id", "") or "").strip()
        for item in sessions
        if str(item.get("session_id", "") or "").strip()
    ]
    return selected_window_id, session_ids


def _async_reinject_variables(agents: list[dict[str, Any]], live_by_id: dict[str, dict[str, str]]) -> None:
    """在位置回绑后，将 badge/agent_id/agent_name 重新注入到 live session，
    以便后续 rebind 可直接走 badge 匹配快路径。"""
    import iterm2

    _t0 = _time_mod.monotonic()
    _perf_log.info("[iterm-perf] _async_reinject_variables START  agents=%d  live=%d", len(agents), len(live_by_id))

    pairs: list[tuple[str, dict[str, Any]]] = []
    for agent in agents:
        if not isinstance(agent, dict):
            continue
        sid = str(agent.get("session_id", "") or "").strip()
        if not sid or sid not in live_by_id:
            continue
        pairs.append((sid, agent))

    if not pairs:
        _perf_log.info("[iterm-perf] _async_reinject_variables SKIP (no pairs)")
        return

    async def _inject(connection):
        app = await iterm2.async_get_app(connection)
        for sid, agent in pairs:
            session = app.get_session_by_id(sid)
            if session is None:
                continue
            badge = str(agent.get("badge", "") or "").strip()
            agent_id = str(agent.get("agent_id", "") or "").strip()
            agent_name = str(agent.get("agent_name", "") or "").strip()
            agent_label = str(agent.get("session_label", "") or "").strip()
            try:
                if badge:
                    await session.async_set_variable("user.badge", badge)
                if agent_id:
                    await session.async_set_variable("user.agent_id", agent_id)
                if agent_name:
                    await session.async_set_variable("user.agent_name", agent_name)
                if agent_label:
                    await session.async_set_variable("user.agent_label", agent_label)
            except Exception:
                pass

    try:
        _iterm_run_with_timeout(_inject, timeout=5.0)
    except Exception:
        pass  # best-effort，不影响 rebind 主流程
    _perf_log.info("[iterm-perf] _async_reinject_variables END  pairs=%d  elapsed=%.3fs", len(pairs), _time_mod.monotonic() - _t0)


def _rebind_state_sessions(state_path: Path, state: dict[str, Any]) -> dict[str, Any]:
    _t0 = _time_mod.monotonic()
    _perf_log.info("[iterm-perf] _rebind_state_sessions START")
    selected_window_id, live_sessions = _list_live_sessions(str(state.get("window_id", "") or ""))
    _perf_log.info("[iterm-perf] _rebind_state_sessions after _list_live_sessions  elapsed=%.3fs  live=%d", _time_mod.monotonic() - _t0, len(live_sessions))
    live_session_ids = [
        str(item.get("session_id", "") or "").strip()
        for item in live_sessions
        if str(item.get("session_id", "") or "").strip()
    ]
    if not live_session_ids:
        return {
            "state": state,
            "rebound": False,
            "rebound_count": 0,
            "reason": "no_live_sessions",
        }

    new_state = json.loads(json.dumps(state, ensure_ascii=False))
    if not isinstance(new_state, dict):
        new_state = {}

    structure_changed = False
    raw_agents = new_state.get("agents", [])
    if not isinstance(raw_agents, list):
        raw_agents = []
        structure_changed = True

    normalized_agents: list[dict[str, Any]] = []
    for index, row in enumerate(raw_agents, start=1):
        defaults = _default_agent_row(index)
        current = dict(row) if isinstance(row, dict) else {}
        if not isinstance(row, dict):
            structure_changed = True

        for field, default_value in defaults.items():
            existing = current.get(field)
            if field == "index":
                if existing is None:
                    current[field] = default_value
                    structure_changed = True
                continue

            if str(existing or "").strip():
                continue

            current[field] = default_value
            structure_changed = True

        current["index"] = index
        normalized_agent_id = _canonical_agent_id(current.get("agent_id", ""), default_index=index)
        if str(current.get("agent_id", "") or "") != normalized_agent_id:
            current["agent_id"] = normalized_agent_id
            structure_changed = True
        normalized_agents.append(current)

    expected_count = int(new_state.get("tab_count") or new_state.get("count") or 0)
    if expected_count <= 0:
        expected_count = len(live_session_ids)

    while len(normalized_agents) < expected_count:
        normalized_agents.append(_default_agent_row(len(normalized_agents) + 1))
        structure_changed = True

    agents = normalized_agents
    new_state["agents"] = agents

    live_by_id: dict[str, dict[str, str]] = {
        str(item.get("session_id", "") or "").strip(): item
        for item in live_sessions
        if str(item.get("session_id", "") or "").strip()
    }
    unassigned_live_ids = set(live_by_id.keys())

    rebound_count = 0

    def _set_agent_session(agent_row: dict[str, Any], session_id: str) -> None:
        nonlocal rebound_count
        old_session_id = str(agent_row.get("session_id", "") or "").strip()
        new_session_id = str(session_id or "").strip()
        if old_session_id != new_session_id:
            rebound_count += 1
        agent_row["session_id"] = new_session_id
        if new_session_id:
            unassigned_live_ids.discard(new_session_id)

    unresolved_agents: list[dict[str, Any]] = []

    # 第一阶段：保留仍然有效的原 session 绑定，避免“按顺序错位”。
    for agent in agents:
        if not isinstance(agent, dict):
            continue

        old_session_id = str(agent.get("session_id", "") or "").strip()
        if old_session_id and old_session_id in live_by_id:
            _set_agent_session(agent, old_session_id)
            continue

        unresolved_agents.append(agent)

    # 第二阶段：优先按 badge 精确回绑（A01/A02...），可修复错位状态。
    for agent in unresolved_agents:
        if not isinstance(agent, dict):
            continue

        current_session_id = str(agent.get("session_id", "") or "").strip()
        if current_session_id and current_session_id in live_by_id:
            continue

        badge = str(agent.get("badge", "") or "").strip()
        if not badge:
            continue

        matched_ids = [
            session_id
            for session_id in list(unassigned_live_ids)
            if str(live_by_id.get(session_id, {}).get("badge", "") or "").strip() == badge
        ]
        if len(matched_ids) == 1:
            _set_agent_session(agent, matched_ids[0])

    # 第 2.5 阶段：按位置顺序回绑（iTerm 重启后 pane 布局通常不变）。
    # 仅当大多数 agent 的 badge 匹配都失败时启用（表明发生了整体重启
    # 而非个别 session 丢失），避免少量丢失时错位。
    still_unresolved_count = sum(
        1 for a in unresolved_agents
        if isinstance(a, dict)
        and not (
            str(a.get("session_id", "") or "").strip()
            and str(a.get("session_id", "") or "").strip() in live_by_id
        )
    )
    majority_unresolved = still_unresolved_count >= max(2, len(agents) // 2)
    enough_live_for_position = len(unassigned_live_ids) >= still_unresolved_count
    if majority_unresolved and enough_live_for_position and unassigned_live_ids:
        ordered_unassigned = [
            str(item.get("session_id", "") or "").strip()
            for item in live_sessions
            if str(item.get("session_id", "") or "").strip() in unassigned_live_ids
        ]
        position_idx = 0
        for agent in unresolved_agents:
            if not isinstance(agent, dict):
                continue
            cur = str(agent.get("session_id", "") or "").strip()
            if cur and cur in live_by_id:
                continue
            if position_idx < len(ordered_unassigned):
                _set_agent_session(agent, ordered_unassigned[position_idx])
                position_idx += 1

        # 回注 user 变量以便后续 rebind 可直接 badge 匹配
        _async_reinject_variables(agents, live_by_id)

    # 第三阶段：按标签/代理标识兜底（不依赖会被进程覆盖的 session_name）。
    for agent in unresolved_agents:
        if not isinstance(agent, dict):
            continue

        current_session_id = str(agent.get("session_id", "") or "").strip()
        if current_session_id and current_session_id in live_by_id:
            continue

        session_label = str(agent.get("session_label", "") or "").strip().lower()
        agent_id = _canonical_agent_id(agent.get("agent_id", "")).lower()
        agent_name = str(agent.get("agent_name", "") or "").strip().lower()
        if not session_label and not agent_id and not agent_name:
            continue

        matched_ids: list[str] = []
        for session_id in list(unassigned_live_ids):
            live_row = live_by_id.get(session_id, {})
            live_label = str(live_row.get("agent_label", "") or "").strip().lower()
            live_agent_id = _canonical_agent_id(live_row.get("agent_id", "")).lower()
            live_agent_name = str(live_row.get("agent_name", "") or "").strip().lower()
            live_name = str(live_row.get("name", "") or "").strip().lower()

            if session_label and live_label and session_label == live_label:
                matched_ids.append(session_id)
                continue
            if agent_id and live_agent_id and agent_id == live_agent_id:
                matched_ids.append(session_id)
                continue
            if agent_name and live_agent_name and agent_name == live_agent_name:
                matched_ids.append(session_id)
                continue
            if session_label and live_name and session_label == live_name:
                matched_ids.append(session_id)
                continue

        if len(matched_ids) == 1:
            _set_agent_session(agent, matched_ids[0])

    # 第四阶段：仍未匹配的失效会话统一清空。
    # 安全守卫：如果 live sessions 数量远少于已知 agent 数量，
    # 说明 iTerm API 可能返回了不完整结果，避免误清。
    # 阈值：live 数量至少为已知含 SID 的 agent 数的一半。
    total_agents_with_sid = sum(
        1 for a in agents if isinstance(a, dict)
        and str(a.get("session_id", "") or "").strip()
    )
    api_seems_complete = (
        total_agents_with_sid == 0
        or len(live_by_id) >= max(1, total_agents_with_sid // 2)
    )
    for agent in unresolved_agents:
        if not isinstance(agent, dict):
            continue
        current_session_id = str(agent.get("session_id", "") or "").strip()
        if current_session_id and current_session_id not in live_by_id and api_seems_complete:
            _set_agent_session(agent, "")

    resolved_session_ids: list[str] = []
    seen: set[str] = set()
    for agent in agents:
        if not isinstance(agent, dict):
            continue
        session_id = str(agent.get("session_id", "") or "").strip()
        if not session_id or session_id in seen:
            continue
        seen.add(session_id)
        resolved_session_ids.append(session_id)

    existing_session_ids = [str(item).strip() for item in state.get("session_ids", []) if str(item).strip()]
    if existing_session_ids != resolved_session_ids:
        rebound_count += 1

    previous_window_id = str(state.get("window_id", "") or "")
    if selected_window_id and selected_window_id != previous_window_id:
        rebound_count += 1
        new_state["window_id"] = selected_window_id

    new_state["session_ids"] = resolved_session_ids
    expected_tab_count = int(new_state.get("count") or len(agents) or len(resolved_session_ids))
    new_state["tab_count"] = max(len(resolved_session_ids), expected_tab_count)

    if structure_changed:
        rebound_count += 1

    if rebound_count <= 0:
        return {
            "state": state,
            "rebound": False,
            "rebound_count": 0,
            "reason": "no_state_change",
        }

    _save_state(state_path, new_state)
    _perf_log.info("[iterm-perf] _rebind_state_sessions END (rebound)  elapsed=%.3fs  rebound_count=%d", _time_mod.monotonic() - _t0, rebound_count)
    return {
        "state": new_state,
        "rebound": True,
        "rebound_count": rebound_count,
        "reason": "rebound_applied",
    }


def _has_missing_sessions(rows: list[dict[str, Any]]) -> bool:
    for row in rows:
        if str(row.get("error", "") or "").strip() == "session not found in iTerm":
            return True
    return False


def _refresh_state_via_rebind(state_path: Path, state: dict[str, Any]) -> tuple[dict[str, Any], bool, int, str]:
    _t0 = _time_mod.monotonic()
    _perf_log.info("[iterm-perf] _refresh_state_via_rebind START")
    try:
        payload = _rebind_state_sessions(state_path, state)
    except Exception as e:
        _perf_log.info("[iterm-perf] _refresh_state_via_rebind END (exception)  elapsed=%.3fs  err=%s", _time_mod.monotonic() - _t0, e)
        return state, False, 0, f"state rebind failed: {e}"

    if payload.get("rebound"):
        rebound_state = payload.get("state")
        if isinstance(rebound_state, dict):
            _perf_log.info("[iterm-perf] _refresh_state_via_rebind END (rebound)  elapsed=%.3fs", _time_mod.monotonic() - _t0)
            return rebound_state, True, int(payload.get("rebound_count") or 0), ""
        _perf_log.info("[iterm-perf] _refresh_state_via_rebind END (invalid)  elapsed=%.3fs", _time_mod.monotonic() - _t0)
        return state, False, 0, "state rebind failed: invalid rebound state"

    reason = str(payload.get("reason", "") or "").strip()
    if reason:
        _perf_log.info("[iterm-perf] _refresh_state_via_rebind END (skipped: %s)  elapsed=%.3fs", reason, _time_mod.monotonic() - _t0)
        return state, False, 0, f"state rebind skipped: {reason}"

    _perf_log.info("[iterm-perf] _refresh_state_via_rebind END  elapsed=%.3fs", _time_mod.monotonic() - _t0)
    return state, False, 0, ""


def _run_direct_with_optional_rebind(
    *,
    state_path: Path,
    state: dict[str, Any],
    target_agent_ids: list[str],
    all_agents: bool,
    text: str | None,
    append_enter: bool,
    wait_sec: float,
    read_lines: int,
) -> dict[str, Any]:
    current_state = state
    state_rebound = False
    rebound_count = 0
    rebind_error = ""

    def _apply_rebind() -> bool:
        nonlocal current_state, state_rebound, rebound_count, rebind_error

        rebound_state, changed, changed_count, error = _refresh_state_via_rebind(state_path, current_state)
        if changed:
            current_state = rebound_state
            state_rebound = True
            rebound_count += max(1, int(changed_count or 0))
            rebind_error = ""
            return True

        if error and not rebind_error:
            rebind_error = error
        return False

    precheck_rebind = False
    try:
        initial_sessions = _build_agent_sessions(current_state)
        initial_targets = _resolve_targets(initial_sessions, target_agent_ids, all_agents=all_agents)

        _, live_session_ids = _list_live_session_ids(str(current_state.get("window_id", "") or ""))
        live_session_set = {sid for sid in live_session_ids if sid}
        if live_session_set:
            target_session_ids = {item.session_id for item in initial_targets if item.session_id}
            if not target_session_ids.issubset(live_session_set):
                precheck_rebind = True
    except (Exception, SystemExit):
        precheck_rebind = True

    if precheck_rebind:
        _apply_rebind()

    try:
        sessions = _build_agent_sessions(current_state)
        targets = _resolve_targets(sessions, target_agent_ids, all_agents=all_agents)
    except ValueError as e:
        if _apply_rebind():
            sessions = _build_agent_sessions(current_state)
            targets = _resolve_targets(sessions, target_agent_ids, all_agents=all_agents)
        else:
            if rebind_error:
                raise ValueError(f"{e} ({rebind_error})") from e
            raise

    rows = _run_iterm_io(
        targets=targets,
        text=text,
        append_enter=append_enter,
        wait_sec=wait_sec,
        read_lines=read_lines,
    )

    if _has_missing_sessions(rows):
        if _apply_rebind():
            rebound_sessions = _build_agent_sessions(current_state)
            rebound_targets = _resolve_targets(rebound_sessions, target_agent_ids, all_agents=all_agents)
            rebound_rows = _run_iterm_io(
                targets=rebound_targets,
                text=text,
                append_enter=append_enter,
                wait_sec=wait_sec,
                read_lines=read_lines,
            )
            targets = rebound_targets
            rows = rebound_rows

    return {
        "targets": targets,
        "rows": rows,
        "state_rebound": state_rebound,
        "rebound_count": rebound_count,
        "rebind_error": rebind_error,
    }


def list_iterm_agent_sessions(state_file: str = "") -> dict[str, Any]:
    _t0 = _time_mod.monotonic()
    _perf_log.info("[iterm-perf] === list_iterm_agent_sessions START ===")
    try:
        if not _is_direct_mode_enabled():
            _perf_log.info("[iterm-perf]   direct_mode=OFF → subprocess")
            result, retry_count = _run_subprocess_with_http_401_retry(
                action="list",
                text=None,
                agent_id="",
                all_agents=True,
                wait_sec=0.0,
                read_lines=0,
                state_file=state_file,
            )
            if retry_count > 0 and isinstance(result, dict):
                result["auth_retry_count"] = retry_count
            _perf_log.info("[iterm-perf] === list_iterm_agent_sessions END (subprocess)  elapsed=%.3fs ===", _time_mod.monotonic() - _t0)
            return result

        state_path = _normalize_state_file(state_file)
        _t1 = _time_mod.monotonic()
        state = _load_state(state_path)
        _perf_log.info("[iterm-perf]   _load_state  elapsed=%.3fs", _time_mod.monotonic() - _t1)

        _t2 = _time_mod.monotonic()
        rebound_state, state_rebound, rebound_count, rebind_error = _refresh_state_via_rebind(state_path, state)
        _perf_log.info("[iterm-perf]   _refresh_state_via_rebind  elapsed=%.3fs  rebound=%s  count=%d  err=%r", _time_mod.monotonic() - _t2, state_rebound, rebound_count, rebind_error)
        if state_rebound:
            state = rebound_state

        sessions = _build_agent_sessions(state)

        result = {
            "ok": True,
            "ts": _now_iso(),
            "state_file": str(state_path),
            "tab_count": int(state.get("tab_count") or len(sessions)),
            "window_id": state.get("window_id", ""),
            "state_rebound": state_rebound,
            "rebound_count": int(rebound_count or 0),
            "sessions": [
                {
                    "index": item.index,
                    "agent_id": item.agent_id,
                    "agent_name": item.agent_name,
                    "session_id": item.session_id,
                }
                for item in sessions
            ],
        }
        if rebind_error:
            if sessions and "Cannot run the event loop while another loop is running" in rebind_error:
                result["rebind_warning_suppressed"] = True
                _sync_tui_binding_warning(rebind_error, warning_suppressed=True)
            else:
                result["rebind_error"] = rebind_error
                _sync_tui_binding_warning(rebind_error)
        else:
            _sync_tui_binding_warning("")
        _perf_log.info("[iterm-perf] === list_iterm_agent_sessions END  elapsed=%.3fs  sessions=%d ===", _time_mod.monotonic() - _t0, len(sessions))
        return result
    except Exception as e:
        _perf_log.info("[iterm-perf] === list_iterm_agent_sessions END (error)  elapsed=%.3fs  err=%s ===", _time_mod.monotonic() - _t0, e)
        return {
            "ok": False,
            "ts": _now_iso(),
            "error": str(e),
        }


def send_iterm_input(
    text: str,
    agent_id: str = "",
    all_agents: bool = False,
    wait_sec: float = 0.4,
    read_lines: int = 20,
    state_file: str = "",
    append_enter: bool = True,
) -> dict[str, Any]:
    _t0 = _time_mod.monotonic()
    _perf_log.info("[iterm-perf] === send_iterm_input START  agent=%s all=%s wait=%.1fs ===", agent_id or "*", all_agents, wait_sec)
    try:
        normalized_text = str(text)
        normalized_wait_sec = max(0.0, float(wait_sec))
        normalized_read_lines = max(0, int(read_lines))

        if not _is_direct_mode_enabled():
            result, retry_count = _run_subprocess_with_http_401_retry(
                action="send",
                text=normalized_text,
                agent_id=agent_id,
                all_agents=all_agents,
                wait_sec=normalized_wait_sec,
                read_lines=normalized_read_lines,
                state_file=state_file,
            )
            if retry_count > 0 and isinstance(result, dict):
                result["auth_retry_count"] = retry_count
            _perf_log.info("[iterm-perf] === send_iterm_input END (subprocess)  elapsed=%.3fs ===", _time_mod.monotonic() - _t0)
            return result

        state_path = _normalize_state_file(state_file)
        target_agent_ids = _parse_agent_ids(agent_id)
        state = _load_state(state_path)
        auth_retry_count = 0

        direct_result = _run_direct_with_optional_rebind(
            state_path=state_path,
            state=state,
            target_agent_ids=target_agent_ids,
            all_agents=all_agents,
            text=normalized_text,
            append_enter=bool(append_enter),
            wait_sec=normalized_wait_sec,
            read_lines=normalized_read_lines,
        )
        rows = direct_result["rows"]
        targets = direct_result["targets"]

        while auth_retry_count < _ITERM_AUTH_RETRY_MAX and _rows_have_http_401(rows):
            auth_retry_count += 1
            _perf_log.warning(
                "[iterm-perf] direct send hit HTTP401, retry=%d/%d",
                auth_retry_count,
                _ITERM_AUTH_RETRY_MAX,
            )
            _time_mod.sleep(_ITERM_AUTH_RETRY_SLEEP_SEC * auth_retry_count)
            state = _load_state(state_path)
            direct_result = _run_direct_with_optional_rebind(
                state_path=state_path,
                state=state,
                target_agent_ids=target_agent_ids,
                all_agents=all_agents,
                text=normalized_text,
                append_enter=bool(append_enter),
                wait_sec=normalized_wait_sec,
                read_lines=normalized_read_lines,
            )
            rows = direct_result["rows"]
            targets = direct_result["targets"]

        ok = all(not row.get("error") for row in rows)

        result = {
            "ok": ok,
            **_result_header(state_path, targets),
            "action": "send",
            "text": normalized_text,
            "read_lines": normalized_read_lines,
            "state_rebound": bool(direct_result.get("state_rebound")),
            "rebound_count": int(direct_result.get("rebound_count") or 0),
            "results": rows,
        }
        if auth_retry_count > 0:
            result["auth_retry_count"] = auth_retry_count
        rebind_error = str(direct_result.get("rebind_error", "") or "").strip()
        if rebind_error:
            result["rebind_error"] = rebind_error
            _sync_tui_binding_warning(rebind_error)
        else:
            _sync_tui_binding_warning("")
        return result
    except Exception as e:
        _perf_log.info("[iterm-perf] === send_iterm_input END (error)  elapsed=%.3fs  err=%s ===", _time_mod.monotonic() - _t0, e)
        return {
            "ok": False,
            "ts": _now_iso(),
            "action": "send",
            "error": str(e),
        }


def read_iterm_output(
    agent_id: str = "",
    all_agents: bool = False,
    read_lines: int = 20,
    state_file: str = "",
) -> dict[str, Any]:
    _t0 = _time_mod.monotonic()
    _perf_log.info("[iterm-perf] === read_iterm_output START  agent=%s all=%s ===", agent_id or "*", all_agents)
    try:
        normalized_read_lines = max(0, int(read_lines))

        if not _is_direct_mode_enabled():
            result, retry_count = _run_subprocess_with_http_401_retry(
                action="read",
                text=None,
                agent_id=agent_id,
                all_agents=all_agents,
                wait_sec=0.0,
                read_lines=normalized_read_lines,
                state_file=state_file,
            )
            if retry_count > 0 and isinstance(result, dict):
                result["auth_retry_count"] = retry_count
            _perf_log.info("[iterm-perf] === read_iterm_output END (subprocess)  elapsed=%.3fs ===", _time_mod.monotonic() - _t0)
            return result

        state_path = _normalize_state_file(state_file)
        target_agent_ids = _parse_agent_ids(agent_id)
        state = _load_state(state_path)
        auth_retry_count = 0

        direct_result = _run_direct_with_optional_rebind(
            state_path=state_path,
            state=state,
            target_agent_ids=target_agent_ids,
            all_agents=all_agents,
            text=None,
            append_enter=True,
            wait_sec=0.0,
            read_lines=normalized_read_lines,
        )
        rows = direct_result["rows"]
        targets = direct_result["targets"]

        while auth_retry_count < _ITERM_AUTH_RETRY_MAX and _rows_have_http_401(rows):
            auth_retry_count += 1
            _perf_log.warning(
                "[iterm-perf] direct read hit HTTP401, retry=%d/%d",
                auth_retry_count,
                _ITERM_AUTH_RETRY_MAX,
            )
            _time_mod.sleep(_ITERM_AUTH_RETRY_SLEEP_SEC * auth_retry_count)
            state = _load_state(state_path)
            direct_result = _run_direct_with_optional_rebind(
                state_path=state_path,
                state=state,
                target_agent_ids=target_agent_ids,
                all_agents=all_agents,
                text=None,
                append_enter=True,
                wait_sec=0.0,
                read_lines=normalized_read_lines,
            )
            rows = direct_result["rows"]
            targets = direct_result["targets"]

        ok = all(not row.get("error") for row in rows)

        result = {
            "ok": ok,
            **_result_header(state_path, targets),
            "action": "read",
            "read_lines": normalized_read_lines,
            "state_rebound": bool(direct_result.get("state_rebound")),
            "rebound_count": int(direct_result.get("rebound_count") or 0),
            "results": rows,
        }
        if auth_retry_count > 0:
            result["auth_retry_count"] = auth_retry_count
        rebind_error = str(direct_result.get("rebind_error", "") or "").strip()
        if rebind_error:
            result["rebind_error"] = rebind_error
            _sync_tui_binding_warning(rebind_error)
        else:
            _sync_tui_binding_warning("")
        return result
    except Exception as e:
        _perf_log.info("[iterm-perf] === read_iterm_output END (error)  elapsed=%.3fs  err=%s ===", _time_mod.monotonic() - _t0, e)
        return {
            "ok": False,
            "ts": _now_iso(),
            "action": "read",
            "error": str(e),
        }


# ── Terminal Live Viewer APIs ──────────────────────────────────────────

import threading
import asyncio
import time

_active_streamers: dict[str, dict] = {}  # session_id -> {"thread", "stop_event", "loop"}
_streamer_lock = threading.Lock()


def read_session_screen(session_id: str, lines: int = 60) -> dict[str, Any]:
    """One-shot read of a session's screen contents."""
    import iterm2

    result: dict[str, Any] = {"ok": False, "session_id": session_id, "lines": []}

    async def main(connection):
        app = await iterm2.async_get_app(connection)
        session = app.get_session_by_id(session_id)
        if session is None:
            result["error"] = "session not found"
            return
        output = await _read_tail_lines(session, lines)
        result["ok"] = True
        result["lines"] = output
        result["ts"] = _now_iso()

    try:
        _iterm_run_with_timeout(main)
    except Exception as e:
        result["error"] = str(e)
    return result


def send_to_session(session_id: str, text: str) -> dict[str, Any]:
    """Send text (command) to a specific iTerm session."""
    import iterm2

    result: dict[str, Any] = {"ok": False, "session_id": session_id}

    async def main(connection):
        app = await iterm2.async_get_app(connection)
        session = app.get_session_by_id(session_id)
        if session is None:
            result["error"] = "session not found"
            return
        await session.async_send_text(text)
        result["ok"] = True
        result["ts"] = _now_iso()

    try:
        _iterm_run_with_timeout(main)
    except Exception as e:
        result["error"] = str(e)
    return result


def start_session_streamer(session_id: str, publish_fn=None) -> dict[str, Any]:
    """Start a ScreenStreamer for a session. Pushes output via publish_fn(event_type, payload).
    Returns immediately; streaming runs in background thread.
    """
    with _streamer_lock:
        if session_id in _active_streamers:
            return {"ok": True, "session_id": session_id, "status": "already_running"}

    stop_event = threading.Event()

    def _streamer_thread():
        import iterm2

        async def main(connection):
            app = await iterm2.async_get_app(connection)
            session = app.get_session_by_id(session_id)
            if session is None:
                if publish_fn:
                    publish_fn("terminal_output", {
                        "session_id": session_id,
                        "error": "session not found",
                        "ts": _now_iso(),
                    })
                return

            async with session.get_screen_streamer() as streamer:
                while not stop_event.is_set():
                    try:
                        contents = await asyncio.wait_for(
                            streamer.async_get(), timeout=2.0
                        )
                    except asyncio.TimeoutError:
                        continue
                    except Exception:
                        break

                    if contents is None:
                        continue

                    raw_lines: list[tuple[str, bool]] = []
                    num = int(getattr(contents, "number_of_lines", 0) or 0)
                    for i in range(num):
                        try:
                            line_obj = contents.line(i)
                            raw_lines.append(
                                (
                                    str(getattr(line_obj, "string", "")),
                                    bool(getattr(line_obj, "hard_eol", True)),
                                )
                            )
                        except Exception:
                            continue

                    lines = _coalesce_wrapped_lines(raw_lines, max_lines=max(1, num))
                    if publish_fn and lines:
                        publish_fn("terminal_output", {
                            "session_id": session_id,
                            "lines": lines,
                            "ts": _now_iso(),
                        })

        try:
            iterm2.run_until_complete(main)
        except Exception:
            pass
        finally:
            # 清理本线程的 event loop，防止 fd 泄漏到主进程
            try:
                loop = asyncio.get_event_loop()
                if loop and not loop.is_closed():
                    loop.close()
            except Exception:
                pass
            with _streamer_lock:
                _active_streamers.pop(session_id, None)

    thread = threading.Thread(target=_streamer_thread, daemon=True, name=f"streamer-{session_id}")
    with _streamer_lock:
        _active_streamers[session_id] = {"thread": thread, "stop_event": stop_event}
    thread.start()

    return {"ok": True, "session_id": session_id, "status": "started", "ts": _now_iso()}


def stop_session_streamer(session_id: str) -> dict[str, Any]:
    """Stop a running ScreenStreamer."""
    with _streamer_lock:
        entry = _active_streamers.pop(session_id, None)
    if entry is None:
        return {"ok": True, "session_id": session_id, "status": "not_running"}
    entry["stop_event"].set()
    entry["thread"].join(timeout=5.0)
    return {"ok": True, "session_id": session_id, "status": "stopped", "ts": _now_iso()}


def list_active_streamers() -> list[str]:
    """Return list of session_ids with active streamers."""
    with _streamer_lock:
        return list(_active_streamers.keys())
