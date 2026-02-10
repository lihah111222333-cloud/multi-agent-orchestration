"""Agent 运营数据存储：交互表 / 提示词模板表 / 命令卡表。"""

from __future__ import annotations

import json
import os
import re
from datetime import datetime
from typing import Any, Optional

from audit_log import append_event
from db.postgres import connect_cursor, execute, fetch_all, fetch_one
from utils import escape_like, normalize_limit

KEY_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{1,127}$")
MAX_LIMIT = 1000
MAX_SQL_LENGTH = 4096
_SQL_WRITE_KEYWORD_RE = re.compile(
    r"\b(insert|update|delete|merge|create|alter|drop|truncate|grant|revoke|comment|copy|vacuum|analyze|refresh|reindex|cluster|call|do)\b",
    re.IGNORECASE,
)
_SQL_DANGEROUS_EXEC_KEYWORD_RE = re.compile(
    r"\b(create|alter|drop|truncate|grant|revoke|comment|copy|vacuum|analyze|refresh|reindex|cluster|call|do)\b",
    re.IGNORECASE,
)
_SQL_DML_KEYWORD_RE = re.compile(r"\b(insert|update|delete|merge)\b", re.IGNORECASE)
_SQL_TOKEN_RE = re.compile(r"('(?:''|[^'])*')|(\"(?:\"\"|[^\"])*\")|(--[^\n]*$)|(/\*.*?\*/)", re.MULTILINE | re.DOTALL)
_ALLOWED_EXEC_KEYWORDS = {
    "insert",
    "update",
    "delete",
    "merge",
    "with",
}
_DB_EXECUTE_ALLOWED_TABLES = {
    "agent_interactions",
    "prompt_templates",
    "command_cards",
    "command_card_runs",
}
_DML_TARGET_TABLE_RE = re.compile(
    r"\b(?:insert\s+into|update|delete\s+from|merge\s+into)\s+([A-Za-z_][A-Za-z0-9_$]*(?:\.[A-Za-z_][A-Za-z0-9_$]*)?)\b",
    re.IGNORECASE,
)


class RowMissingError(RuntimeError):
    pass


def _require_row(row: Optional[dict[str, Any]], action: str) -> dict[str, Any]:
    if row is None:
        raise RowMissingError(f"{action} 执行失败：数据库未返回结果")
    return row





def _strip_sql_literals(query: str) -> str:
    return _SQL_TOKEN_RE.sub(" ", str(query))


def _validate_single_statement(query: str) -> str:
    text = str(query or "").strip()
    if not text:
        raise ValueError("sql 不能为空")
    if len(text) > MAX_SQL_LENGTH:
        raise ValueError(f"sql 超过最大长度限制 ({MAX_SQL_LENGTH} 字符)")

    body = text.rstrip(";").strip()
    if not body:
        raise ValueError("sql 不能为空")
    if ";" in _strip_sql_literals(body):
        raise ValueError("仅允许执行单条 SQL")

    return body


def _first_sql_keyword(query: str) -> str:
    match = re.match(r"\s*([a-zA-Z_]+)", query)
    return match.group(1).lower() if match else ""


def _validate_read_only_query(query: str) -> str:
    body = _validate_single_statement(query)
    sanitized = _strip_sql_literals(body)
    first = _first_sql_keyword(sanitized)
    if first not in {"select", "with"}:
        raise ValueError("db_query 仅允许 SELECT/CTE 查询")
    if _SQL_WRITE_KEYWORD_RE.search(sanitized):
        raise ValueError("db_query 检测到写操作关键字，已拒绝")
    return body


