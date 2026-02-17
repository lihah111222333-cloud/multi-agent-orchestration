"""Agent monitor helpers.

GO:migrate → go/internal/monitor/patrol.go
GO:target  → Go goroutine + ticker
GO:notes:
  - classify_status → Go 等价, 用 strings.Contains
  - patrol_agents_loop → Go for-select + time.Ticker (更简洁)
  - _safe_int/_safe_float → Go 编译时类型检查 (消除)
  - status_memory → Go sync.Map 或 struct 字段
"""

from __future__ import annotations

import time
from collections.abc import Iterable
from datetime import datetime, timezone
from typing import Any, Callable

ERROR_KEYWORDS = ("traceback", "error", "exception")
DISCONNECTED_KEYWORDS = ("timeout", "connection refused", "econnreset")
PROMPT_ONLY_MARKERS = ("$", "#", ">>>", "...", ">")
STATUS_NAMES = ("running", "idle", "stuck", "error", "disconnected", "unknown")
DEFAULT_STUCK_SEC = 60
DEFAULT_INTERVAL_SEC = 5.0


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


def _safe_float(value: Any, default: float = 0.0) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return float(default)


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


def _event_payload_from_snapshot(snapshot: dict[str, Any]) -> dict[str, Any]:
    payload = {
        "ok": bool(snapshot.get("ok")),
        "ts": str(snapshot.get("ts", "") or ""),
        "summary": snapshot.get("summary", _empty_summary()),
        "agents": snapshot.get("agents", []),
        "source": snapshot.get("source", {}),
    }
    error_text = str(snapshot.get("error", "") or "").strip()
    if error_text:
        payload["error"] = error_text
    return payload


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
        now_value = float(time.time())

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
            last_change_ts = _safe_float(previous.get("last_change_ts"), default=now_value)
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


def run_patrol_cycle(
    *,
    list_sessions_func: Callable[[], dict[str, Any]],
    read_output_func: Callable[..., dict[str, Any]],
    upsert_status_func: Callable[..., Any] | None = None,
    publish_event_func: Callable[[str, dict[str, Any]], Any] | None = None,
    read_lines: int = 30,
    now_ts: float | None = None,
    status_memory: dict[str, dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """Execute one patrol cycle, optionally persist status and publish SSE payload."""
    snapshot = patrol_agents_once(
        list_sessions_func=list_sessions_func,
        read_output_func=read_output_func,
        read_lines=read_lines,
        now_ts=now_ts,
        status_memory=status_memory,
    )

    store_errors: list[str] = []
    persisted = 0
    if callable(upsert_status_func):
        agents = snapshot.get("agents", [])
        if isinstance(agents, list):
            for row in agents:
                if not isinstance(row, dict):
                    continue

                agent_id = str(row.get("agent_id", "")).strip()
                if not agent_id:
                    continue

                try:
                    upsert_status_func(
                        agent_id=agent_id,
                        agent_name=str(row.get("agent_name", "") or ""),
                        session_id=str(row.get("session_id", "") or ""),
                        status=str(row.get("status", "unknown") or "unknown"),
                        stagnant_sec=_safe_int(row.get("stagnant_sec"), default=0),
                        error=str(row.get("error", "") or ""),
                        output_tail=row.get("output_tail") if isinstance(row.get("output_tail"), list) else [],
                    )
                    persisted += 1
                except Exception as exc:
                    store_errors.append(f"{agent_id}:{exc}")

    source = snapshot.get("source") if isinstance(snapshot.get("source"), dict) else {}
    source["store_ok"] = len(store_errors) == 0
    snapshot["source"] = source
    snapshot["persisted"] = persisted

    if store_errors:
        snapshot["ok"] = False
        if not snapshot.get("error"):
            snapshot["error"] = "; ".join(store_errors[:3])

    if callable(publish_event_func):
        publish_event_func("agent_status", _event_payload_from_snapshot(snapshot))

    return snapshot


def patrol_agents_loop(
    *,
    list_sessions_func: Callable[[], dict[str, Any]],
    read_output_func: Callable[..., dict[str, Any]],
    upsert_status_func: Callable[..., Any] | None = None,
    publish_event_func: Callable[[str, dict[str, Any]], Any] | None = None,
    on_cycle_func: Callable[[dict[str, Any]], Any] | None = None,
    interval_sec: float = DEFAULT_INTERVAL_SEC,
    read_lines: int = 30,
    stop_event: Any | None = None,
    status_memory: dict[str, dict[str, Any]] | None = None,
    max_cycles: int | None = None,
    time_func: Callable[[], float] | None = None,
) -> int:
    """Run patrol loop until stopped.

    Returns:
        Number of completed patrol cycles.
    """
    interval = max(0.0, _safe_float(interval_sec, default=DEFAULT_INTERVAL_SEC))
    cycles = 0
    now_provider = time_func if callable(time_func) else time.time

    while True:
        is_set = getattr(stop_event, "is_set", None)
        if callable(is_set) and is_set():
            break

        now_ts = _safe_float(now_provider(), default=time.time())
        try:
            snapshot = run_patrol_cycle(
                list_sessions_func=list_sessions_func,
                read_output_func=read_output_func,
                upsert_status_func=upsert_status_func,
                publish_event_func=publish_event_func,
                read_lines=read_lines,
                now_ts=now_ts,
                status_memory=status_memory,
            )
        except Exception as exc:
            snapshot = {
                "ok": False,
                "ts": _now_iso(),
                "error": str(exc),
                "summary": _empty_summary(),
                "agents": [],
                "source": {"sessions_ok": False, "output_ok": False, "store_ok": False},
            }
            if callable(publish_event_func):
                publish_event_func("agent_status", _event_payload_from_snapshot(snapshot))

        if callable(on_cycle_func):
            on_cycle_func(snapshot)

        cycles += 1
        if max_cycles is not None and cycles >= max(1, int(max_cycles)):
            break

        wait = getattr(stop_event, "wait", None)
        if callable(wait):
            if wait(interval):
                break
        elif interval > 0:
            time.sleep(interval)

    return cycles
