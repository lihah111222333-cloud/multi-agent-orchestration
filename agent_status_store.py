"""Agent 状态存储（PostgreSQL）。"""

from __future__ import annotations

import json
import re
from datetime import datetime
from typing import Any

from db.postgres import fetch_all, fetch_one
from utils import normalize_limit

__all__ = ["upsert_agent_status", "query_agent_status"]


_AGENT_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
_ALLOWED_STATUS = {"running", "idle", "stuck", "error", "disconnected", "unknown"}
_MAX_OUTPUT_LINES = 50


def _normalize_agent_id(agent_id: str) -> str:
    text = str(agent_id or "").strip()
    if not text:
        raise ValueError("agent_id 不能为空")
    if _AGENT_ID_RE.fullmatch(text) is None:
        raise ValueError(f"agent_id 格式非法: {text}")
    return text


def _normalize_status(status: str) -> str:
    text = str(status or "unknown").strip().lower() or "unknown"
    if text not in _ALLOWED_STATUS:
        raise ValueError(f"status 非法: {status}")
    return text


def _normalize_stagnant_sec(stagnant_sec: int) -> int:
    try:
        value = int(stagnant_sec)
    except (TypeError, ValueError) as exc:
        raise ValueError("stagnant_sec 必须是整数") from exc
    if value < 0:
        raise ValueError("stagnant_sec 不能小于 0")
    return value


def _normalize_output_tail(output_tail: Any) -> list[str]:
    if output_tail is None:
        return []

    if isinstance(output_tail, list):
        items = output_tail
    else:
        items = [output_tail]

    lines: list[str] = []
    for item in items:
        text = str(item).strip()
        if text:
            lines.append(text)

    if len(lines) > _MAX_OUTPUT_LINES:
        return lines[-_MAX_OUTPUT_LINES:]
    return lines


def _as_output_json(lines: list[str]) -> str:
    return json.dumps(lines, ensure_ascii=False)


def _parse_output_tail(value: Any) -> list[str]:
    if isinstance(value, list):
        return [str(item) for item in value]

    if isinstance(value, str):
        text = value.strip()
        if not text:
            return []
        try:
            parsed = json.loads(text)
        except json.JSONDecodeError:
            return [text]
        if isinstance(parsed, list):
            return [str(item) for item in parsed]
        return [text]

    return []


def _fmt_dt(value: Any) -> str:
    if isinstance(value, datetime):
        return value.isoformat()
    return str(value or "")


def _row_to_agent_status(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "agent_id": str(row.get("agent_id", "")),
        "agent_name": str(row.get("agent_name", "")),
        "session_id": str(row.get("session_id", "")),
        "status": str(row.get("status", "unknown")),
        "stagnant_sec": int(row.get("stagnant_sec", 0) or 0),
        "error": str(row.get("error", "")),
        "output_tail": _parse_output_tail(row.get("output_tail")),
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
    }


def upsert_agent_status(
    agent_id: str,
    agent_name: str = "",
    session_id: str = "",
    status: str = "unknown",
    stagnant_sec: int = 0,
    error: str = "",
    output_tail: list[str] | None = None,
) -> dict[str, Any]:
    """Insert or update an agent status snapshot."""
    aid = _normalize_agent_id(agent_id)
    normalized_status = _normalize_status(status)
    stagnant_value = _normalize_stagnant_sec(stagnant_sec)
    lines = _normalize_output_tail(output_tail)

    row = fetch_one(
        """
        INSERT INTO agent_status (
            agent_id,
            agent_name,
            session_id,
            status,
            stagnant_sec,
            error,
            output_tail,
            created_at,
            updated_at
        )
        VALUES (%s, %s, %s, %s, %s, %s, %s::jsonb, NOW(), NOW())
        ON CONFLICT (agent_id)
        DO UPDATE SET
            agent_name = EXCLUDED.agent_name,
            session_id = EXCLUDED.session_id,
            status = EXCLUDED.status,
            stagnant_sec = EXCLUDED.stagnant_sec,
            error = EXCLUDED.error,
            output_tail = EXCLUDED.output_tail,
            updated_at = NOW()
        RETURNING agent_id, agent_name, session_id, status, stagnant_sec,
                  error, output_tail, created_at, updated_at
        """,
        (
            aid,
            str(agent_name or ""),
            str(session_id or ""),
            normalized_status,
            stagnant_value,
            str(error or ""),
            _as_output_json(lines),
        ),
    )

    if row is None:
        raise RuntimeError("upsert_agent_status 执行失败：数据库未返回结果")

    return _row_to_agent_status(row)


def query_agent_status(
    agent_id: str = "",
    status: str = "",
    limit: int = 100,
) -> list[dict[str, Any]]:
    """Query latest agent status rows with optional filters."""
    where: list[str] = []
    params: list[Any] = []

    if agent_id:
        where.append("agent_id = %s")
        params.append(_normalize_agent_id(agent_id))

    if status:
        where.append("status = %s")
        params.append(_normalize_status(status))

    sql_text = (
        "SELECT agent_id, agent_name, session_id, status, stagnant_sec, "
        "error, output_tail, created_at, updated_at "
        "FROM agent_status"
    )
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY updated_at DESC, agent_id ASC LIMIT %s"
    params.append(normalize_limit(limit, default=100))

    rows = fetch_all(sql_text, params)
    return [_row_to_agent_status(row) for row in rows]
