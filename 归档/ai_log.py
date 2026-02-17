"""AI 日志查询（基于 system_logs 派生）。

GO:migrate → go/internal/store/ai_log.go
GO:target  → Go sqlx + regexp (编译时)
GO:notes:
  - _to_ai_row → Go struct AILogRow (消除)
  - _HTTPX_REQUEST_RE 等正则 → Go regexp.MustCompile
  - query_ai_logs keyword LIKE → 等价移植
"""

from __future__ import annotations

import re
from datetime import datetime
from typing import Any
from urllib.parse import urlparse

from db.postgres import fetch_all
from utils import escape_like, normalize_limit

__all__ = ["query_ai_logs", "list_ai_filter_values"]

_HTTPX_REQUEST_RE = re.compile(
    r'HTTP Request:\s+([A-Z]+)\s+(https?://\S+)\s+"HTTP/\d+(?:\.\d+)?\s+(\d{3})\s+([^"]+)"',
)
_ERROR_CODE_RE = re.compile(r"Error code:\s*(\d{3})")
_MODEL_RE = re.compile(r"model=([A-Za-z0-9._:-]+)")

_AI_LOGGER_PREFIXES = ("httpx", "openai", "langchain_openai")
_AI_HINTS = [
    "/responses",
    "/chat/completions",
    "openai",
    "error code:",
    "gpt-",
    "reasoning",
    "previous_response_id",
    "responses store",
    "conversation",
]


def _format_ts(value: Any) -> str:
    if isinstance(value, datetime):
        return value.astimezone().strftime("%Y-%m-%d %H:%M:%S")
    return str(value or "")


def _extract_endpoint(url: str) -> str:
    parsed = urlparse(url)
    path = str(parsed.path or "").strip()
    if not path:
        return ""
    if path.endswith("/"):
        path = path[:-1]
    return path


def _classify_row(logger_name: str, message: str, level: str, method: str, endpoint: str) -> str:
    text = message.lower()
    if method and endpoint:
        return "api_request"
    if "error code:" in text or "invalid_request_error" in text:
        return "api_error"
    if "use_previous_response_id" in text or "responses store" in text:
        return "compat_fallback"
    if logger_name == "utils" and "model=" in text:
        return "runtime_config"
    if level.upper() == "ERROR":
        return "error"
    return "ai_event"


def _to_ai_row(row: dict[str, Any]) -> dict[str, str]:
    logger_name = str(row.get("logger", "") or "")
    message = str(row.get("message", "") or "")
    level = str(row.get("level", "") or "").upper()
    raw = str(row.get("raw", "") or "")

    method = ""
    url = ""
    endpoint = ""
    status_code = ""
    status_text = ""

    req_match = _HTTPX_REQUEST_RE.search(message)
    if req_match:
        method = req_match.group(1)
        url = req_match.group(2)
        endpoint = _extract_endpoint(url)
        status_code = req_match.group(3)
        status_text = req_match.group(4).strip()

    if not status_code:
        err_code = _ERROR_CODE_RE.search(message)
        if err_code:
            status_code = err_code.group(1)
            status_text = "error"

    model = ""
    model_match = _MODEL_RE.search(message)
    if model_match:
        model = model_match.group(1)

    category = _classify_row(logger_name, message, level, method, endpoint)
    return {
        "ts": _format_ts(row.get("ts")),
        "level": level,
        "logger": logger_name,
        "message": message,
        "raw": raw,
        "category": category,
        "method": method,
        "url": url,
        "endpoint": endpoint,
        "status_code": status_code,
        "status_text": status_text,
        "model": model,
    }


def query_ai_logs(
    limit: int = 100,
    level: str = "",
    logger_name: str = "",
    keyword: str = "",
    category: str = "",
    endpoint: str = "",
    status_code: str = "",
) -> list[dict[str, str]]:
    """查询 AI 相关日志（按时间倒序）。"""
    max_items = normalize_limit(limit)
    max_scan = min(max_items * 6, 5000)

    where = []
    params: list[Any] = []

    logger_predicates = ["logger LIKE %s ESCAPE E'\\\\'" for _ in _AI_LOGGER_PREFIXES]
    logger_params = [f"{escape_like(prefix)}%" for prefix in _AI_LOGGER_PREFIXES]
    hint_predicates = [
        "LOWER(message) LIKE %s ESCAPE E'\\\\'" for _ in _AI_HINTS
    ]
    hint_params = [f"%{escape_like(hint)}%" for hint in _AI_HINTS]
    where.append(
        "("
        + " OR ".join(logger_predicates + hint_predicates)
        + ")"
    )
    params.extend(logger_params + hint_params)

    if level:
        where.append("level = %s")
        params.append(level.upper())

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
    params.append(max_scan)

    rows = fetch_all(sql_text, params)
    normalized_endpoint = str(endpoint or "").strip().lower()
    normalized_category = str(category or "").strip().lower()
    normalized_status = str(status_code or "").strip()

    result: list[dict[str, str]] = []
    for row in rows:
        item = _to_ai_row(row)
        if normalized_category and item["category"].lower() != normalized_category:
            continue
        if normalized_status and item["status_code"] != normalized_status:
            continue
        if normalized_endpoint:
            endpoint_text = item["endpoint"].lower()
            if normalized_endpoint not in endpoint_text:
                continue
        result.append(item)
        if len(result) >= max_items:
            break

    return result


def list_ai_filter_values(limit: int = 600) -> dict[str, list[str]]:
    """返回 AI 日志筛选项。"""
    rows = query_ai_logs(limit=limit)

    levels = sorted({row["level"] for row in rows if row.get("level")})
    loggers = sorted({row["logger"] for row in rows if row.get("logger")})
    categories = sorted({row["category"] for row in rows if row.get("category")})
    endpoints = sorted({row["endpoint"] for row in rows if row.get("endpoint")})
    status_codes = sorted({row["status_code"] for row in rows if row.get("status_code")})

    return {
        "levels": levels,
        "loggers": loggers,
        "categories": categories,
        "endpoints": endpoints,
        "status_codes": status_codes,
    }
