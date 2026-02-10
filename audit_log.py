"""全局审计日志（PostgreSQL 持久化）。"""

from __future__ import annotations

import json
from datetime import datetime, timezone
from typing import Any, Optional

from db.postgres import execute, fetch_all
from utils import escape_like, normalize_limit

__all__ = ["append_event", "query_events", "list_filter_values"]


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()





def _row_to_event(row: dict[str, Any]) -> dict[str, Any]:
    ts = row.get("ts")
    if isinstance(ts, datetime):
        ts_text = ts.isoformat()
    else:
        ts_text = str(ts or "")

    event: dict[str, Any] = {
        "ts": ts_text,
        "event_type": str(row.get("event_type", "")),
        "action": str(row.get("action", "")),
        "result": str(row.get("result", "")),
        "actor": str(row.get("actor", "")),
        "target": str(row.get("target", "")),
        "detail": str(row.get("detail", "")),
        "level": str(row.get("level", "INFO")),
    }
    if row.get("extra") is not None:
        event["extra"] = row.get("extra")
    return event


def append_event(
    event_type: str,
    action: str,
    result: str = "ok",
    actor: str = "",
    target: str = "",
    detail: Optional[str] = "",
    level: str = "INFO",
    extra: Optional[dict[str, Any]] = None,
) -> dict[str, Any]:
    """写入一条审计事件。"""

    event: dict[str, Any] = {
        "ts": _now_iso(),
        "event_type": (event_type or "system").strip(),
        "action": (action or "event").strip(),
        "result": (result or "ok").strip(),
        "actor": (actor or "").strip(),
        "target": (target or "").strip(),
        "detail": (detail or "").strip(),
        "level": (level or "INFO").strip().upper(),
    }
    if extra is not None:
        event["extra"] = extra

    execute(
        """
        INSERT INTO audit_events (ts, event_type, action, result, actor, target, detail, level, extra)
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s::jsonb)
        """,
        (
            event["ts"],
            event["event_type"],
            event["action"],
            event["result"],
            event["actor"],
            event["target"],
            event["detail"],
            event["level"],
            json.dumps(extra, ensure_ascii=False) if extra is not None else None,
        ),
    )

    return event


def query_events(
    limit: int = 100,
    event_type: str = "",
    action: str = "",
    result: str = "",
    actor: str = "",
    keyword: str = "",
) -> list[dict[str, Any]]:
    """按条件查询审计日志（新 -> 旧）。"""

    max_items = normalize_limit(limit)
    where = []
    params: list[Any] = []

    if event_type:
        where.append("event_type = %s")
        params.append(event_type)
    if action:
        where.append("action = %s")
        params.append(action)
    if result:
        where.append("result = %s")
        params.append(result)
    if actor:
        where.append("actor = %s")
        params.append(actor)

    if keyword:
        kw = f"%{escape_like(keyword.lower())}%"
        where.append(
            "(" + " OR ".join(
                [
                    "LOWER(event_type) LIKE %s ESCAPE E'\\\\'",
                    "LOWER(action) LIKE %s ESCAPE E'\\\\'",
                    "LOWER(result) LIKE %s ESCAPE E'\\\\'",
                    "LOWER(actor) LIKE %s ESCAPE E'\\\\'",
                    "LOWER(target) LIKE %s ESCAPE E'\\\\'",
                    "LOWER(detail) LIKE %s ESCAPE E'\\\\'",
                    "LOWER(COALESCE(extra::text, '')) LIKE %s ESCAPE E'\\\\'",
                ]
            ) + ")"
        )
        params.extend([kw] * 7)

    sql_text = """
        SELECT ts, event_type, action, result, actor, target, detail, level, extra
        FROM audit_events
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY ts DESC, id DESC LIMIT %s"
    params.append(max_items)

    rows = fetch_all(sql_text, params)
    return [_row_to_event(row) for row in rows]


def list_filter_values() -> dict[str, list[str]]:
    """返回当前可用筛选值（去重）。"""

    event_types = [
        str(row["value"])
        for row in fetch_all(
            "SELECT DISTINCT event_type AS value FROM audit_events WHERE event_type <> '' ORDER BY value"
        )
    ]
    actions = [
        str(row["value"])
        for row in fetch_all(
            "SELECT DISTINCT action AS value FROM audit_events WHERE action <> '' ORDER BY value"
        )
    ]
    results = [
        str(row["value"])
        for row in fetch_all(
            "SELECT DISTINCT result AS value FROM audit_events WHERE result <> '' ORDER BY value"
        )
    ]
    actors = [
        str(row["value"])
        for row in fetch_all(
            "SELECT DISTINCT actor AS value FROM audit_events WHERE actor <> '' ORDER BY value"
        )
    ]

    return {
        "event_types": event_types,
        "actions": actions,
        "results": results,
        "actors": actors,
    }
