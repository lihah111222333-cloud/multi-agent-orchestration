"""消息总线异常日志：结构化写入与查询（PostgreSQL 持久化）。

分类 (category):
  - tool_timeout      工具执行超时
  - tool_error        工具执行异常
  - client_disconnect 客户端断连
  - session_stale     过期 session 拒绝
  - crash_restart     进程崩溃重启
  - unknown           其它未归类异常

严重等级 (severity): warning / error / critical


GO:migrate → go/internal/store/bus_log.go
GO:target  → Go sqlx + slog
"""

from __future__ import annotations

import json
import logging
import traceback as _tb
from datetime import datetime, timezone
from typing import Any, Optional

from db.postgres import execute, fetch_all
from utils import escape_like, normalize_limit

__all__ = [
    "record_bus_exception",
    "query_bus_exceptions",
    "list_bus_categories",
]

_logger = logging.getLogger("bus_log")

VALID_CATEGORIES = frozenset({
    "tool_timeout",
    "tool_error",
    "client_disconnect",
    "session_stale",
    "crash_restart",
    "unknown",
})

VALID_SEVERITIES = frozenset({"warning", "error", "critical"})


def _format_ts(value: Any) -> str:
    if isinstance(value, datetime):
        return value.astimezone().strftime("%Y-%m-%d %H:%M:%S")
    return str(value or "")


def record_bus_exception(
    category: str = "unknown",
    severity: str = "error",
    source: str = "",
    message: str = "",
    traceback: str = "",
    tool_name: str = "",
    extra: Optional[dict[str, Any]] = None,
    ts: Optional[datetime] = None,
) -> dict[str, Any]:
    """写入一条消息总线异常日志到 PostgreSQL。

    内部 try/except 保护，绝不影响主流程（写入失败仅 debug 日志）。
    """
    event_time = ts or datetime.now(timezone.utc)
    cat = (category or "unknown").strip().lower()
    if cat not in VALID_CATEGORIES:
        cat = "unknown"
    sev = (severity or "error").strip().lower()
    if sev not in VALID_SEVERITIES:
        sev = "error"

    record = {
        "ts": _format_ts(event_time),
        "category": cat,
        "severity": sev,
        "source": str(source or ""),
        "tool_name": str(tool_name or ""),
        "message": str(message or ""),
        "traceback": str(traceback or ""),
        "extra": extra,
    }

    try:
        extra_json = json.dumps(extra, ensure_ascii=False, default=str) if extra else None
        execute(
            """
            INSERT INTO bus_exception_logs
                (ts, category, severity, source, tool_name, message, traceback, extra)
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s)
            """,
            (
                event_time,
                cat,
                sev,
                record["source"],
                record["tool_name"],
                record["message"],
                record["traceback"],
                extra_json,
            ),
        )
    except Exception:
        # 日志写入绝不影响主流程
        _logger.debug("bus_log write failed", exc_info=True)

    return record


def query_bus_exceptions(
    limit: int = 100,
    category: str = "",
    severity: str = "",
    keyword: str = "",
) -> list[dict[str, Any]]:
    """查询消息总线异常日志（最新优先）。"""
    max_items = normalize_limit(limit)
    where: list[str] = []
    params: list[Any] = []

    if category:
        where.append("category = %s")
        params.append(category.strip().lower())

    if severity:
        where.append("severity = %s")
        params.append(severity.strip().lower())

    if keyword:
        kw = f"%{escape_like(keyword.lower())}%"
        where.append(
            "(" + " OR ".join([
                "LOWER(source) LIKE %s ESCAPE E'\\\\'",
                "LOWER(tool_name) LIKE %s ESCAPE E'\\\\'",
                "LOWER(message) LIKE %s ESCAPE E'\\\\'",
                "LOWER(traceback) LIKE %s ESCAPE E'\\\\'",
            ]) + ")"
        )
        params.extend([kw] * 4)

    sql = "SELECT ts, category, severity, source, tool_name, message, traceback, extra FROM bus_exception_logs"
    if where:
        sql += " WHERE " + " AND ".join(where)
    sql += " ORDER BY ts DESC, id DESC LIMIT %s"
    params.append(max_items)

    rows = fetch_all(sql, params)
    return [
        {
            "ts": _format_ts(row.get("ts")),
            "category": str(row.get("category", "")),
            "severity": str(row.get("severity", "")),
            "source": str(row.get("source", "")),
            "tool_name": str(row.get("tool_name", "")),
            "message": str(row.get("message", "")),
            "traceback": str(row.get("traceback", "")),
            "extra": row.get("extra"),
        }
        for row in rows
    ]


def list_bus_categories() -> dict[str, list[str]]:
    """返回已有的异常分类和严重等级（用于 UI 过滤下拉）。"""
    categories = [
        str(row["value"])
        for row in fetch_all(
            "SELECT DISTINCT category AS value FROM bus_exception_logs WHERE category <> '' ORDER BY value"
        )
    ]
    severities = [
        str(row["value"])
        for row in fetch_all(
            "SELECT DISTINCT severity AS value FROM bus_exception_logs WHERE severity <> '' ORDER BY value"
        )
    ]
    return {
        "categories": categories,
        "severities": severities,
    }