def _validate_execute_query(query: str) -> str:
    body = _validate_single_statement(query)
    sanitized = _strip_sql_literals(body)
    first = _first_sql_keyword(sanitized)
    if not first:
        raise ValueError("sql 语法无效")
    if first in {"select", "show", "explain"}:
        raise ValueError("db_execute 不允许只读 SQL，请改用 db_query")
    if first not in _ALLOWED_EXEC_KEYWORDS:
        raise ValueError(f"db_execute 不支持该 SQL 类型: {first}")

    if _SQL_DANGEROUS_EXEC_KEYWORD_RE.search(sanitized):
        raise ValueError("db_execute 禁止执行 DDL/管理语句")

    if first == "with" and not _SQL_DML_KEYWORD_RE.search(sanitized):
        raise ValueError("db_execute 的 WITH 语句必须包含 INSERT/UPDATE/DELETE/MERGE")

    dml_tables = {
        str(match.group(1) or "").strip().lower().split(".")[-1]
        for match in _DML_TARGET_TABLE_RE.finditer(sanitized)
        if str(match.group(1) or "").strip()
    }
    if not dml_tables:
        raise ValueError("db_execute 未检测到 DML 目标表")

    blocked_tables = sorted(table for table in dml_tables if table not in _DB_EXECUTE_ALLOWED_TABLES)
    if blocked_tables:
        raise ValueError(f"db_execute 禁止操作非白名单表: {', '.join(blocked_tables)}")

    return body


def _is_db_execute_enabled() -> bool:
    raw = str(os.getenv("AGENT_DB_EXECUTE_ENABLED", "0")).strip().lower()
    return raw in {"1", "true", "yes", "on"}





def _normalize_key(name: str, value: str) -> str:
    text = str(value or "").strip()
    if not text:
        raise ValueError(f"{name} 不能为空")
    if not KEY_RE.fullmatch(text):
        raise ValueError(f"{name} 格式非法: {text}")
    return text


def _normalize_status(status: str) -> str:
    text = str(status or "pending").strip().lower()
    if not text:
        return "pending"
    return text


def _as_json_text(value: Any) -> str:
    if value is None:
        return "{}"
    if isinstance(value, (dict, list)):
        return json.dumps(value, ensure_ascii=False)
    if isinstance(value, str):
        stripped = value.strip()
        if not stripped:
            return "{}"
        try:
            loaded = json.loads(stripped)
            if isinstance(loaded, (dict, list)):
                return json.dumps(loaded, ensure_ascii=False)
        except json.JSONDecodeError:
            pass
        return json.dumps({"value": value}, ensure_ascii=False)
    return json.dumps({"value": value}, ensure_ascii=False)


def _fmt_dt(value: Any) -> str:
    if isinstance(value, datetime):
        return value.isoformat()
    return str(value or "")


def _row_to_interaction(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "thread_id": str(row.get("thread_id", "")),
        "parent_id": row.get("parent_id"),
        "sender": str(row.get("sender", "")),
        "receiver": str(row.get("receiver", "")),
        "msg_type": str(row.get("msg_type", "")),
        "status": str(row.get("status", "")),
        "requires_review": bool(row.get("requires_review", False)),
        "reviewed_by": str(row.get("reviewed_by", "")),
        "review_note": str(row.get("review_note", "")),
        "reviewed_at": _fmt_dt(row.get("reviewed_at")),
        "payload": row.get("payload") if isinstance(row.get("payload"), (dict, list)) else {},
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
    }


def _row_to_prompt(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "prompt_key": str(row.get("prompt_key", "")),
        "title": str(row.get("title", "")),
        "agent_key": str(row.get("agent_key", "")),
        "tool_name": str(row.get("tool_name", "")),
        "prompt_text": str(row.get("prompt_text", "")),
        "variables": row.get("variables") if isinstance(row.get("variables"), (dict, list)) else {},
        "tags": row.get("tags") if isinstance(row.get("tags"), (dict, list)) else [],
        "enabled": bool(row.get("enabled", True)),
        "created_by": str(row.get("created_by", "")),
        "updated_by": str(row.get("updated_by", "")),
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
    }


