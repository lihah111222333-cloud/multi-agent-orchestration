"""Codex TUI 编排状态总线（run_id 版）。

该模块在 multi-agent-orchestration 侧维护一份轻量状态与事件日志，
用于对齐 Codex TUI 的 orchestration 状态接口：

- BeginOrchestrationTaskState
- UpdateOrchestrationTaskState
- EndOrchestrationTaskState
- SetOrchestrationBindingWarning


GO:split
GO:notes:
  - Python 端 (iterm_bridge 依赖的部分) → GO:keep-python
  - Go 端 (状态总线查询) → go/internal/bus/tui.go
"""

from __future__ import annotations

import json
import os
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Generator

try:
    import fcntl
except ImportError:  # pragma: no cover
    fcntl = None  # type: ignore[assignment]

ROOT_DIR = Path(__file__).resolve().parent
STATE_PATH = ROOT_DIR / "data" / "orchestration_tui_bus.json"
LOCK_PATH = ROOT_DIR / "data" / ".orchestration_tui_bus.lock"
MAX_EVENTS = 2000

__all__ = [
    "publish_begin",
    "publish_update",
    "publish_end",
    "publish_binding_warning",
    "get_snapshot",
    "list_events",
    "reset_state",
]


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _default_state() -> dict[str, Any]:
    return {
        "seq": 0,
        "updated_at": "",
        "binding_warning": None,
        "active_runs": {},
        "events": [],
    }


def _normalize_text(value: Any) -> str | None:
    text = str(value or "").strip()
    return text or None


def _normalize_run_id(run_id: Any) -> str:
    text = str(run_id or "").strip()
    return text or "__missing__"


