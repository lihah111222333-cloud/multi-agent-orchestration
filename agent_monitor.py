"""Agent monitor helpers."""

from __future__ import annotations

from collections.abc import Iterable
from datetime import datetime, timezone
from typing import Any, Callable

ERROR_KEYWORDS = ("traceback", "error", "exception")
DISCONNECTED_KEYWORDS = ("timeout", "connection refused", "econnreset")
PROMPT_ONLY_MARKERS = ("$", "#", ">>>", "...", ">")
STATUS_NAMES = ("running", "idle", "stuck", "error", "disconnected", "unknown")
DEFAULT_STUCK_SEC = 60


def _normalize_lines(lines: Iterable[object]) -> list[str]:
    """Normalize raw output lines into stripped text rows."""
    normalized: list[str] = []
    for item in lines:
        text = str(item).strip()
        if text:
            normalized.append(text)
    return normalized


def _is_prompt_only(lines: list[str]) -> bool:
    """Return whether all visible lines are shell/python prompts."""
    if not lines:
        return True
    return all(line in PROMPT_ONLY_MARKERS for line in lines)


def _safe_int(value: Any, default: int = 0) -> int:
    try:
        return int(value)
    except (TypeError, ValueError):
        return int(default)


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _empty_summary() -> dict[str, int]:
    return {
        "total": 0,
        "healthy": 0,
        "unhealthy": 0,
        **{name: 0 for name in STATUS_NAMES},
    }


def _summarize_agents(agents: list[dict[str, Any]]) -> dict[str, int]:
    summary = _empty_summary()
    for row in agents:
        status = str(row.get("status", "unknown")).strip().lower()
        if status not in STATUS_NAMES:
            status = "unknown"
        summary[status] += 1
        summary["total"] += 1

    summary["healthy"] = summary["running"] + summary["idle"]
    summary["unhealthy"] = summary["total"] - summary["healthy"]
    return summary


def classify_status(
    lines: list[str],
    has_session: bool = True,
    stagnant_sec: int = 0,
) -> str:
    """Classify agent runtime status from output snippets.

    Args:
        lines: Recent terminal output lines.
        has_session: Whether backing session exists.
        stagnant_sec: Seconds since last output change.

    Returns:
        One of: running/idle/stuck/error/disconnected/unknown.
    """
    if not has_session:
        return "unknown"

    normalized = _normalize_lines(lines)
    if _is_prompt_only(normalized):
        return "idle"

    merged = "\n".join(normalized).lower()

    if any(keyword in merged for keyword in ERROR_KEYWORDS):
        return "error"

    if any(keyword in merged for keyword in DISCONNECTED_KEYWORDS):
        return "disconnected"

    if _safe_int(stagnant_sec) >= DEFAULT_STUCK_SEC:
        return "stuck"

    return "running"


def patrol_agents_once(
    *,
    list_sessions_func: Callable[[], dict[str, Any]],
    read_output_func: Callable[..., dict[str, Any]],
    read_lines: int = 30,
    now_ts: float | None = None,
    status_memory: dict[str, dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """Run one patrol cycle and build agent status snapshot."""
    ts = _now_iso()
    memory = status_memory if status_memory is not None else {}
    now_value = float(now_ts) if now_ts is not None else 0.0

    if now_value <= 0:
        from time import time as _time

        now_value = float(_time())

    sessions_payload = list_sessions_func()
    if not sessions_payload.get("ok"):
        return {
            "ok": False,
            "ts": ts,
            "error": str(sessions_payload.get("error", "list_sessions_failed")),
            "summary": _empty_summary(),
            "agents": [],
            "source": {"sessions_ok": False, "output_ok": False},
        }

    sessions = sessions_payload.get("sessions", [])
    if not isinstance(sessions, list):
        sessions = []

    output_payload = read_output_func(all_agents=True, read_lines=max(1, _safe_int(read_lines, default=30)))
    if not output_payload.get("ok"):
        error_text = str(output_payload.get("error", "read_output_failed"))
        agents = [
            {
                "agent_id": str(item.get("agent_id", "")),
                "agent_name": str(item.get("agent_name", "")),
                "session_id": str(item.get("session_id", "")),
                "status": "unknown",
                "stagnant_sec": 0,
                "error": error_text,
                "output_tail": [],
            }
            for item in sessions
            if isinstance(item, dict)
        ]
        return {
            "ok": False,
            "ts": ts,
            "error": error_text,
            "summary": _summarize_agents(agents),
            "agents": agents,
            "source": {"sessions_ok": True, "output_ok": False},
        }

    rows = output_payload.get("results", [])
    if not isinstance(rows, list):
        rows = []

    row_by_agent: dict[str, dict[str, Any]] = {}
    for row in rows:
        if not isinstance(row, dict):
            continue
        agent_id = str(row.get("agent_id", "")).strip()
        if agent_id:
            row_by_agent[agent_id] = row

    agents: list[dict[str, Any]] = []
    for item in sessions:
        if not isinstance(item, dict):
            continue

        agent_id = str(item.get("agent_id", "")).strip()
        agent_name = str(item.get("agent_name", "")).strip()
        session_id = str(item.get("session_id", "")).strip()

        row = row_by_agent.get(agent_id, {})
        output_tail_raw = row.get("output", [])
        output_tail = output_tail_raw if isinstance(output_tail_raw, list) else [str(output_tail_raw)]
        output_tail = [str(line) for line in output_tail if str(line).strip()]

        error_text = str(row.get("error", "")).strip()
        has_session = bool(session_id) and "session not found" not in error_text.lower()

        fingerprint = "\n".join(output_tail[-6:])
        previous = memory.get(agent_id)
        if previous and previous.get("fingerprint") == fingerprint:
            last_change_ts = float(previous.get("last_change_ts", now_value))
        else:
            last_change_ts = now_value

        memory[agent_id] = {
            "fingerprint": fingerprint,
            "last_change_ts": last_change_ts,
        }

        stagnant_sec = max(0, int(now_value - last_change_ts))
        status = classify_status(
            output_tail,
            has_session=has_session,
            stagnant_sec=stagnant_sec,
        )

        if error_text and status not in {"error", "disconnected"}:
            status = "disconnected"
        if status not in STATUS_NAMES:
            status = "unknown"

        agents.append(
            {
                "agent_id": agent_id,
                "agent_name": agent_name,
                "session_id": session_id,
                "status": status,
                "stagnant_sec": stagnant_sec,
                "error": error_text,
                "output_tail": output_tail[-20:],
            }
        )

    return {
        "ok": True,
        "ts": ts,
        "summary": _summarize_agents(agents),
        "agents": agents,
        "source": {"sessions_ok": True, "output_ok": True},
    }