def _row_to_prompt_version(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "prompt_key": str(row.get("prompt_key", "")),
        "title": str(row.get("title", "")),
        "agent_key": str(row.get("agent_key", "")),
        "tool_name": str(row.get("tool_name", "")),
        "prompt_text": str(row.get("prompt_text", "")),
        "variables": row.get("variables") if isinstance(row.get("variables"), (dict, list)) else {},
        "tags": row.get("tags") if isinstance(row.get("tags"), (dict, list)) else [],
        "enabled": bool(row.get("enabled", True)),
        "created_by": str(row.get("created_by", "")),
        "updated_by": str(row.get("updated_by", "")),
        "source_updated_at": _fmt_dt(row.get("source_updated_at")),
        "created_at": _fmt_dt(row.get("created_at")),
        "archived_at": _fmt_dt(row.get("archived_at")),
    }


def _row_to_card(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "id": int(row.get("id", 0)),
        "card_key": str(row.get("card_key", "")),
        "title": str(row.get("title", "")),
        "description": str(row.get("description", "")),
        "command_template": str(row.get("command_template", "")),
        "args_schema": row.get("args_schema") if isinstance(row.get("args_schema"), (dict, list)) else {},
        "risk_level": str(row.get("risk_level", "normal")),
        "enabled": bool(row.get("enabled", True)),
        "created_by": str(row.get("created_by", "")),
        "updated_by": str(row.get("updated_by", "")),
        "created_at": _fmt_dt(row.get("created_at")),
        "updated_at": _fmt_dt(row.get("updated_at")),
    }


def create_interaction(
    sender: str,
    receiver: str,
    msg_type: str,
    content: str,
    thread_id: str = "",
    parent_id: Optional[int] = None,
    requires_review: bool = False,
    metadata: Optional[Any] = None,
    status: str = "pending",
) -> dict[str, Any]:
    """创建 Agent 间交互记录并持久化到 PostgreSQL。

    Args:
        sender: 发送方标识。
        receiver: 接收方标识。
        msg_type: 消息类型。
        content: 消息内容。
        thread_id: 会话线程 ID。
        parent_id: 父消息 ID（用于嵌套回复）。
        requires_review: 是否需要人工审批。
        metadata: 附加元数据。
        status: 初始状态，默认 'pending'。

    Returns:
        标准化后的交互记录字典。

    Raises:
        RowMissingError: 数据库插入未返回结果时抛出。
    """
    sender_text = _normalize_key("sender", sender)
    receiver_text = str(receiver or "").strip()
    msg_type_text = _normalize_key("msg_type", msg_type)
    thread_id_text = str(thread_id or "").strip()
    status_text = _normalize_status(status)

    payload = {
        "content": str(content or ""),
        "metadata": metadata if isinstance(metadata, (dict, list)) else ({"value": metadata} if metadata is not None else {}),
    }

    row = fetch_one(
        """
        INSERT INTO agent_interactions (
            thread_id, parent_id, sender, receiver, msg_type, status,
            requires_review, payload, updated_at
        )
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s::jsonb, NOW())
        RETURNING id, thread_id, parent_id, sender, receiver, msg_type, status,
                  requires_review, reviewed_by, review_note, reviewed_at,
                  payload, created_at, updated_at
        """,
        (
            thread_id_text,
            parent_id,
            sender_text,
            receiver_text,
            msg_type_text,
            status_text,
            bool(requires_review),
            _as_json_text(payload),
        ),
    )

    result = _row_to_interaction(_require_row(row, "create_interaction"))
    append_event(
        event_type="agent_interaction",
        action="create",
        result="ok",
        actor=sender_text,
        target=receiver_text,
        detail=f"msg_type={msg_type_text}",
        extra={"interaction_id": result["id"], "thread_id": thread_id_text},
    )
    return result