def _atomic_write_json(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp_path = path.with_name(f".{path.name}.tmp-{os.getpid()}")
    tmp_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    os.replace(tmp_path, path)


@contextmanager
def _locked_state_rw() -> Generator[tuple[dict[str, Any], Any], None, None]:
    """返回 (state, save)；save(new_state) 负责原子落盘。"""
    LOCK_PATH.parent.mkdir(parents=True, exist_ok=True)
    lock_fd = open(LOCK_PATH, "a+", encoding="utf-8")
    try:
        if fcntl is not None:
            fcntl.flock(lock_fd.fileno(), fcntl.LOCK_EX)

        if STATE_PATH.exists():
            raw = STATE_PATH.read_text(encoding="utf-8").strip()
        else:
            raw = ""
        if raw:
            try:
                state = json.loads(raw)
            except json.JSONDecodeError:
                state = _default_state()
        else:
            state = _default_state()

        if not isinstance(state, dict):
            state = _default_state()
        if not isinstance(state.get("active_runs"), dict):
            state["active_runs"] = {}
        if not isinstance(state.get("events"), list):
            state["events"] = []

        def save(new_state: dict[str, Any]) -> None:
            _atomic_write_json(STATE_PATH, new_state)

        yield state, save
    finally:
        if fcntl is not None:
            try:
                fcntl.flock(lock_fd.fileno(), fcntl.LOCK_UN)
            except Exception:
                pass
        lock_fd.close()


def _upsert_run(
    state: dict[str, Any],
    run_id: str,
    status_header: str | None,
    status_details: str | None,
    seq: int,
    ts: str,
) -> None:
    runs = state.setdefault("active_runs", {})
    existing = runs.get(run_id)
    if not isinstance(existing, dict):
        existing = {
            "run_id": run_id,
            "status_header": None,
            "status_details": None,
            "last_seq": 0,
            "updated_at": ts,
        }

    if status_header is not None:
        existing["status_header"] = status_header
    if status_details is not None:
        existing["status_details"] = status_details
    existing["last_seq"] = seq
    existing["updated_at"] = ts
    runs[run_id] = existing


def _append_event(
    state: dict[str, Any],
    *,
    event: str,
    payload: dict[str, Any],
    source: str,
) -> dict[str, Any]:
    seq = int(state.get("seq") or 0) + 1
    ts = _now_iso()
    state["seq"] = seq
    state["updated_at"] = ts

    normalized_payload = dict(payload)
    normalized_source = str(source or "unknown").strip() or "unknown"
    row = {
        "seq": seq,
        "ts": ts,
        "event": event,
        "source": normalized_source,
        "payload": normalized_payload,
    }

    events = state.setdefault("events", [])
    events.append(row)
    if len(events) > MAX_EVENTS:
        del events[: len(events) - MAX_EVENTS]

    if event in {"BeginOrchestrationTaskState", "UpdateOrchestrationTaskState"}:
        run_id = _normalize_run_id(normalized_payload.get("run_id"))
        _upsert_run(
            state,
            run_id=run_id,
            status_header=_normalize_text(normalized_payload.get("status_header")),
            status_details=_normalize_text(normalized_payload.get("status_details")),
            seq=seq,
            ts=ts,
        )
    elif event == "EndOrchestrationTaskState":
        run_id = _normalize_run_id(normalized_payload.get("run_id"))
        runs = state.setdefault("active_runs", {})
        runs.pop(run_id, None)
    elif event == "SetOrchestrationBindingWarning":
        state["binding_warning"] = _normalize_text(normalized_payload.get("warning"))

    runs = state.get("active_runs", {})
    active_count = len(runs) if isinstance(runs, dict) else 0
    return {
        "ok": True,
        "seq": seq,
        "ts": ts,
        "event": event,
        "active_count": active_count,
        "running": active_count > 0,
        "binding_warning": state.get("binding_warning"),
    }


def publish_begin(
    run_id: str,
    status_header: str | None = None,
    status_details: str | None = None,
    source: str = "multi",
) -> dict[str, Any]:
    with _locked_state_rw() as (state, save):
        result = _append_event(
            state,
            event="BeginOrchestrationTaskState",
            payload={
                "run_id": _normalize_run_id(run_id),
                "status_header": _normalize_text(status_header),
                "status_details": _normalize_text(status_details),
            },
            source=source,
        )
        save(state)
        return result


def publish_update(
    run_id: str,
    status_header: str | None = None,
    status_details: str | None = None,
    source: str = "multi",
) -> dict[str, Any]:
    with _locked_state_rw() as (state, save):
        result = _append_event(
            state,
            event="UpdateOrchestrationTaskState",
            payload={
                "run_id": _normalize_run_id(run_id),
                "status_header": _normalize_text(status_header),
                "status_details": _normalize_text(status_details),
            },
            source=source,
        )
        save(state)
        return result


def publish_end(run_id: str, source: str = "multi") -> dict[str, Any]:
    with _locked_state_rw() as (state, save):
        result = _append_event(
            state,
            event="EndOrchestrationTaskState",
            payload={"run_id": _normalize_run_id(run_id)},
            source=source,
        )
        save(state)
        return result


def publish_binding_warning(warning: str | None, source: str = "multi") -> dict[str, Any]:
    with _locked_state_rw() as (state, save):
        result = _append_event(
            state,
            event="SetOrchestrationBindingWarning",
            payload={"warning": _normalize_text(warning)},
            source=source,
        )
        save(state)
        return result


def get_snapshot() -> dict[str, Any]:
    with _locked_state_rw() as (state, _save):
        runs = state.get("active_runs", {})
        if not isinstance(runs, dict):
            runs = {}
        rows = [dict(v) for v in runs.values() if isinstance(v, dict)]
        rows.sort(key=lambda item: int(item.get("last_seq") or 0), reverse=True)
        return {
            "ok": True,
            "seq": int(state.get("seq") or 0),
            "updated_at": str(state.get("updated_at", "") or ""),
            "running": bool(rows),
            "active_count": len(rows),
            "binding_warning": state.get("binding_warning"),
            "active_runs": rows,
        }


def list_events(limit: int = 100, since_seq: int = 0) -> dict[str, Any]:
    try:
        normalized_limit = int(limit or 100)
    except (TypeError, ValueError):
        normalized_limit = 100
    try:
        normalized_since = int(since_seq or 0)
    except (TypeError, ValueError):
        normalized_since = 0
    normalized_limit = max(1, min(normalized_limit, 1000))
    normalized_since = max(0, normalized_since)

    with _locked_state_rw() as (state, _save):
        events = state.get("events", [])
        if not isinstance(events, list):
            events = []
        filtered = [
            dict(item)
            for item in events
            if isinstance(item, dict) and int(item.get("seq") or 0) > normalized_since
        ]
        if len(filtered) > normalized_limit:
            filtered = filtered[-normalized_limit:]
        return {
            "ok": True,
            "seq": int(state.get("seq") or 0),
            "count": len(filtered),
            "events": filtered,
        }


def reset_state(source: str = "multi") -> dict[str, Any]:
    with _locked_state_rw() as (_state, save):
        state = _default_state()
        state["updated_at"] = _now_iso()
        state["events"] = [
            {
                "seq": 1,
                "ts": state["updated_at"],
                "event": "ResetOrchestrationState",
                "source": str(source or "unknown").strip() or "unknown",
                "payload": {},
            }
        ]
        state["seq"] = 1
        save(state)
        return {
            "ok": True,
            "seq": 1,
            "event": "ResetOrchestrationState",
            "active_count": 0,
            "running": False,
            "binding_warning": None,
        }
