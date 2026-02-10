"""文件共享存储（PostgreSQL）。"""

from __future__ import annotations

from datetime import datetime
from typing import Any, Optional

from audit_log import append_event
from db.postgres import execute, fetch_all, fetch_one
from utils import escape_like, normalize_limit

__all__ = ["write_file", "read_file", "list_files", "delete_file"]


MAX_LIST_LIMIT = 1000





def _normalize_path(path: str) -> str:
    p = str(path or "").strip().replace("\\", "/")
    if not p:
        raise ValueError("path 不能为空")
    p = p.strip("/")
    if not p:
        raise ValueError("path 不能为空")
    return p


def _row_to_file(row: dict[str, Any]) -> dict[str, Any]:
    created_at = row.get("created_at")
    updated_at = row.get("updated_at")

    return {
        "path": str(row.get("path", "")),
        "content": str(row.get("content", "")),
        "updated_by": str(row.get("updated_by", "")),
        "created_at": created_at.isoformat() if isinstance(created_at, datetime) else str(created_at or ""),
        "updated_at": updated_at.isoformat() if isinstance(updated_at, datetime) else str(updated_at or ""),
    }


def write_file(path: str, content: str, actor: str = "") -> dict[str, Any]:
    """Write or upsert a shared file into PostgreSQL."""
    file_path = _normalize_path(path)
    user = str(actor or "")
    execute(
        """
        INSERT INTO shared_files (path, content, updated_by, created_at, updated_at)
        VALUES (%s, %s, %s, NOW(), NOW())
        ON CONFLICT (path)
        DO UPDATE SET content = EXCLUDED.content,
                      updated_by = EXCLUDED.updated_by,
                      updated_at = NOW()
        """,
        (file_path, str(content or ""), user),
    )

    append_event(
        event_type="file_share",
        action="write",
        result="ok",
        actor=user,
        target=file_path,
        detail=f"size={len(str(content or ''))}",
    )

    result = read_file(file_path)
    return result or {"path": file_path, "content": str(content or ""), "updated_by": user}


def read_file(path: str) -> Optional[dict[str, Any]]:
    """Read a shared file by path."""
    file_path = _normalize_path(path)
    row = fetch_one(
        """
        SELECT path, content, updated_by, created_at, updated_at
        FROM shared_files
        WHERE path = %s
        """,
        (file_path,),
    )
    if not row:
        return None
    return _row_to_file(row)


def list_files(prefix: str = "", limit: int = 200) -> list[dict[str, Any]]:
    """List shared files, optionally filtered by path prefix."""
    max_items = normalize_limit(limit, default=200)
    where = []
    params: list[Any] = []

    if prefix:
        normalized_prefix = _normalize_path(prefix)
        where.append("path LIKE %s ESCAPE E'\\\\'")
        params.append(f"{escape_like(normalized_prefix)}%")

    sql_text = "SELECT path, content, updated_by, created_at, updated_at FROM shared_files"
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY updated_at DESC, path ASC LIMIT %s"
    params.append(max_items)

    rows = fetch_all(sql_text, params)
    return [_row_to_file(row) for row in rows]


def delete_file(path: str, actor: str = "") -> dict[str, Any]:
    """Delete a shared file by path."""
    file_path = _normalize_path(path)
    removed = execute("DELETE FROM shared_files WHERE path = %s", (file_path,)) > 0

    append_event(
        event_type="file_share",
        action="delete",
        result="ok" if removed else "not_found",
        actor=str(actor or ""),
        target=file_path,
        detail="",
    )

    return {
        "ok": True,
        "deleted": removed,
        "path": file_path,
    }