def list_interactions(
    thread_id: str = "",
    sender: str = "",
    receiver: str = "",
    msg_type: str = "",
    status: str = "",
    requires_review: Optional[bool] = None,
    limit: int = 100,
) -> list[dict[str, Any]]:
    """按条件查询交互记录列表。

    Args:
        thread_id: 按会话线程过滤。
        sender: 按发送方过滤。
        receiver: 按接收方过滤。
        msg_type: 按消息类型过滤。
        status: 按状态过滤。
        requires_review: 按是否需审批过滤。
        limit: 返回最大条数。

    Returns:
        交互记录字典列表，按创建时间倒序。
    """
    where = []
    params: list[Any] = []

    if thread_id:
        where.append("thread_id = %s")
        params.append(str(thread_id).strip())
    if sender:
        where.append("sender = %s")
        params.append(str(sender).strip())
    if receiver:
        where.append("receiver = %s")
        params.append(str(receiver).strip())
    if msg_type:
        where.append("msg_type = %s")
        params.append(str(msg_type).strip())
    if status:
        where.append("status = %s")
        params.append(_normalize_status(status))
    if requires_review is not None:
        where.append("requires_review = %s")
        params.append(bool(requires_review))

    sql_text = """
        SELECT id, thread_id, parent_id, sender, receiver, msg_type, status,
               requires_review, reviewed_by, review_note, reviewed_at,
               payload, created_at, updated_at
        FROM agent_interactions
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY created_at DESC, id DESC LIMIT %s"
    params.append(normalize_limit(limit))

    return [_row_to_interaction(row) for row in fetch_all(sql_text, params)]


def review_interaction(interaction_id: int, status: str, reviewer: str = "", note: str = "") -> dict[str, Any]:
    try:
        iid = int(interaction_id)
    except (TypeError, ValueError) as exc:
        raise ValueError("interaction_id 非法") from exc

    status_text = _normalize_status(status)
    reviewer_text = str(reviewer or "").strip()
    note_text = str(note or "").strip()

    updated = fetch_one(
        """
        UPDATE agent_interactions
        SET status = %s,
            reviewed_by = %s,
            review_note = %s,
            reviewed_at = NOW(),
            updated_at = NOW()
        WHERE id = %s
        RETURNING id, thread_id, parent_id, sender, receiver, msg_type, status,
                  requires_review, reviewed_by, review_note, reviewed_at,
                  payload, created_at, updated_at
        """,
        (status_text, reviewer_text, note_text, iid),
    )
    if not updated:
        return {"ok": False, "message": f"interaction not found: {iid}"}

    result = _row_to_interaction(updated)
    append_event(
        event_type="agent_interaction",
        action="review",
        result="ok",
        actor=reviewer_text,
        target=str(iid),
        detail=status_text,
    )
    return {"ok": True, "interaction": result}


def save_prompt_template(
    prompt_key: str,
    title: str,
    prompt_text: str,
    agent_key: str = "",
    tool_name: str = "",
    variables: Optional[Any] = None,
    tags: Optional[Any] = None,
    enabled: bool = True,
    updated_by: str = "",
) -> dict[str, Any]:
    """保存或更新提示词模板（按 prompt_key 做 upsert）。

    Args:
        prompt_key: 提示词唯一标识。
        title: 提示词标题。
        prompt_text: 提示词正文。
        agent_key: 关联的 Agent 标识。
        tool_name: 关联的工具名称。
        variables: 模板变量定义。
        tags: 标签列表。
        enabled: 是否启用。
        updated_by: 更新人标识。

    Returns:
        标准化后的提示词模板字典。

    Raises:
        ValueError: prompt_text 为空或 prompt_key 格式非法时抛出。
    """
    key = _normalize_key("prompt_key", prompt_key)
    title_text = str(title or "").strip()
    prompt_body = str(prompt_text or "").strip()
    if not prompt_body:
        raise ValueError("prompt_text 不能为空")

    previous = fetch_one(
        """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               created_at, updated_at
        FROM prompt_templates
        WHERE prompt_key = %s
        """,
        (key,),
    )

    if previous:
        execute(
            """
            INSERT INTO prompt_template_versions (
                prompt_key, title, agent_key, tool_name, prompt_text,
                variables, tags, enabled, created_by, updated_by,
                source_updated_at
            )
            VALUES (%s, %s, %s, %s, %s, %s::jsonb, %s::jsonb, %s, %s, %s, %s)
            """,
            (
                str(previous.get("prompt_key") or ""),
                str(previous.get("title") or ""),
                str(previous.get("agent_key") or ""),
                str(previous.get("tool_name") or ""),
                str(previous.get("prompt_text") or ""),
                _as_json_text(previous.get("variables")),
                _as_json_text(previous.get("tags") if previous.get("tags") is not None else []),
                bool(previous.get("enabled", True)),
                str(previous.get("created_by") or ""),
                str(previous.get("updated_by") or ""),
                previous.get("updated_at"),
            ),
        )

    row = fetch_one(
        """
        INSERT INTO prompt_templates (
            prompt_key, title, agent_key, tool_name, prompt_text,
            variables, tags, enabled, created_by, updated_by, updated_at
        )
        VALUES (%s, %s, %s, %s, %s, %s::jsonb, %s::jsonb, %s, %s, %s, NOW())
        ON CONFLICT (prompt_key)
        DO UPDATE SET
            title = EXCLUDED.title,
            agent_key = EXCLUDED.agent_key,
            tool_name = EXCLUDED.tool_name,
            prompt_text = EXCLUDED.prompt_text,
            variables = EXCLUDED.variables,
            tags = EXCLUDED.tags,
            enabled = EXCLUDED.enabled,
            updated_by = EXCLUDED.updated_by,
            updated_at = NOW()
        RETURNING id, prompt_key, title, agent_key, tool_name, prompt_text,
                  variables, tags, enabled, created_by, updated_by,
                  created_at, updated_at
        """,
        (
            key,
            title_text,
            str(agent_key or "").strip(),
            str(tool_name or "").strip(),
            prompt_body,
            _as_json_text(variables),
            _as_json_text(tags if tags is not None else []),
            bool(enabled),
            str(updated_by or ""),
            str(updated_by or ""),
        ),
    )

    result = _row_to_prompt(_require_row(row, "save_prompt_template"))
    append_event(
        event_type="prompt_template",
        action="save",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail=result.get("tool_name", ""),
    )
    return result


def list_prompt_template_versions(prompt_key: str, limit: int = 20) -> list[dict[str, Any]]:
    key = _normalize_key("prompt_key", prompt_key)
    rows = fetch_all(
        """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               source_updated_at, created_at, archived_at
        FROM prompt_template_versions
        WHERE prompt_key = %s
        ORDER BY id DESC
        LIMIT %s
        """,
        (key, normalize_limit(limit, default=20)),
    )
    return [_row_to_prompt_version(row) for row in rows]


def rollback_prompt_template(prompt_key: str, version_id: int, updated_by: str = "") -> dict[str, Any]:
    key = _normalize_key("prompt_key", prompt_key)
    try:
        vid = int(version_id)
    except (TypeError, ValueError) as exc:
        raise ValueError("version_id 非法") from exc
    if vid <= 0:
        raise ValueError("version_id 非法")

    version_row = fetch_one(
        """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               source_updated_at, created_at, archived_at
        FROM prompt_template_versions
        WHERE id = %s AND prompt_key = %s
        """,
        (vid, key),
    )
    if not version_row:
        return {"ok": False, "message": f"prompt version not found: {key}#{vid}"}

    prompt = save_prompt_template(
        prompt_key=key,
        title=str(version_row.get("title") or ""),
        prompt_text=str(version_row.get("prompt_text") or ""),
        agent_key=str(version_row.get("agent_key") or ""),
        tool_name=str(version_row.get("tool_name") or ""),
        variables=version_row.get("variables"),
        tags=version_row.get("tags"),
        enabled=bool(version_row.get("enabled", True)),
        updated_by=str(updated_by or ""),
    )
    append_event(
        event_type="prompt_template",
        action="rollback",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail=f"version_id={vid}",
    )
    return {"ok": True, "prompt": prompt, "version": _row_to_prompt_version(version_row)}


def get_prompt_template(prompt_key: str) -> Optional[dict[str, Any]]:
    key = _normalize_key("prompt_key", prompt_key)
    row = fetch_one(
        """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               created_at, updated_at
        FROM prompt_templates
        WHERE prompt_key = %s
        """,
        (key,),
    )
    return _row_to_prompt(row) if row else None


def list_prompt_templates(
    agent_key: str = "",
    tool_name: str = "",
    keyword: str = "",
    enabled_only: bool = False,
    limit: int = 100,
) -> list[dict[str, Any]]:
    where = []
    params: list[Any] = []

    if agent_key:
        where.append("agent_key = %s")
        params.append(str(agent_key).strip())
    if tool_name:
        where.append("tool_name = %s")
        params.append(str(tool_name).strip())
    if enabled_only:
        where.append("enabled = TRUE")
    if keyword:
        kw = f"%{escape_like(keyword.lower())}%"
        where.append(
            "(LOWER(prompt_key) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(title) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(prompt_text) LIKE %s ESCAPE E'\\\\')"
        )
        params.extend([kw, kw, kw])

    sql_text = """
        SELECT id, prompt_key, title, agent_key, tool_name, prompt_text,
               variables, tags, enabled, created_by, updated_by,
               created_at, updated_at
        FROM prompt_templates
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY updated_at DESC, id DESC LIMIT %s"
    params.append(normalize_limit(limit))

    return [_row_to_prompt(row) for row in fetch_all(sql_text, params)]


