"""系统日志查询与写入（PostgreSQL 持久化）。

GO:migrate → go/internal/store/system_log.go
GO:target  → Go sqlx + slog
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any, Optional

from db.postgres import execute, fetch_all
from utils import escape_like, normalize_limit

__all__ = ["append_log", "query_logs", "list_filter_values"]




def _format_ts(value: Any) -> str:
    if isinstance(value, datetime):
        return value.astimezone().strftime("%Y-%m-%d %H:%M:%S")
    return str(value or "")


def append_log(
    level: str,
    logger_name: str,
    message: str,
    raw: str = "",
    ts: Optional[datetime] = None,
) -> dict[str, str]:
    """Write a system log entry to PostgreSQL."""
    event_time = ts or datetime.now(timezone.utc)
    level_text = (level or "INFO").upper()
    logger_text = (logger_name or "").strip()
    message_text = str(message or "")
    raw_text = str(raw or "")

    execute(
        """
        INSERT INTO system_logs (ts, level, logger, message, raw)
        VALUES (%s, %s, %s, %s, %s)
        """,
        (event_time, level_text, logger_text, message_text, raw_text),
    )

    return {
        "ts": _format_ts(event_time),
        "level": level_text,
        "logger": logger_text,
        "message": message_text,
        "raw": raw_text,
    }


def query_logs(limit: int = 100, level: str = "", logger_name: str = "", keyword: str = "") -> list[dict]:
    """Query system logs with optional filters (newest first)."""
    max_items = normalize_limit(limit)
    where = []
    params: list[Any] = []

    if level:
        where.append("level = %s")
        params.append(level)

    if logger_name:
        where.append("logger = %s")
        params.append(logger_name)

    if keyword:
        kw = f"%{escape_like(keyword.lower())}%"
        where.append(
            "(" + " OR ".join(
                [
                    "LOWER(level) LIKE %s ESCAPE E'\\\\'",
                    "LOWER(logger) LIKE %s ESCAPE E'\\\\'",
                    "LOWER(message) LIKE %s ESCAPE E'\\\\'",
                    "LOWER(raw) LIKE %s ESCAPE E'\\\\'",
                ]
            ) + ")"
        )
        params.extend([kw] * 4)

    sql_text = "SELECT ts, level, logger, message, raw FROM system_logs"
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY ts DESC, id DESC LIMIT %s"
    params.append(max_items)

    rows = fetch_all(sql_text, params)
    return [
        {
            "ts": _format_ts(row.get("ts")),
            "level": str(row.get("level", "")),
            "logger": str(row.get("logger", "")),
            "message": str(row.get("message", "")),
            "raw": str(row.get("raw", "")),
        }
        for row in rows
    ]


def list_filter_values() -> dict[str, list[str]]:
    """Return distinct filter values for the system log UI."""
    levels = [
        str(row["value"])
        for row in fetch_all("SELECT DISTINCT level AS value FROM system_logs WHERE level <> '' ORDER BY value")
    ]
    loggers = [
        str(row["value"])
        for row in fetch_all("SELECT DISTINCT logger AS value FROM system_logs WHERE logger <> '' ORDER BY value")
    ]

    return {
        "levels": levels,
        "loggers": loggers,
    }
