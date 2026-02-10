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
import os
import subprocess
import sys
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

ROOT_DIR = Path(__file__).resolve().parents[1]
DEFAULT_STATE_FILE = ROOT_DIR / "data" / "iterm_launch_state.json"
DIRECT_MODE_ENV = "ITERM_IO_BRIDGE_DIRECT"


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


def _load_state(path: Path) -> dict[str, Any]:
    if not path.exists():
        raise FileNotFoundError(f"state 文件不存在: {path}")
    return json.loads(path.read_text(encoding="utf-8"))


def _save_state(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")


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
                agent_id=str(agent.get("agent_id", "") or f"agent_{index:02d}").strip(),
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
    return [part.strip() for part in parts if part.strip()]


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
    value = str(os.getenv(DIRECT_MODE_ENV, "") or "").strip().lower()
    return value in {"1", "true", "yes", "on"}


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

    completed = subprocess.run(
        cmd,
        cwd=str(ROOT_DIR),
        capture_output=True,
        text=True,
        check=False,
        env=child_env,
    )

    stdout = (completed.stdout or "").strip()
    stderr = (completed.stderr or "").strip()
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


async def _read_tail_lines(session: Any, lines: int) -> list[str]:
    if lines <= 0:
        return []

    screen = await session.async_get_screen_contents()
    total = int(getattr(screen, "number_of_lines", 0) or 0)
    start = max(0, total - lines)

    output: list[str] = []
    for index in range(start, total):
        try:
            text = screen.line(index).string
        except Exception:
            continue
        line = _sanitize_screen_line(text)
        if line.strip():
            output.append(line)

    return output[-lines:]


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

    async def main(connection):
        app = await iterm2.async_get_app(connection)
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
            for session, row in session_rows:
                try:
                    await session.async_send_text(text)
                    row["sent"] = True
                except Exception as e:
                    row["error"] = f"send failed: {e}"

            if append_enter:
                await asyncio.sleep(0.2)
                for session, row in session_rows:
                    if row["error"]:
                        continue
                    try:
                        await session.async_send_text("\r")
                    except Exception as e:
                        row["error"] = f"submit failed: {e}"

        if text is not None and read_lines > 0 and wait_sec > 0:
            await asyncio.sleep(wait_sec)

        if read_lines > 0:
            for session, row in session_rows:
                if row["error"]:
                    continue
                try:
                    row["output"] = await _read_tail_lines(session, read_lines)
                    row["read"] = True
                except Exception as e:
                    row["error"] = f"read failed: {e}"

    iterm2.run_until_complete(main)
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

    selected_window_id = ""
    sessions: list[dict[str, str]] = []

    async def main(connection):
        nonlocal selected_window_id, sessions

        app = await iterm2.async_get_app(connection)
        windows = list(getattr(app, "terminal_windows", []) or [])

        target_window = None
        normalized_window_id = str(window_id or "").strip()
        if normalized_window_id:
            for window in windows:
                if str(getattr(window, "window_id", "") or "") == normalized_window_id:
                    target_window = window
                    break

        if target_window is None:
            target_window = getattr(app, "current_terminal_window", None)
        if target_window is None and windows:
            target_window = windows[0]
        if target_window is None:
            return

        selected_window_id = str(getattr(target_window, "window_id", "") or "")

        seen: set[str] = set()
        for tab in list(getattr(target_window, "tabs", []) or []):
            for session in _iter_tab_sessions(tab):
                session_id = str(getattr(session, "session_id", "") or "").strip()
                if not session_id or session_id in seen:
                    continue
                seen.add(session_id)

                badge = ""
                agent_id = ""
                agent_name = ""
                agent_label = ""
                get_variable = getattr(session, "async_get_variable", None)
                if get_variable is not None:
                    try:
                        badge_value = await get_variable("user.badge")
                        badge = str(badge_value or "").strip()
                    except Exception:
                        badge = ""

                    try:
                        agent_id_value = await get_variable("user.agent_id")
                        agent_id = str(agent_id_value or "").strip()
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

    iterm2.run_until_complete(main)
    return selected_window_id, sessions


def _list_live_session_ids(window_id: str = "") -> tuple[str, list[str]]:
    selected_window_id, sessions = _list_live_sessions(window_id)
    session_ids = [
        str(item.get("session_id", "") or "").strip()
        for item in sessions
        if str(item.get("session_id", "") or "").strip()
    ]
    return selected_window_id, session_ids


def _rebind_state_sessions(state_path: Path, state: dict[str, Any]) -> dict[str, Any]:
    selected_window_id, live_sessions = _list_live_sessions(str(state.get("window_id", "") or ""))
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

    agents = new_state.get("agents", [])
    if not isinstance(agents, list):
        agents = []
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

    # 第三阶段：按标签/代理标识兜底（不依赖会被进程覆盖的 session_name）。
    for agent in unresolved_agents:
        if not isinstance(agent, dict):
            continue

        current_session_id = str(agent.get("session_id", "") or "").strip()
        if current_session_id and current_session_id in live_by_id:
            continue

        session_label = str(agent.get("session_label", "") or "").strip().lower()
        agent_id = str(agent.get("agent_id", "") or "").strip().lower()
        agent_name = str(agent.get("agent_name", "") or "").strip().lower()
        if not session_label and not agent_id and not agent_name:
            continue

        matched_ids: list[str] = []
        for session_id in list(unassigned_live_ids):
            live_row = live_by_id.get(session_id, {})
            live_label = str(live_row.get("agent_label", "") or "").strip().lower()
            live_agent_id = str(live_row.get("agent_id", "") or "").strip().lower()
            live_agent_name = str(live_row.get("agent_name", "") or "").strip().lower()
            live_name = str(live_row.get("name", "") or "").strip().lower()
            live_session_name = str(live_row.get("session_name", "") or "").strip().lower()

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
            if agent_id and live_name and agent_id in live_name:
                matched_ids.append(session_id)
                continue
            if agent_id and live_session_name and agent_id in live_session_name:
                matched_ids.append(session_id)

        if len(matched_ids) == 1:
            _set_agent_session(agent, matched_ids[0])

    # 第四阶段：仍未匹配的失效会话统一清空。
    for agent in unresolved_agents:
        if not isinstance(agent, dict):
            continue
        current_session_id = str(agent.get("session_id", "") or "").strip()
        if current_session_id and current_session_id not in live_by_id:
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

    if rebound_count <= 0:
        return {
            "state": state,
            "rebound": False,
            "rebound_count": 0,
            "reason": "no_state_change",
        }

    _save_state(state_path, new_state)
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
    sessions = _build_agent_sessions(state)
    targets = _resolve_targets(sessions, target_agent_ids, all_agents=all_agents)

    rows = _run_iterm_io(
        targets=targets,
        text=text,
        append_enter=append_enter,
        wait_sec=wait_sec,
        read_lines=read_lines,
    )

    state_rebound = False
    rebound_count = 0
    rebind_error = ""

    if _has_missing_sessions(rows):
        try:
            rebound_payload = _rebind_state_sessions(state_path, state)
            if rebound_payload.get("rebound"):
                rebound_state = rebound_payload.get("state")
                if isinstance(rebound_state, dict):
                    rebound_sessions = _build_agent_sessions(rebound_state)
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
                    state_rebound = True
                    rebound_count = int(rebound_payload.get("rebound_count") or 0)
            else:
                reason = str(rebound_payload.get("reason", "") or "").strip()
                if reason:
                    rebind_error = f"state rebind skipped: {reason}"
        except Exception as e:
            rebind_error = f"state rebind failed: {e}"

    return {
        "targets": targets,
        "rows": rows,
        "state_rebound": state_rebound,
        "rebound_count": rebound_count,
        "rebind_error": rebind_error,
    }


def list_iterm_agent_sessions(state_file: str = "") -> dict[str, Any]:
    try:
        state_path = _normalize_state_file(state_file)
        state = _load_state(state_path)
        sessions = _build_agent_sessions(state)

        return {
            "ok": True,
            "ts": _now_iso(),
            "state_file": str(state_path),
            "tab_count": int(state.get("tab_count") or len(sessions)),
            "window_id": state.get("window_id", ""),
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
    except Exception as e:
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
    try:
        normalized_text = str(text)
        normalized_wait_sec = max(0.0, float(wait_sec))
        normalized_read_lines = max(0, int(read_lines))

        if not _is_direct_mode_enabled():
            return _run_io_via_subprocess(
                action="send",
                text=normalized_text,
                agent_id=agent_id,
                all_agents=all_agents,
                wait_sec=normalized_wait_sec,
                read_lines=normalized_read_lines,
                state_file=state_file,
            )

        state_path = _normalize_state_file(state_file)
        state = _load_state(state_path)

        direct_result = _run_direct_with_optional_rebind(
            state_path=state_path,
            state=state,
            target_agent_ids=_parse_agent_ids(agent_id),
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
        rebind_error = str(direct_result.get("rebind_error", "") or "").strip()
        if rebind_error:
            result["rebind_error"] = rebind_error
        return result
    except Exception as e:
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
    try:
        normalized_read_lines = max(0, int(read_lines))

        if not _is_direct_mode_enabled():
            return _run_io_via_subprocess(
                action="read",
                text=None,
                agent_id=agent_id,
                all_agents=all_agents,
                wait_sec=0.0,
                read_lines=normalized_read_lines,
                state_file=state_file,
            )

        state_path = _normalize_state_file(state_file)
        state = _load_state(state_path)

        direct_result = _run_direct_with_optional_rebind(
            state_path=state_path,
            state=state,
            target_agent_ids=_parse_agent_ids(agent_id),
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
        rebind_error = str(direct_result.get("rebind_error", "") or "").strip()
        if rebind_error:
            result["rebind_error"] = rebind_error
        return result
    except Exception as e:
        return {
            "ok": False,
            "ts": _now_iso(),
            "action": "read",
            "error": str(e),
        }