def set_prompt_template_enabled(prompt_key: str, enabled: bool, updated_by: str = "") -> dict[str, Any]:
    key = _normalize_key("prompt_key", prompt_key)
    row = fetch_one(
        """
        UPDATE prompt_templates
        SET enabled = %s,
            updated_by = %s,
            updated_at = NOW()
        WHERE prompt_key = %s
        RETURNING id, prompt_key, title, agent_key, tool_name, prompt_text,
                  variables, tags, enabled, created_by, updated_by,
                  created_at, updated_at
        """,
        (bool(enabled), str(updated_by or ""), key),
    )
    if not row:
        return {"ok": False, "message": f"prompt not found: {key}"}

    result = _row_to_prompt(row)
    append_event(
        event_type="prompt_template",
        action="toggle",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail="enabled" if enabled else "disabled",
    )
    return {"ok": True, "prompt": result}


def save_command_card(
    card_key: str,
    title: str,
    command_template: str,
    description: str = "",
    args_schema: Optional[Any] = None,
    risk_level: str = "normal",
    enabled: bool = True,
    updated_by: str = "",
) -> dict[str, Any]:
    """保存或更新命令卡定义（按 card_key 做 upsert）。

    Args:
        card_key: 命令卡唯一标识。
        title: 命令卡标题。
        command_template: 命令模板（支持 {param} 占位符）。
        description: 命令卡描述。
        args_schema: 参数 schema 定义。
        risk_level: 风险等级（low/normal/high/critical）。
        enabled: 是否启用。
        updated_by: 更新人标识。

    Returns:
        标准化后的命令卡字典。

    Raises:
        ValueError: title 或 command_template 为空时抛出。
    """
    key = _normalize_key("card_key", card_key)
    title_text = str(title or "").strip()
    command_text = str(command_template or "").strip()
    if not title_text:
        raise ValueError("title 不能为空")
    if not command_text:
        raise ValueError("command_template 不能为空")

    row = fetch_one(
        """
        INSERT INTO command_cards (
            card_key, title, description, command_template, args_schema,
            risk_level, enabled, created_by, updated_by, updated_at
        )
        VALUES (%s, %s, %s, %s, %s::jsonb, %s, %s, %s, %s, NOW())
        ON CONFLICT (card_key)
        DO UPDATE SET
            title = EXCLUDED.title,
            description = EXCLUDED.description,
            command_template = EXCLUDED.command_template,
            args_schema = EXCLUDED.args_schema,
            risk_level = EXCLUDED.risk_level,
            enabled = EXCLUDED.enabled,
            updated_by = EXCLUDED.updated_by,
            updated_at = NOW()
        RETURNING id, card_key, title, description, command_template,
                  args_schema, risk_level, enabled, created_by, updated_by,
                  created_at, updated_at
        """,
        (
            key,
            title_text,
            str(description or ""),
            command_text,
            _as_json_text(args_schema),
            str(risk_level or "normal").strip().lower() or "normal",
            bool(enabled),
            str(updated_by or ""),
            str(updated_by or ""),
        ),
    )

    result = _row_to_card(_require_row(row, "save_command_card"))
    append_event(
        event_type="command_card",
        action="save",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail=result.get("risk_level", "normal"),
    )
    return result


def get_command_card(card_key: str) -> Optional[dict[str, Any]]:
    key = _normalize_key("card_key", card_key)
    row = fetch_one(
        """
        SELECT id, card_key, title, description, command_template,
               args_schema, risk_level, enabled, created_by, updated_by,
               created_at, updated_at
        FROM command_cards
        WHERE card_key = %s
        """,
        (key,),
    )
    return _row_to_card(row) if row else None


def list_command_cards(
    keyword: str = "",
    risk_level: str = "",
    enabled_only: bool = False,
    limit: int = 100,
) -> list[dict[str, Any]]:
    where = []
    params: list[Any] = []

    if risk_level:
        where.append("risk_level = %s")
        params.append(str(risk_level).strip().lower())
    if enabled_only:
        where.append("enabled = TRUE")
    if keyword:
        kw = f"%{escape_like(keyword.lower())}%"
        where.append(
            "(LOWER(card_key) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(title) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(description) LIKE %s ESCAPE E'\\\\' "
            "OR LOWER(command_template) LIKE %s ESCAPE E'\\\\')"
        )
        params.extend([kw, kw, kw, kw])

    sql_text = """
        SELECT id, card_key, title, description, command_template,
               args_schema, risk_level, enabled, created_by, updated_by,
               created_at, updated_at
        FROM command_cards
    """
    if where:
        sql_text += " WHERE " + " AND ".join(where)
    sql_text += " ORDER BY updated_at DESC, id DESC LIMIT %s"
    params.append(normalize_limit(limit))

    return [_row_to_card(row) for row in fetch_all(sql_text, params)]


def set_command_card_enabled(card_key: str, enabled: bool, updated_by: str = "") -> dict[str, Any]:
    key = _normalize_key("card_key", card_key)
    row = fetch_one(
        """
        UPDATE command_cards
        SET enabled = %s,
            updated_by = %s,
            updated_at = NOW()
        WHERE card_key = %s
        RETURNING id, card_key, title, description, command_template,
                  args_schema, risk_level, enabled, created_by, updated_by,
                  created_at, updated_at
        """,
        (bool(enabled), str(updated_by or ""), key),
    )
    if not row:
        return {"ok": False, "message": f"command card not found: {key}"}

    result = _row_to_card(row)
    append_event(
        event_type="command_card",
        action="toggle",
        result="ok",
        actor=str(updated_by or ""),
        target=key,
        detail="enabled" if enabled else "disabled",
    )
    return {"ok": True, "command_card": result}


# 便捷：允许 Agent 执行只读查询
def db_query(sql_text: str, limit: int = 200) -> list[dict[str, Any]]:
    query = _validate_read_only_query(sql_text)
    wrapped = f"SELECT * FROM ({query}) AS t LIMIT %s"
    with connect_cursor(row_as_dict=True, autocommit=False) as cur:
        cur.execute("SET LOCAL transaction_read_only = on")
        cur.execute(wrapped, (normalize_limit(limit, default=200),))
        return cur.fetchall()


# 便捷：允许 Agent 执行单条变更语句（测试阶段）
def db_execute(sql_text: str) -> dict[str, Any]:
    if not _is_db_execute_enabled():
        return {
            "ok": False,
            "error": "db_execute 已禁用：请设置 AGENT_DB_EXECUTE_ENABLED=1 后重试",
        }

    query = _validate_execute_query(sql_text)

    rowcount = execute(query)
    append_event(
        event_type="db",
        action="execute",
        result="ok",
        actor="agent",
        target="postgres",
        detail=query[:180],
        extra={"rowcount": rowcount},
    )
    return {"ok": True, "rowcount": rowcount}
